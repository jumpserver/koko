package sqlservice

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/jumpserver/koko/pkg/chen/dialect"
	"github.com/pingcap/tidb/pkg/parser/mysql"
)

type mysqlDDLHeader struct {
	verb                 string
	kind                 ObjectKind
	replace              bool
	definer              string
	definerStart         int
	verbToken, kindToken token
	nameToken            token
	next                 int
	tokens               []token
}

func mysqlObjectName(ts []token, pos int, db string) (string, string, int, error) {
	if pos >= len(ts) || !ts[pos].ident() {
		return "", "", pos, errors.New("expected an object identifier")
	}
	name := ts[pos].value
	pos++
	if pos < len(ts) && ts[pos].kind == '.' {
		if pos+1 >= len(ts) || !ts[pos+1].ident() {
			return "", "", pos, errors.New("incomplete qualified identifier")
		}
		return name, ts[pos+1].value, pos + 2, nil
	}
	return db, name, pos, nil
}

// Native parsing proves the compound statement boundary. The original header
// tokens independently bind schema, kind, name and DEFINER without splitting
// quoted names or extracting statements from the program body.
func mysqlObjectHeader(sql string, ref ObjectRef, opts AnalyzeOptions) (mysqlDDLHeader, error) {
	var h mysqlDDLHeader
	mode, err := mysql.GetSQLMode(opts.MySQLSQLMode)
	if err != nil {
		return h, err
	}
	for _, tok := range lexMode(sql, dialect.MySQL, mode.HasANSIQuotesMode(), mode.HasNoBackslashEscapesMode()) {
		if tok.kind == 'c' {
			if strings.HasPrefix(tok.text, "/*!") || strings.HasPrefix(tok.text, "/*M!") {
				return h, errors.New("use an explicit native definition without executable comments")
			}
			continue
		}
		h.tokens = append(h.tokens, tok)
	}
	ts := h.tokens
	if len(ts) < 3 || !ts[0].is("CREATE") && !ts[0].is("ALTER") {
		return h, errors.New("expected one CREATE or ALTER object definition")
	}
	h.verb = strings.ToUpper(ts[0].value)
	h.verbToken = ts[0]
	pos := 1
	if pos+1 < len(ts) && ts[pos].is("OR") && ts[pos+1].is("REPLACE") {
		h.replace = true
		pos += 2
	}
	if pos < len(ts) && ts[pos].is("DEFINER") {
		h.definerStart = ts[pos].start
		if pos+2 >= len(ts) || ts[pos+1].kind != '=' {
			return h, errors.New("invalid DEFINER")
		}
		pos += 2
		switch {
		case ts[pos].is("CURRENT_USER"):
			pos++
			if pos+1 < len(ts) && ts[pos].kind == '(' && ts[pos+1].kind == ')' {
				pos += 2
			}
		default:
			if !ts[pos].ident() && ts[pos].kind != 's' {
				return h, errors.New("invalid DEFINER account")
			}
			pos++
			if pos+1 < len(ts) && ts[pos].kind == '@' {
				pos += 2
			}
		}
		h.definer = sql[h.definerStart:ts[pos-1].end]
	}
	if pos >= len(ts) || !ts[pos].is(string(ref.Kind)) {
		return h, errors.New("definition kind differs from the selected object")
	}
	h.kind = ref.Kind
	h.kindToken = ts[pos]
	pos++
	if pos >= len(ts) || ts[pos].is("IF") {
		return h, errors.New("provide a strict object definition without IF EXISTS or IF NOT EXISTS")
	}
	h.nameToken = ts[pos]
	db, name, next, err := mysqlObjectName(ts, pos, ref.Database)
	if err != nil || db != ref.Database || name != ref.Name {
		return h, errors.New("definition identity differs from the selected object")
	}
	h.next = next
	if ref.Kind == TriggerKind {
		start := next
		for start < len(ts) && !ts[start].is("ON") {
			start++
		}
		tableDB, table, _, err := mysqlObjectName(ts, start+1, ref.Database)
		if err != nil || tableDB != ref.Database || table != ref.Table {
			return h, errors.New("trigger definition targets a different table")
		}
	}
	return h, nil
}

func mysqlQualified(ref ObjectRef) string {
	return mysqlObjectIdent(ref.Database) + "." + mysqlObjectIdent(ref.Name)
}
func mysqlObjectIdent(name string) string { return quoteIdentifier(name, dialect.MySQL, '`') }

// Verify an explicit ordering anchor before a replacement can remove the
// original trigger. Both the anchor and its table must be visible here.
func checkMySQLTriggerAnchor(ctx context.Context, conn *sql.Conn, p MySQLObjectProfile, ref ObjectRef, h mysqlDDLHeader) error {
	t := h.tokens
	for i := h.next; i+3 < len(t); i++ {
		if !t[i].is("FOR") || !t[i+1].is("EACH") || !t[i+2].is("ROW") {
			continue
		}
		if !t[i+3].is("FOLLOWS") && !t[i+3].is("PRECEDES") {
			return nil
		}
		if i+4 >= len(t) || !t[i+4].ident() || t[i+4].value == ref.Name {
			return errors.New("choose another trigger as the ordering anchor")
		}
		anchor := ref
		anchor.Name = t[i+4].value
		props, err := myObjectProperties(ctx, conn, p, anchor)
		if err != nil {
			return err
		}
		if !t[h.next].is(myString(props, "action_timing")) {
			return errors.New("trigger ordering requires the same timing")
		}
		events, _ := props["events"].([]string)
		for j := h.next + 1; j < i && !t[j].is("ON"); j++ {
			if !t[j].is("OR") && !slices.Contains(events, strings.ToUpper(t[j].value)) {
				return errors.New("trigger ordering requires the same event")
			}
		}
		return nil
	}
	return nil
}

func mysqlRestoreTriggerOrder(source string, h mysqlDDLHeader, detail ObjectDetail, confirmReorder bool) (string, error) {
	previous, _ := detail.Properties["previous_trigger"].(string)
	next, _ := detail.Properties["next_trigger"].(string)
	if h.next+1 >= len(h.tokens) || !h.tokens[h.next].is(myString(detail.Properties, "action_timing")) {
		return source, nil
	}
	var proposed []string
	for i := h.next + 1; i < len(h.tokens) && !h.tokens[i].is("ON"); i++ {
		if !h.tokens[i].is("OR") {
			proposed = append(proposed, strings.ToUpper(h.tokens[i].value))
		}
	}
	existing, _ := detail.Properties["events"].([]string)
	slices.Sort(proposed)
	existing = slices.Clone(existing)
	slices.Sort(existing)
	if !slices.Equal(existing, proposed) {
		return source, nil // An explicit timing/event change selects a new ordering group.
	}
	for i := h.next; i+2 < len(h.tokens); i++ {
		t := h.tokens
		if t[i].is("FOR") && t[i+1].is("EACH") && t[i+2].is("ROW") {
			if i+3 < len(t) && (t[i+3].is("FOLLOWS") || t[i+3].is("PRECEDES")) {
				if confirmReorder {
					return "", errors.New("choose either an ordering anchor or database-default ordering")
				}
				return source, nil
			}
			if mixed, _ := detail.Properties["trigger_order_requires_choice"].(bool); mixed {
				if !confirmReorder {
					return "", errors.New("overlapping trigger events require an explicit FOLLOWS or PRECEDES anchor or acceptance of the database-default ordering")
				}
				return source, nil
			}
			relation, name := "FOLLOWS", previous
			if name == "" {
				relation, name = "PRECEDES", next
			}
			if name == "" {
				return source, nil
			}
			at := t[i+2].end
			return source[:at] + " " + relation + " " + mysqlObjectIdent(name) + source[at:], nil
		}
	}
	return "", errors.New("trigger requires FOR EACH ROW")
}

func mysqlPlanDDL(req ObjectPlanRequest, profile MySQLObjectProfile, opts AnalyzeOptions, detail ObjectDetail, old ObjectDefinition, obOwner bool) ([]ObjectStep, error) {
	source := req.Definition
	header, err := mysqlObjectHeader(source, req.Ref, opts)
	if err != nil {
		return nil, err
	}
	if req.Action == "create" {
		if header.verb != "CREATE" || header.replace {
			return nil, errors.New("first creation requires a strict CREATE")
		}
		return []ObjectStep{{SQL: source}}, nil
	}
	if header.verb == "ALTER" && req.Ref.Kind == TriggerKind {
		return nil, ErrCatalogUnsupported
	}
	if header.verb == "ALTER" && req.Ref.Kind.IsRoutine() {
		return []ObjectStep{{SQL: source}}, nil
	}
	original, err := mysqlObjectHeader(old.Text, req.Ref, opts)
	if err != nil {
		return nil, errors.New("the native original definition cannot be verified for replacement")
	}
	if req.Ref.Kind == TriggerKind {
		source, err = mysqlRestoreTriggerOrder(source, header, detail, req.ConfirmTriggerReorder)
		if err != nil {
			return nil, err
		}
	}
	if profile.Product == dialect.OceanBaseMySQL && req.Ref.Kind == TriggerKind {
		if !obOwner || old.Environment["collation_connection"] == "" || old.Environment["database_collation"] == "" ||
			(header.definer != "" && header.definer != original.definer) {
			return nil, ErrCatalogUnsupported
		}
		// The native default preserves this owner's identity without requiring
		// a global privilege for the explicit DEFINER emitted by SHOW CREATE.
		if header.definer != "" {
			source = source[:header.definerStart] + source[header.definerStart+len(header.definer):]
		}
	} else if header.definer == "" && original.definer != "" {
		at := header.kindToken.start
		source = source[:at] + original.definer + " " + source[at:]
	}
	if req.Ref.Kind == "event" {
		// RENAME has a distinct target and is admitted only as an explicit action.
		for i := header.next; i < len(header.tokens); i++ {
			if header.tokens[i].is("DO") {
				break
			}
			if header.tokens[i].is("RENAME") {
				return nil, errors.New("use the event Rename action")
			}
		}
		if header.verb == "CREATE" {
			source = source[:header.verbToken.start] + "ALTER" + source[header.verbToken.end:]
		}
		return []ObjectStep{{SQL: source}}, nil
	}
	if profile.Product == dialect.MariaDB {
		if !header.replace {
			at := header.verbToken.end
			source = source[:at] + " OR REPLACE" + source[at:]
		}
		return []ObjectStep{{SQL: source, ReplacesExisting: true}}, nil
	}
	if header.replace {
		return nil, errors.New("replacement requires a strict CREATE after DROP")
	}
	return []ObjectStep{{SQL: "DROP " + strings.ToUpper(string(req.Ref.Kind)) + " " + mysqlQualified(req.Ref)}, {SQL: source}}, nil
}

func mysqlEventAlter(ref ObjectRef, def ObjectDefinition, opts AnalyzeOptions, suffix string, obOwner bool) (ObjectStep, error) {
	if def.Status == "restricted" {
		return ObjectStep{}, ErrCatalogForbidden
	}
	if def.Status == "unsupported" {
		if !obOwner {
			return ObjectStep{}, ErrCatalogUnsupported
		}
		return ObjectStep{SQL: "ALTER EVENT " + mysqlQualified(ref) + " " + suffix}, nil
	}
	h, err := mysqlObjectHeader(def.Text, ref, opts)
	if err != nil || h.definer == "" {
		return ObjectStep{}, errors.New("an original native event DEFINER is required")
	}
	return ObjectStep{SQL: fmt.Sprintf("ALTER %s EVENT %s %s", h.definer, mysqlQualified(ref), suffix)}, nil
}
