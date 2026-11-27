package sqlservice

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strconv"
	"strings"

	pg "github.com/pganalyze/pg_query_go/v6"
)

func pgDDLNames(nodes []*pg.Node) ([]string, error) {
	if len(nodes) < 1 || len(nodes) > 2 {
		return nil, errors.New("expected a PostgreSQL name with an optional schema")
	}
	names := make([]string, len(nodes))
	for i, n := range nodes {
		name := n.GetString_().GetSval()
		if !validObjectIdentifier(name, 63) {
			return nil, errors.New("invalid PostgreSQL identifier")
		}
		names[i] = name
	}
	return names, nil
}

// Declarations are parsed locally but names (including domains and array types)
// resolve using this console's actual PostgreSQL namespace.
func pgArgumentType(ctx context.Context, conn *sql.Conn, t *pg.TypeName) (ObjectTypeRef, error) {
	var ref ObjectTypeRef
	if t == nil || t.PctType {
		return ref, errors.New("specify a concrete input parameter type")
	}
	names, err := pgDDLNames(t.Names)
	if err != nil {
		return ref, err
	}
	for i := range names {
		names[i] = pgIdent(names[i])
	}
	typeName := strings.Join(names, ".")
	if len(t.ArrayBounds) > 0 {
		typeName += "[]"
	}
	err = conn.QueryRowContext(ctx, `SELECT n.nspname,t.typname FROM pg_catalog.pg_type t JOIN pg_catalog.pg_namespace n ON n.oid=t.typnamespace
	WHERE t.oid=pg_catalog.to_regtype($1) AND pg_catalog.has_type_privilege(t.oid,'USAGE')`, typeName).Scan(&ref.Schema, &ref.Name)
	return ref, err
}

func pgObjectDDL(ctx context.Context, conn *sql.Conn, req ObjectPlanRequest, detail ObjectDetail, version int) (ObjectRef, []ObjectStep, error) {
	ref := req.Ref
	tree, err := pg.Parse(req.Definition)
	if err != nil {
		return ref, nil, err
	}
	if len(tree.Stmts) != 1 {
		return ref, nil, errors.New("the definition must contain exactly one top-level object command")
	}
	node := tree.Stmts[0].Stmt
	editing := req.Action == "edit"
	if alter := node.GetAlterFunctionStmt(); alter != nil {
		f := alter.Func
		if !editing || f == nil || f.ArgsUnspecified || !(ref.Kind == ProcedureKind && alter.Objtype == pg.ObjectType_OBJECT_PROCEDURE || ref.Kind == FunctionKind && alter.Objtype == pg.ObjectType_OBJECT_FUNCTION) {
			return ref, nil, errors.New("ALTER requires the selected kind and a complete routine signature")
		}
		names, err := pgDDLNames(f.Objname)
		if err != nil || !slices.Equal(names, []string{ref.Schema, ref.Name}) {
			return ref, nil, errors.New("ALTER target differs from the selected routine")
		}
		types := make([]ObjectTypeRef, 0, len(f.Objargs))
		for _, arg := range f.Objargs {
			typ, err := pgArgumentType(ctx, conn, arg.GetTypeName())
			if err != nil {
				return ref, nil, err
			}
			types = append(types, typ)
		}
		if !slices.Equal(types, ref.InputTypes) {
			return ref, nil, errors.New("ALTER signature differs from the selected routine")
		}
		return ref, []ObjectStep{{SQL: req.Definition}}, nil
	}
	if f := node.GetCreateFunctionStmt(); f != nil {
		names, err := pgDDLNames(f.Funcname)
		if err != nil {
			return ref, nil, err
		}
		if !ref.Kind.IsRoutine() || f.IsProcedure != (ref.Kind == ProcedureKind) || !slices.Equal(names, []string{ref.Schema, ref.Name}) || f.Replace != editing {
			return ref, nil, errors.New("use the selected qualified routine and the matching CREATE or CREATE OR REPLACE action")
		}
		types := []ObjectTypeRef{}
		for _, n := range f.Parameters {
			param := n.GetFunctionParameter()
			if param == nil {
				return ref, nil, errors.New("invalid routine parameter")
			}
			if param.Mode == pg.FunctionParameterMode_FUNC_PARAM_OUT || param.Mode == pg.FunctionParameterMode_FUNC_PARAM_TABLE {
				continue
			}
			typ, err := pgArgumentType(ctx, conn, param.ArgType)
			if err != nil {
				return ref, nil, err
			}
			types = append(types, typ)
		}
		if (editing || ref.InputTypes != nil) && !slices.Equal(ref.InputTypes, types) {
			return ref, nil, errors.New("input signature differs from the selected routine")
		}
		ref.InputTypes = types
		return ref, []ObjectStep{{SQL: req.Definition}}, nil
	}
	t := node.GetCreateTrigStmt()
	if t == nil || ref.Kind != "trigger" || t.Trigname != ref.Name || t.Relation.GetSchemaname() != ref.Schema || t.Relation.GetRelname() != ref.Table || t.Relation.GetCatalogname() != "" || t.Replace && !editing {
		return ref, nil, errors.New("use the selected trigger and its schema-qualified table")
	}
	if detail.Properties["is_clone"] == true {
		return ref, nil, ErrCatalogUnsupported
	}
	var valid bool
	names, err := pgDDLNames(t.Funcname)
	if err != nil {
		return ref, nil, err
	}
	for i := range names {
		names[i] = pgIdent(names[i])
	}
	err = conn.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_catalog.pg_proc p JOIN pg_catalog.pg_namespace n ON n.oid=p.pronamespace
	WHERE p.oid=pg_catalog.to_regprocedure($1) AND p.pronargs=0 AND p.prorettype='pg_catalog.trigger'::pg_catalog.regtype
	AND pg_catalog.has_schema_privilege(n.oid,'USAGE') AND pg_catalog.has_function_privilege(p.oid,'EXECUTE'))`, strings.Join(names, ".")+"()").Scan(&valid)
	if err != nil {
		return ref, nil, err
	}
	if !valid {
		return ref, nil, errors.New("select an executable zero-argument trigger function")
	}
	if !editing {
		return ref, []ObjectStep{{SQL: req.Definition}}, nil
	}
	// PG 14+ supports native replacement of ordinary triggers. Preserve the
	// operator's body and quoting, adding only the missing OR REPLACE token.
	var steps []ObjectStep
	if version >= 140000 && !t.Isconstraint && detail.Properties["is_constraint"] == false {
		source := req.Definition
		if !t.Replace {
			tokens, err := pg.Scan(source)
			if err != nil {
				return ref, nil, err
			}
			for _, token := range tokens.Tokens {
				if token.Token == pg.Token_CREATE {
					source = source[:token.End] + " OR REPLACE" + source[token.End:]
					break
				}
			}
		}
		steps = []ObjectStep{{SQL: source}}
	} else {
		if t.Replace {
			return ref, nil, errors.New("provide a strict CREATE definition for transactional trigger replacement")
		}
		steps = []ObjectStep{{SQL: "DROP TRIGGER " + pgObjectTarget(ref) + " RESTRICT"}, {SQL: req.Definition}}
	}
	// Native CREATE OR REPLACE also resets tgenabled to origin (O).
	// Restore replica/always/disabled atomically with the new definition.
	if state, _ := detail.Properties["state"].(string); state != "O" {
		restore, err := pgTriggerState(ref, state)
		if err != nil {
			return ref, nil, err
		}
		steps = append(steps, restore)
	}
	return ref, steps, nil
}

func pgInvoke(ctx context.Context, conn *sql.Conn, detail ObjectDetail, args []ObjectArgument, mask func(string, string) (string, bool)) (ObjectStep, error) {
	if !slices.Contains(detail.Actions, "invoke") {
		return ObjectStep{}, ErrCatalogForbidden
	}
	var inputs []any
	var sqlArgs, policyArgs, displayArgs []string
	index := 0
	skipped, ambiguous := false, false
	for _, param := range detail.Parameters {
		mode := param.Mode
		if mode == "TABLE" || mode == "OUT" && detail.Ref.Kind == FunctionKind {
			continue
		}
		typ := param.Type
		expression, display := "NULL", "NULL"
		previewLiteral := ""
		if mode != "OUT" {
			if index >= len(args) {
				return ObjectStep{}, errors.New("supply every input argument or explicitly select its default")
			}
			arg := args[index]
			index++
			if arg.Mode == "default" {
				if !param.HasDefault || arg.Value != nil || arg.ActualType != nil {
					return ObjectStep{}, errors.New("argument has no usable default")
				}
				skipped, ambiguous = true, true
				continue
			}
			if param.TypeKind == "p" {
				if arg.ActualType == nil {
					return ObjectStep{}, errors.New("select a concrete type for the polymorphic argument")
				}
				if !strings.HasPrefix(typ.Name, "any") || typ.Schema != "pg_catalog" {
					return ObjectStep{}, ErrCatalogUnsupported
				}
				actual := *arg.ActualType
				var allowed bool
				err := conn.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_catalog.pg_type t JOIN pg_catalog.pg_namespace n ON n.oid=t.typnamespace WHERE n.nspname=$1 AND t.typname=$2 AND t.typtype<>'p' AND pg_catalog.has_type_privilege(t.oid,'USAGE'))`, actual.Schema, actual.Name).Scan(&allowed)
				if err != nil {
					return ObjectStep{}, err
				}
				if !allowed {
					return ObjectStep{}, errors.New("the concrete type is unavailable")
				}
				typ = actual
				ambiguous = true
			} else if arg.ActualType != nil {
				return ObjectStep{}, errors.New("only polymorphic arguments accept a concrete type")
			}
			switch arg.Mode {
			case "null":
				if arg.Value != nil {
					return ObjectStep{}, errors.New("NULL does not accept a value")
				}
			case "value":
				if arg.Value == nil || strings.ContainsRune(*arg.Value, 0) {
					return ObjectStep{}, errors.New("provide a valid input value")
				}
				inputs = append(inputs, *arg.Value)
				expression = "$" + strconv.Itoa(len(inputs)) + "::pg_catalog.text"
				escape := strings.NewReplacer("\\", "\\\\", "'", "''")
				display = "E'" + escape.Replace(*arg.Value) + "'"
				if mask != nil {
					if value, hidden := mask(param.Name, *arg.Value); hidden {
						previewLiteral = "E'" + escape.Replace(value) + "'"
					}
				}
			default:
				return ObjectStep{}, errors.New("invalid argument mode")
			}
		}
		suffix := "::" + pgTypeName(typ)
		prefix := ""
		if mode == "VARIADIC" {
			prefix = "VARIADIC "
		}
		if skipped {
			if param.Name == "" || mode == "VARIADIC" {
				return ObjectStep{}, errors.New("omitted defaults require named following arguments")
			}
			prefix = pgIdent(param.Name) + " => "
		}
		if previewLiteral == "" {
			previewLiteral = display
		}
		sqlArgs = append(sqlArgs, prefix+expression+suffix)
		policyArgs = append(policyArgs, prefix+display+suffix)
		displayArgs = append(displayArgs, prefix+previewLiteral+suffix)
	}
	if index != len(args) {
		return ObjectStep{}, errors.New("unexpected routine argument")
	}
	if ambiguous {
		var unique bool
		err := conn.QueryRowContext(ctx, `SELECT count(*)=1 FROM pg_catalog.pg_proc p JOIN pg_catalog.pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname=$1 AND p.proname=$2`, detail.Ref.Schema, detail.Ref.Name).Scan(&unique)
		if err != nil {
			return ObjectStep{}, err
		}
		if !unique {
			return ObjectStep{}, errors.New("the shortened or polymorphic call may resolve to another overload; supply the full declared types")
		}
	}
	prefix, role := "SELECT * FROM ", "function_return"
	if detail.Ref.Kind == ProcedureKind {
		prefix, role = "CALL ", "output_parameters"
	}
	prefix += pgIdent(detail.Ref.Schema) + "." + pgIdent(detail.Ref.Name) + "("
	return ObjectStep{SQL: prefix + strings.Join(displayArgs, ", ") + ")", PolicySQL: prefix + strings.Join(policyArgs, ", ") + ")", BoundSQL: prefix + strings.Join(sqlArgs, ", ") + ")", Args: inputs, ResultRole: role}, nil
}
