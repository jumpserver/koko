package sqlservice

import (
	"errors"
	"fmt"
	"github.com/jumpserver/koko/pkg/chen/dialect"
	"github.com/pingcap/tidb/pkg/parser/mysql"
	"strings"
)

func mysqlArgumentLiteral(value string, opts AnalyzeOptions) string {
	mode, _ := mysql.GetSQLMode(opts.MySQLSQLMode)
	if !mode.HasNoBackslashEscapesMode() {
		value = strings.ReplaceAll(value, "\\", "\\\\")
	}
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func mysqlInvoke(req ObjectPlanRequest, detail ObjectDetail, profile MySQLObjectProfile, opts AnalyzeOptions, mask func(string, string) (string, bool)) ([]ObjectStep, error) {
	var parts, shown, policy []string
	var args, initArgs []any
	var vars, init, initDisplay, initPolicy, outputs []string
	omitted := false
	input := 0
	for _, par := range detail.Parameters {
		if par.Position == 0 || par.Mode == "RETURN" {
			continue
		}
		placeholder, display, actual := "NULL", "NULL", "NULL"
		var value any
		if par.Mode != "OUT" {
			if input >= len(req.Arguments) {
				return nil, errors.New("supply every input argument or explicitly select a default")
			}
			arg := req.Arguments[input]
			input++
			if arg.ActualType != nil {
				return nil, errors.New("routine input types are fixed by the database")
			}
			switch arg.Mode {
			case "default":
				if !par.HasDefault || arg.Value != nil {
					return nil, errors.New("the selected parameter has no confirmed default")
				}
				omitted = true
				continue
			case "null":
				if arg.Value != nil {
					return nil, errors.New("NULL does not accept a value")
				}
			case "value":
				if arg.Value == nil {
					return nil, errors.New("provide an input value")
				}
				value = *arg.Value
				placeholder = "?"
				actual = mysqlArgumentLiteral(*arg.Value, opts)
				display = actual
				if mask != nil {
					if v, hidden := mask(par.Name, *arg.Value); hidden {
						display = mysqlArgumentLiteral(v, opts)
					}
				}
			default:
				return nil, errors.New("unknown argument mode")
			}
		}
		if omitted {
			return nil, errors.New("only trailing MariaDB procedure defaults may be omitted")
		}
		switch par.Mode {
		case "IN", "":
			if placeholder == "?" {
				args = append(args, value)
			}
			parts = append(parts, placeholder)
			shown = append(shown, display)
			policy = append(policy, actual)
		case "OUT", "INOUT":
			if req.Ref.Kind == FunctionKind && (profile.Product != dialect.MariaDB || profile.Version.Major < 10 || profile.Version.Major == 10 && profile.Version.Minor < 8) {
				return nil, ErrCatalogUnsupported
			}
			if len(vars) >= 100 || len(req.BindingID) < 16 {
				return nil, errors.New("too many output parameters or missing console binding")
			}
			v := fmt.Sprintf("@chen_%s_%d", req.BindingID, len(vars)+1)
			vars = append(vars, v)
			init = append(init, v+" = "+placeholder)
			initDisplay = append(initDisplay, v+" = "+display)
			initPolicy = append(initPolicy, v+" = "+actual)
			if placeholder == "?" {
				initArgs = append(initArgs, value)
			}
			parts = append(parts, v)
			shown = append(shown, v)
			policy = append(policy, v)
			name := par.Name
			if name == "" {
				name = fmt.Sprintf("OUT%d", par.Position)
			}
			outputs = append(outputs, v+" AS "+mysqlObjectIdent(name))
		default:
			return nil, ErrCatalogUnsupported
		}
	}
	if input != len(req.Arguments) {
		return nil, errors.New("unexpected routine arguments")
	}
	prefix := "CALL "
	role := "business_result"
	steps := []ObjectStep{}
	if len(vars) > 0 {
		steps = append(steps, ObjectStep{SQL: "SET " + strings.Join(initDisplay, ", "), PolicySQL: "SET " + strings.Join(initPolicy, ", "), BoundSQL: "SET " + strings.Join(init, ", "), Args: initArgs, SessionVariables: true})
	}
	if req.Ref.Kind == FunctionKind {
		prefix = "SELECT "
		role = "function_return"
		if len(vars) > 0 {
			r := "@chen_" + req.BindingID + "_result"
			vars = append(vars, r)
			prefix = "SET " + r + " = "
			role = ""
			outputs = append([]string{r + " AS " + mysqlObjectIdent("function_return")}, outputs...)
		}
	}
	prefix += mysqlQualified(req.Ref) + "("
	steps = append(steps, ObjectStep{SQL: prefix + strings.Join(shown, ", ") + ")", PolicySQL: prefix + strings.Join(policy, ", ") + ")", BoundSQL: prefix + strings.Join(parts, ", ") + ")", Args: args, ResultRole: role})
	if len(outputs) > 0 {
		steps = append(steps, ObjectStep{SQL: "SELECT " + strings.Join(outputs, ", "), ResultRole: "output_parameters"})
		clear := make([]string, len(vars))
		for i, v := range vars {
			clear[i] = v + " = NULL"
		}
		steps = append(steps, ObjectStep{SQL: "SET " + strings.Join(clear, ", "), SessionVariables: true, Cleanup: true})
	}
	return steps, nil
}
