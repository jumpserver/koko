package sqlservice

import (
	"errors"
	"github.com/jumpserver/koko/pkg/chen/dialect"
	"strings"
)

// The pinned MySQL grammar lacks schema-qualified names for CREATE TRIGGER
// and EVENT and some MariaDB syntax. Project just those verified header spans
// for boundary analysis. Authorization and dispatch always use the original SQL.
func mysqlObjectParserSQL(source string, h mysqlDDLHeader, p MySQLObjectProfile) (string, error) {
	b := []byte(source)
	hide := func(a, z int) {
		for i := a; i < z; i++ {
			if b[i] != '\r' && b[i] != '\n' {
				b[i] = ' '
			}
		}
	}
	t := h.tokens
	if h.replace && p.Product == dialect.MariaDB {
		hide(t[1].start, t[2].end)
	}
	if (h.kind == TriggerKind || h.kind == EventKind) && t[h.next-1].start != h.nameToken.start {
		hide(h.nameToken.start, t[h.next-1].start)
	}
	if h.kind == EventKind {
		for i := h.next; i+3 < len(t) && !t[i].is("DO"); i++ {
			if t[i].is("RENAME") && t[i+1].is("TO") && t[i+3].kind == '.' {
				hide(t[i+2].start, t[i+3].end)
			}
		}
	}
	if p.Product == dialect.MariaDB && h.kind == TriggerKind && h.next+3 < len(t) && t[h.next+2].is("OR") {
		if p.Version.Major < 12 {
			return "", ErrCatalogUnsupported
		}
		for i := h.next + 2; i+1 < len(t) && t[i].is("OR"); i += 2 {
			if !t[i+1].is("UPDATE") && !t[i+1].is("DELETE") && !t[i+1].is("INSERT") {
				return "", errors.New("invalid trigger event")
			}
			hide(t[i].start, t[i+1].end)
		}
	}
	if h.kind.IsRoutine() && h.verb == "CREATE" {
		if h.next >= len(t) || t[h.next].kind != '(' {
			return "", errors.New("a stored routine declaration requires a parameter list")
		}
		open, start, depth := h.next, h.next+1, 0
		for i := open + 1; i < len(t); i++ {
			if t[i].kind == ';' {
				return "", errors.New("unexpected statement in parameter list")
			}
			if t[i].kind == '(' {
				depth++
				continue
			}
			if t[i].kind == ')' && depth > 0 {
				depth--
				continue
			}
			if depth > 0 || t[i].kind != ',' && t[i].kind != ')' {
				continue
			}
			if p.Product == dialect.MariaDB && start < i {
				if h.kind == FunctionKind && (t[start].is("OUT") || t[start].is("INOUT") || t[start].is("IN")) {
					if !t[start].is("IN") && (p.Version.Major < 10 || p.Version.Major == 10 && p.Version.Minor < 8) {
						return "", ErrCatalogUnsupported
					}
					hide(t[start].start, t[start].end)
				}
				for j := start; j < i; j++ {
					if t[j].is("DEFAULT") {
						if h.kind != ProcedureKind || p.Version.Major < 11 || p.Version.Major == 11 && p.Version.Minor < 8 {
							return "", ErrCatalogUnsupported
						}
						if j+1 == i {
							return "", errors.New("missing parameter default expression")
						}
						hide(t[j].start, t[i].start)
						break
					}
				}
			}
			if t[i].kind == ')' {
				break
			}
			start = i + 1
		}
	}
	return string(b), nil
}

// Default positions for MariaDB releases supporting defaults before exposing
// PARAMETER_DEFAULT in INFORMATION_SCHEMA. Call only on an authorized definition.
func mysqlRoutineDefaultPositions(source string, ref ObjectRef, opts AnalyzeOptions) (map[int]bool, error) {
	h, err := mysqlObjectHeader(source, ref, opts)
	if err != nil {
		return nil, err
	}
	if !ref.Kind.IsRoutine() || h.next >= len(h.tokens) || h.tokens[h.next].kind != '(' {
		return nil, errors.New("expected a native routine definition")
	}
	pos, depth := 1, 0
	defaults := map[int]bool{}
	for _, t := range h.tokens[h.next+1:] {
		switch t.kind {
		case '(':
			depth++
		case ')':
			if depth == 0 {
				return defaults, nil
			}
			depth--
		case ',':
			if depth == 0 {
				pos++
			}
		default:
			if depth == 0 && t.is("DEFAULT") {
				defaults[pos] = true
			}
		}
	}
	return nil, errors.New("incomplete routine parameter list")
}

func AnalyzeObjectCommands(d dialect.ID, cmd ObjectCommands, opts AnalyzeOptions) (Analysis, error) {
	var combined Analysis
	for _, step := range cmd.Steps {
		source := step.SQL
		parserDialect := d
		if d.UsesMySQLSyntax() {
			parserDialect = dialect.MySQL // No raw/vendor fallback for stored programs.
			if step.ParserSQL != "" {
				source = step.ParserSQL
			}
		}
		out, err := AnalyzeWithOptions(parserDialect, source, opts)
		if err != nil {
			return Analysis{}, err
		}
		if len(out.Statements) != 1 {
			return Analysis{}, errors.New("the object action must have exactly one top-level statement per step")
		}
		s := out.Statements[0]
		if d.UsesMySQLSyntax() && strings.HasPrefix(s.Kind, "create") && step.ParserSQL != "" && s.Kind != "createfunction" && s.Kind != "createprocedure" && s.Kind != "createtrigger" && s.Kind != "createevent" {
			return Analysis{}, errors.New("unexpected object command")
		}
		s.SQL = step.SQL
		combined.Statements = append(combined.Statements, s)
	}
	return combined, nil
}
