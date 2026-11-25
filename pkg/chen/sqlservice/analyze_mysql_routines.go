package sqlservice

import (
	"fmt"
	"reflect"
	"strings"

	omniast "github.com/bytebase/omni/mysql/ast"
	omniparser "github.com/bytebase/omni/mysql/parser"
	"github.com/jumpserver/koko/pkg/chen/dialect"
	"github.com/jumpserver/koko/pkg/chen/sqlanalysis"
	"github.com/pingcap/tidb/pkg/parser"
	"github.com/pingcap/tidb/pkg/parser/mysql"
)

// TiDB does not implement MySQL stored programs. Use the already pinned MySQL
// grammar for their compound boundaries; all execution and policy text still
// comes from the original document. Other statements keep TiDB classification.
func analyzeMySQLRoutines(document, input string, mode mysql.SQLMode, native *parser.Parser) (Analysis, error) {
	out := Analysis{Statements: []Statement{}, Syntax: sqlanalysis.Checked, Compatibility: sqlanalysis.Unknown}
	parseInput := []byte(input)
	for _, t := range lexMode(input, dialect.MySQL, mode.HasANSIQuotesMode(), mode.HasNoBackslashEscapesMode()) {
		if t.is("DELIMITER") && strings.TrimSpace(input[strings.LastIndexByte(input[:t.start], '\n')+1:t.start]) == "" {
			return out, fmt.Errorf("DELIMITER is a client directive; submit the complete routine without it")
		}
		if t.quoted && !t.closed || t.kind == 'c' && strings.HasPrefix(t.text, "/*") && !t.closed {
			return out, fmt.Errorf("unterminated SQL token")
		}
		// Omni's grammar uses the standard string/identifier quote modes. This
		// parser-only projection preserves byte offsets and never changes the
		// SQL sent to the database, including mode-specific routine bodies.
		if t.quoted && t.quote == '"' && mode.HasANSIQuotesMode() {
			for i := t.start; i < t.end; i++ {
				if parseInput[i] == '`' {
					parseInput[i] = '_'
				} else if parseInput[i] == '"' {
					parseInput[i] = '`'
				}
			}
		}
		if t.kind == 's' && mode.HasNoBackslashEscapesMode() {
			for i := t.start; i < t.end; i++ {
				if parseInput[i] == '\\' {
					parseInput[i] = ' '
				}
			}
		}
	}
	foundRoutine := false
	for _, segment := range omniparser.Split(string(parseInput)) {
		if segment.Empty() {
			continue
		}
		tree, err := omniparser.Parse(segment.Text)
		if err != nil || tree == nil || len(tree.Items) != 1 {
			return out, fmt.Errorf("a complete MySQL statement boundary could not be parsed")
		}
		kind := ""
		returnsRows := false
		switch n := tree.Items[0].(type) {
		case *omniast.CreateTriggerStmt:
			kind = "createtrigger"
		case *omniast.CreateFunctionStmt:
			kind = "createfunction"
			if n.IsProcedure {
				kind = "createprocedure"
			}
		case *omniast.CreateEventStmt:
			kind = "createevent"
		case *omniast.DropTriggerStmt:
			kind = "droptrigger"
		case *omniast.DropEventStmt:
			kind = "dropevent"
		case *omniast.AlterEventStmt:
			kind = "alterevent"
		case *omniast.DropRoutineStmt:
			kind = "dropfunction"
			if n.IsProcedure {
				kind = "dropprocedure"
			}
		case *omniast.AlterRoutineStmt:
			kind = "alterfunction"
			if n.IsProcedure {
				kind = "alterprocedure"
			}
		case *omniast.ShowStmt:
			switch strings.ToUpper(n.Type) {
			case "CREATE TRIGGER", "CREATE PROCEDURE", "CREATE FUNCTION", "CREATE EVENT",
				"TRIGGERS", "EVENTS", "PROCEDURE STATUS", "FUNCTION STATUS", "PROCEDURE CODE", "FUNCTION CODE":
				kind, returnsRows = "show", true
			}
		}
		start, end := segment.ByteStart, segment.ByteEnd
		if start < 0 || end <= start || end > len(document) {
			return out, fmt.Errorf("invalid MySQL statement source span")
		}
		statement := Statement{SQL: document[start:end], Start: start, End: end, Kind: kind, ReturnsRows: returnsRows, MayWrite: !returnsRows}
		if kind != "" {
			// DDL remains a write/implicit-commit operation. Routine contents
			// are not independently executed or classified as safe reads.
			foundRoutine = true
		} else {
			nodes, _, err := native.ParseSQL(input[start:end])
			if err != nil || len(nodes) != 1 {
				return out, fmt.Errorf("non-routine MySQL statement is not supported by the native parser")
			}
			statement.Kind = strings.ToLower(strings.TrimSuffix(reflect.TypeOf(nodes[0]).Elem().Name(), "Stmt"))
			statement.ReturnsRows, statement.MayWrite = mysqlStatementCapabilities(nodes[0])
		}
		out.Statements = append(out.Statements, statement)
	}
	if !foundRoutine {
		return out, fmt.Errorf("no MySQL stored program was found")
	}
	return out, nil
}
