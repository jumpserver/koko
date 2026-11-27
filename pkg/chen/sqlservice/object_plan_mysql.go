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

type ObjectRecovery struct {
	User        string            `json:"-"`
	Role        string            `json:"-"`
	HasRoles    bool              `json:"-"`
	Definer     string            `json:"-"`
	ShowSQL     string            `json:"-"`
	Definition  string            `json:"definition"`
	Environment map[string]string `json:"environment,omitempty"`
}

type mysqlPlanContext struct {
	Database, User, Role, SQLMode, Charset, Collation, DatabaseCollation, TimeZone, Version string
}

func buildMySQLCommands(ctx context.Context, conn *sql.Conn, d dialect.ID, req ObjectPlanRequest, authorize func(string) error, mask func(string, string) (string, bool)) (ObjectCommands, error) {
	out := ObjectCommands{Ref: req.Ref}
	if !ValidObjectRef(req.Ref) || req.Ref.Database != req.Ref.Schema || len(req.Ref.InputTypes) != 0 || req.OID != 0 || len(req.Arguments) > 256 || DocumentExceedsLimit(req.Definition) {
		return out, ErrCatalogNotFound
	}
	if err := catalogScopeAccess(ctx, conn, d, req.Ref.Database, req.Ref.Schema); err != nil {
		return out, err
	}
	p, err := ReadMySQLObjectProfile(ctx, conn, d)
	if err != nil {
		return out, err
	}
	action := req.Action
	if action == "readDefinition" {
		action = "definition"
	}
	if !slices.Contains(p.Actions(req.Ref.Kind), action) {
		return out, ErrCatalogUnsupported
	}
	var env mysqlPlanContext
	err = conn.QueryRowContext(ctx, `SELECT COALESCE(DATABASE(),''),CURRENT_USER(),@@session.sql_mode,@@session.character_set_client,@@session.collation_connection,@@collation_database,@@session.time_zone,VERSION()`).Scan(&env.Database, &env.User, &env.SQLMode, &env.Charset, &env.Collation, &env.DatabaseCollation, &env.TimeZone, &env.Version)
	if err != nil {
		return out, err
	}
	if env.Database != req.Ref.Database {
		return out, errors.New("choose a console connected to the selected database")
	}
	hasRoles := p.supportsCurrentRole()
	if hasRoles {
		if err = conn.QueryRowContext(ctx, `SELECT COALESCE(CURRENT_ROLE(),'')`).Scan(&env.Role); err != nil {
			return out, err
		}
	}
	opts := AnalyzeOptions{MySQLSQLMode: env.SQLMode, MySQLVersion: dialect.MySQLCompatibilityNumber(env.Version)}
	detail, err := MySQLObjectDetail(ctx, conn, d, req.Ref)
	var old ObjectDefinition
	if req.Action == "create" {
		if err == nil {
			return out, ErrObjectChanged
		}
		if !errors.Is(err, ErrCatalogNotFound) {
			return out, err
		}
		if req.Ref.Kind == TriggerKind {
			if err = catalogEditableRelationAccess(ctx, conn, d, req.Ref.Database, req.Ref.Table); err != nil {
				return out, err
			}
		}
	} else {
		if err != nil {
			return out, err
		}
		if !slices.Contains(detail.Actions, action) {
			return out, ErrCatalogForbidden
		}
		if req.Action != "invoke" && req.Action != "readDefinition" {
			old, err = MySQLObjectDefinition(ctx, conn, d, req.Ref, authorize)
			if err != nil {
				return out, err
			}
			if req.Baseline != "" && req.Baseline != old.Version {
				return out, ErrObjectChanged
			}
			if req.Action == "edit" && (old.Status != "complete" || req.Baseline == "") {
				return out, errors.New("a complete original definition and its version are required for replacement")
			}
		}
	}
	switch req.Action {
	case "create", "edit":
		out.Steps, err = mysqlPlanDDL(req, p, opts, detail, old)
		if err == nil && req.Action == "edit" {
			if err = checkMySQLDefinitionEnvironment(env, old, req, opts); err != nil {
				return out, err
			}
			show, err := MySQLDefinitionCommand(d, req.Ref)
			if err != nil {
				return out, err
			}
			out.Recovery = &ObjectRecovery{User: env.User, Role: env.Role, HasRoles: hasRoles, Definer: myString(detail.Properties, "definer"), ShowSQL: show, Definition: old.Text, Environment: old.Environment}
		}
	case "invoke":
		if p.defaultsFromDefinition(req.Ref.Kind) && slices.ContainsFunc(req.Arguments, func(a ObjectArgument) bool { return a.Mode == "default" }) {
			old, err = MySQLObjectDefinition(ctx, conn, d, req.Ref, authorize)
			if err != nil {
				return out, err
			}
			if old.Status != "complete" {
				return out, ErrCatalogUnsupported
			}
			for i, par := range detail.Parameters {
				detail.Parameters[i].HasDefault = slices.Contains(old.DefaultPositions, par.Position)
			}
		}
		out.Steps, err = mysqlInvoke(req, detail, p, opts, mask)
	case "drop":
		out.Steps = []ObjectStep{{SQL: "DROP " + strings.ToUpper(string(req.Ref.Kind)) + " " + mysqlQualified(req.Ref)}}
	case "setState", "rename":
		if req.Ref.Kind != EventKind {
			return out, ErrCatalogUnsupported
		}
		suffix := req.State
		if req.Action == "rename" {
			if !validObjectIdentifier(req.NewName, 64) {
				return out, errors.New("invalid event name")
			}
			suffix = "RENAME TO " + mysqlObjectIdent(req.Ref.Database) + "." + mysqlObjectIdent(req.NewName)
		} else if suffix != "ENABLE" && suffix != "DISABLE" {
			return out, errors.New("event state must be ENABLE or DISABLE")
		}
		var st ObjectStep
		st, err = mysqlEventAlter(req.Ref, old, opts, suffix)
		out.Steps = []ObjectStep{st}
	case "readDefinition":
		out.Steps = []ObjectStep{{SQL: "SHOW CREATE " + strings.ToUpper(string(req.Ref.Kind)) + " " + mysqlQualified(req.Ref), ResultRole: "definition"}}
	default:
		return out, ErrCatalogUnsupported
	}
	if err != nil {
		return out, err
	}
	// LAST_EXECUTED and the scheduler setting are observations, not a change
	// to the event being managed. Preserve all other creation evidence.
	delete(detail.Properties, "last_executed")
	delete(detail.Properties, "scheduler")
	for i, s := range out.Steps {
		if (req.Ref.Kind == EventKind || req.Ref.Kind == TriggerKind) && strings.HasPrefix(s.SQL, "DROP ") {
			out.Steps[i].ParserSQL = "DROP " + strings.ToUpper(string(req.Ref.Kind)) + " " + mysqlObjectIdent(req.Ref.Name)
		}
		if req.Action == "readDefinition" {
			out.Steps[i].ParserSQL = "SHOW CREATE " + strings.ToUpper(string(req.Ref.Kind)) + " " + mysqlObjectIdent(req.Ref.Name)
		}

		if req.Action == "create" || req.Action == "edit" || req.Action == "rename" || req.Action == "setState" {
			if !(i == 0 && len(out.Steps) > 1 && strings.HasPrefix(s.SQL, "DROP ")) {
				h, err := mysqlObjectHeader(s.SQL, req.Ref, opts)
				if err != nil {
					return out, err
				}
				if req.Ref.Kind == TriggerKind {
					if err = checkMySQLTriggerAnchor(ctx, conn, p, req.Ref, h); err != nil {
						return out, err
					}
				}
				out.Steps[i].ParserSQL, err = mysqlObjectParserSQL(s.SQL, h, p)
				if err != nil {
					return out, err
				}
			}
		}
		if s.SQL == "" || DocumentExceedsLimit(s.SQL) || DocumentExceedsLimit(s.PolicySQL) {
			return out, ErrDocumentTooLarge
		}
	}
	evidence, err := json.Marshal([]any{env, p.Contract, detail, old.Version, out.Ref, out.Steps, req})
	if err != nil {
		return out, err
	}
	out.Fingerprint = fmt.Sprintf("%x", sha256.Sum256(evidence))
	return out, nil
}

func checkMySQLDefinitionEnvironment(env mysqlPlanContext, old ObjectDefinition, req ObjectPlanRequest, opts AnalyzeOptions) error {
	for k, v := range old.Environment {
		switch strings.ToLower(k) {
		case "sql_mode":
			oldModes, newModes := strings.Split(strings.ToUpper(v), ","), strings.Split(strings.ToUpper(env.SQLMode), ",")
			slices.Sort(oldModes)
			slices.Sort(newModes)
			if !slices.Equal(oldModes, newModes) {
				return errors.New("use a console with the original object's SQL mode before saving")
			}
		case "character_set_client":
			if !strings.EqualFold(v, env.Charset) {
				return errors.New("use a console with the original client character set before saving")
			}
		case "collation_connection":
			if !strings.EqualFold(v, env.Collation) {
				return errors.New("use a console with the original connection collation before saving")
			}
		case "database_collation":
			if !strings.EqualFold(v, env.DatabaseCollation) {
				return errors.New("the database collation differs from the object's creation environment")
			}
		case "time_zone":
			if req.Ref.Kind == EventKind && !strings.EqualFold(v, env.TimeZone) {
				h, err := mysqlObjectHeader(req.Definition, req.Ref, opts)
				if err != nil {
					return err
				}
				for _, t := range h.tokens[h.next:] {
					if t.is("DO") {
						break
					}
					if t.is("SCHEDULE") {
						return errors.New("use a console with the event's original time zone before changing its schedule")
					}
				}
			}
		}
	}
	return nil
}
