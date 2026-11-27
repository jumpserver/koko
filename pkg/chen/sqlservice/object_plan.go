package sqlservice

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/jumpserver/koko/pkg/chen/dialect"
)

type ObjectArgument struct {
	Mode       string         `json:"mode"`
	Value      *string        `json:"value,omitempty"`
	ActualType *ObjectTypeRef `json:"actual_type,omitempty"`
}

type ObjectResultColumn struct {
	Name string        `json:"name"`
	Type ObjectTypeRef `json:"type"`
}

type CursorFetch struct {
	ResultID string `json:"result_id"`
	Row      int    `json:"row"`
	Column   int    `json:"column"`
	Limit    int    `json:"limit"`
}

type ObjectPlanRequest struct {
	BindingID     string               `json:"-"` // Random console-owned namespace for transient output bindings.
	Cursor        *CursorFetch         `json:"cursor,omitempty"`
	ResultColumns []ObjectResultColumn `json:"result_columns,omitempty"`
	Action        string               `json:"action"`
	Ref           ObjectRef            `json:"ref"`
	OID           uint32               `json:"oid,omitempty"`
	Baseline      string               `json:"baseline,omitempty"`
	Definition    string               `json:"definition,omitempty"`
	NewName       string               `json:"new_name,omitempty"`
	State         string               `json:"state,omitempty"`
	Arguments     []ObjectArgument     `json:"arguments,omitempty"`
	ConfirmTriggerAppend bool          `json:"confirm_trigger_append,omitempty"`
}

type ObjectStep struct {
	ParserSQL        string `json:"-"`
	Cleanup          bool   `json:"-"`
	SessionVariables bool   `json:"-"`
	ReplacesExisting bool   `json:"-"` // A rejected native replacement can still remove the old object.
	PolicySQL        string `json:"-"`
	VerifyAfter      bool   `json:"-"`
	SQL              string `json:"sql"` // Actual command with typed values rendered for preview and ACL.
	BoundSQL         string `json:"-"`
	Args             []any  `json:"-"`
	ResultRole       string `json:"result_role,omitempty"`
}

type ObjectCommands struct {
	ExecutionUser string
	Recovery      *ObjectRecovery
	Ref           ObjectRef
	Steps         []ObjectStep
	Fingerprint   string
}

var ErrObjectChanged = errors.New("object or console context changed; reload the object and preview again")
var ErrObjectAlreadyExists = fmt.Errorf("the selected object already exists: %w", ErrObjectChanged)

// A rebuild on the pinned connection after approval verifies the same effective
// identity, native object and exact commands before the worker may send them.
func BuildObjectCommands(ctx context.Context, conn *sql.Conn, d dialect.ID, req ObjectPlanRequest, authorize func(string) error, mask func(string, string) (string, bool)) (ObjectCommands, error) {
	if d.UsesMySQLSyntax() {
		return buildMySQLCommands(ctx, conn, d, req, authorize, mask)
	}
	if d != dialect.PostgreSQL {
		return ObjectCommands{}, ErrCatalogUnsupported
	}
	if !ValidObjectRef(req.Ref) || !validObjectIdentifier(req.Ref.Name, 63) || len(req.Arguments) > 100 || DocumentExceedsLimit(req.Definition) {
		return ObjectCommands{}, errors.New("invalid object identity or document")
	}
	if err := catalogScopeAccess(ctx, conn, d, req.Ref.Database, req.Ref.Schema); err != nil {
		return ObjectCommands{}, err
	}
	version, err := PostgreSQLCatalogVersion(ctx, conn)
	if err != nil {
		return ObjectCommands{}, err
	}
	if version < 100000 || req.Ref.Kind == ProcedureKind && version < 110000 {
		return ObjectCommands{}, ErrCatalogUnsupported
	}
	out := ObjectCommands{Ref: req.Ref}
	var environment string
	err = conn.QueryRowContext(ctx, `SELECT current_user, pg_catalog.json_build_array(session_user,pg_catalog.current_schemas(true),
	 pg_catalog.current_setting('standard_conforming_strings'),pg_catalog.current_setting('TimeZone'),pg_catalog.current_setting('DateStyle'),pg_catalog.current_setting('server_version_num'))::text`).Scan(&out.ExecutionUser, &environment)
	if err != nil {
		return ObjectCommands{}, err
	}

	var detail ObjectDetail
	var baseline, instance string
	if req.Ref.Kind == TriggerKind {
		// The Catalog normally hides non-table triggers. Creation must apply
		// the same boundary even when no trigger yet exists. Include the parent
		// OID so that replacing the table invalidates an outstanding preview.
		err = conn.QueryRowContext(ctx, `SELECT c.oid::text FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace
		 WHERE n.nspname=$1 AND c.relname=$2 AND c.relkind IN ('r','p','f') AND pg_catalog.has_schema_privilege(n.oid,'USAGE')
		 AND (pg_catalog.pg_has_role(c.relowner,'USAGE') OR pg_catalog.has_table_privilege(c.oid,'TRIGGER'))`, req.Ref.Schema, req.Ref.Table).Scan(&instance)
		if errors.Is(err, sql.ErrNoRows) {
			return out, ErrCatalogNotFound
		}
		if err != nil {
			return out, err
		}
	}
	if req.Action != "create" {
		if req.OID == 0 {
			return out, errors.New("object instance required")
		}
		detail, err = PostgreSQLObjectDetail(ctx, conn, req.Ref, req.OID)
		if err != nil {
			return out, err
		}
		capability := req.Action
		if capability == "readDefinition" {
			capability = "definition"
		}
		if capability == "rename" {
			capability = "edit"
		}
		if !slices.Contains(detail.Actions, capability) {
			return out, ErrCatalogForbidden
		}
		if req.Action == "invoke" {
			// EXECUTE-only users cannot read the body. The row version still
			// detects replacement of the selected routine while approval is pending.
			err = conn.QueryRowContext(ctx, `SELECT xmin::text FROM pg_catalog.pg_proc WHERE oid=$1 AND pg_catalog.has_function_privilege(oid,'EXECUTE')`, detail.OID).Scan(&instance)
			if err != nil {
				return out, err
			}
		}
		if req.Action != "invoke" && req.Action != "readDefinition" {
			def, err := PostgreSQLObjectDefinition(ctx, conn, req.Ref, req.OID, authorize)
			if err != nil {
				return out, err
			}
			if def.Status != "complete" {
				return out, ErrCatalogForbidden
			}
			baseline = def.Version
			if req.Baseline != "" && req.Baseline != baseline {
				return out, ErrObjectChanged
			}
			if req.Action == "edit" && req.Baseline == "" {
				return out, errors.New("editing requires the version of the original definition")
			}
		}
	}

	target := pgObjectTarget(req.Ref)
	kind := strings.ToUpper(string(req.Ref.Kind))
	switch req.Action {
	case "create", "edit":
		out.Ref, out.Steps, err = pgObjectDDL(ctx, conn, req, detail, version)
	case "readDefinition":
		step, buildErr := PostgreSQLDefinitionStep(ctx, conn, req.Ref, req.OID)
		err = buildErr
		out.Steps = []ObjectStep{step}
	case "invoke":
		step, buildErr := pgInvoke(ctx, conn, detail, req.Arguments, req.ResultColumns, mask)
		err = buildErr
		out.Steps = []ObjectStep{step}
	case "rename":
		if !validObjectIdentifier(req.NewName, 63) {
			return out, errors.New("invalid new name")
		}
		out.Steps = []ObjectStep{{SQL: "ALTER " + kind + " " + target + " RENAME TO " + pgIdent(req.NewName)}}
	case "drop":
		out.Steps = []ObjectStep{{SQL: "DROP " + kind + " " + target + " RESTRICT"}}
	case "setState":
		step, err := pgTriggerState(req.Ref, req.State)
		if err != nil {
			return out, err
		}
		out.Steps = []ObjectStep{step}
	default:
		return out, errors.New("unknown object action")
	}
	if err != nil {
		return out, err
	}
	if req.Action == "create" {
		_, err := PostgreSQLObjectDetail(ctx, conn, out.Ref, 0)
		if err == nil {
			return out, ErrObjectAlreadyExists
		}
		if !errors.Is(err, ErrCatalogNotFound) {
			return out, err
		}
	}
	if req.Ref.Kind == TriggerKind && req.Action != "create" && req.Action != "readDefinition" {
		table := pgIdent(req.Ref.Schema) + "." + pgIdent(req.Ref.Table)
		out.Steps = append([]ObjectStep{{SQL: "LOCK TABLE " + table + " IN SHARE ROW EXCLUSIVE MODE", VerifyAfter: true}}, out.Steps...)
	}
	for _, step := range out.Steps {
		if step.SQL == "" || DocumentExceedsLimit(step.SQL) {
			return out, ErrDocumentTooLarge
		}
	}
	evidence, err := json.Marshal([]any{out.ExecutionUser, environment, instance, detail, baseline, out.Ref, out.Steps, req})
	if err != nil {
		return out, err
	}
	out.Fingerprint = fmt.Sprintf("%x", sha256.Sum256(evidence))
	return out, nil
}

func pgIdent(name string) string        { return quoteIdentifier(name, dialect.PostgreSQL, '"') }
func pgTypeName(t ObjectTypeRef) string { return pgIdent(t.Schema) + "." + pgIdent(t.Name) }
func validObjectIdentifier(name string, maxBytes int) bool {
	return name != "" && len(name) <= maxBytes && !strings.ContainsRune(name, 0)
}
func pgObjectTarget(ref ObjectRef) string {
	if ref.Kind == "trigger" {
		return pgIdent(ref.Name) + " ON " + pgIdent(ref.Schema) + "." + pgIdent(ref.Table)
	}
	types := make([]string, len(ref.InputTypes))
	for i, t := range ref.InputTypes {
		types[i] = pgTypeName(t)
	}
	return pgIdent(ref.Schema) + "." + pgIdent(ref.Name) + "(" + strings.Join(types, ", ") + ")"
}
func pgTriggerState(ref ObjectRef, state string) (ObjectStep, error) {
	clause := map[string]string{"D": "DISABLE", "O": "ENABLE", "R": "ENABLE REPLICA", "A": "ENABLE ALWAYS"}[state]
	if ref.Kind != "trigger" || clause == "" {
		return ObjectStep{}, ErrCatalogUnsupported
	}
	return ObjectStep{SQL: "ALTER TABLE " + pgIdent(ref.Schema) + "." + pgIdent(ref.Table) + " " + clause + " TRIGGER " + pgIdent(ref.Name)}, nil
}
