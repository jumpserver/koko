package engine

import (
	"context"
	"errors"
	"github.com/jumpserver/koko/pkg/chen/dialect"
	"github.com/jumpserver/koko/pkg/chen/sqlservice"
)

var errOraclePageName = errors.New("Database paging cannot prove this Oracle bare name resolves to a visible column in its lexical scope; use explicit column/CTE aliases or cached results")

type oraclePageNames struct {
	ctx      context.Context
	console  *console
	dialect  dialect.ID
	schema   string
	columns  map[string][]string
	outputs  map[*sqlservice.QueryPageScope][]string
	visiting map[*sqlservice.QueryPageScope]bool
}

func validateOracleColumnBindings(ctx context.Context, c *console, d dialect.ID, scopes []*sqlservice.QueryPageScope) error {
	if len(scopes) > 512 {
		return errOraclePageName
	}
	r := oraclePageNames{ctx: ctx, console: c, dialect: d, columns: map[string][]string{}, outputs: map[*sqlservice.QueryPageScope][]string{}, visiting: map[*sqlservice.QueryPageScope]bool{}}
	references := 0
	for _, scope := range scopes {
		for _, ref := range scope.References {
			references++
			if references > 2048 {
				return errOraclePageName
			}
			if err := r.reference(scope, ref); err != nil {
				return err
			}
		}
	}
	return nil
}
func (r *oraclePageNames) currentSchema() (string, error) {
	if r.schema != "" {
		return r.schema, nil
	}
	err := r.console.conn.QueryRowContext(r.ctx, sqlservice.OracleCurrentSchemaSQL).Scan(&r.schema)
	return r.schema, err
}
func (r *oraclePageNames) baseColumns(base *sqlservice.QueryPageRelation) ([]string, error) {
	owner := base.Schema
	if owner == "" {
		var err error
		owner, err = r.currentSchema()
		if err != nil {
			return nil, err
		}
	}
	key := owner + "\x00" + base.Name
	if columns, ok := r.columns[key]; ok {
		return columns, nil
	}
	read := func(owner string) ([]string, error) {
		rows, err := r.console.conn.QueryContext(r.ctx, `SELECT COLUMN_NAME FROM ALL_TAB_COLUMNS WHERE OWNER=:1 AND TABLE_NAME=:2 ORDER BY COLUMN_ID`, owner, base.Name)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		columns := []string{}
		for rows.Next() {
			var name string
			if err = rows.Scan(&name); err != nil {
				return nil, err
			}
			columns = append(columns, name)
			if len(columns) > 2048 {
				return nil, errOraclePageName
			}
		}
		if err = rows.Err(); err != nil {
			return nil, err
		}
		return columns, nil
	}
	columns, err := read(owner)
	if err != nil {
		return nil, err
	}
	// The source guard already proved this exact unqualified PUBLIC synonym is
	// SYS.DUAL and that no local object shadows it. No generic synonym fallback.
	if len(columns) == 0 && r.dialect == dialect.Oracle && base.Schema == "" && base.Name == "DUAL" {
		columns, err = read("SYS")
	}
	if err != nil {
		return nil, err
	}
	if len(columns) == 0 {
		return nil, errOraclePageName
	}
	r.columns[key] = columns
	return columns, nil
}
func (r *oraclePageNames) bindingColumns(binding sqlservice.QueryPageBinding) ([]string, error) {
	if binding.Columns != nil {
		return binding.Columns, nil
	}
	if binding.Relation != nil {
		return r.baseColumns(binding.Relation)
	}
	if binding.Query != nil {
		return r.outputColumns(binding.Query)
	}
	return nil, errOraclePageName
}
func (r *oraclePageNames) outputColumns(scope *sqlservice.QueryPageScope) ([]string, error) {
	if output, ok := r.outputs[scope]; ok {
		return output, nil
	}
	if r.visiting[scope] {
		return nil, errOraclePageName
	}
	r.visiting[scope] = true
	defer delete(r.visiting, scope)
	output := []string{}
	for _, projection := range scope.Projections {
		if !projection.Star {
			output = append(output, projection.Name)
			continue
		}
		found := false
		for _, source := range scope.Sources {
			if projection.Qualifier != "" && source.Alias != projection.Qualifier {
				continue
			}
			found = true
			columns, err := r.bindingColumns(source)
			if err != nil {
				return nil, err
			}
			output = append(output, columns...)
			if len(output) > 2048 {
				return nil, errOraclePageName
			}
		}
		if !found {
			return nil, errOraclePageName
		}
	}
	r.outputs[scope] = output
	return output, nil
}
func (r *oraclePageNames) reference(scope *sqlservice.QueryPageScope, ref sqlservice.QueryPageReference) error {
	if len(ref.Parts) < 1 || len(ref.Parts) > 3 {
		return errOraclePageName
	}
	name := ref.Parts[len(ref.Parts)-1]
	if len(ref.Parts) == 1 && ref.OutputAlias {
		for _, projection := range scope.Projections {
			if !projection.Star && projection.Name != "" && projection.Name == name {
				return nil
			}
		}
	}
	for current := scope; current != nil; current = current.Parent {
		count := 0
		qualifierFound := false
		for _, source := range current.Sources {
			if len(ref.Parts) == 2 {
				if source.Alias != ref.Parts[0] {
					continue
				}
				qualifierFound = true
			}
			if len(ref.Parts) == 3 {
				if source.Relation == nil || source.Alias != source.Relation.Name || source.Relation.Name != ref.Parts[1] {
					continue
				}
				owner := source.Relation.Schema
				if owner == "" {
					var err error
					owner, err = r.currentSchema()
					if err != nil {
						return err
					}
				}
				if owner != ref.Parts[0] {
					continue
				}
				qualifierFound = true
			}
			columns, err := r.bindingColumns(source)
			if err != nil {
				return err
			}
			for _, column := range columns {
				if column == name {
					count++
				}
			}
		}
		if count == 1 {
			return nil
		}
		if count > 1 || qualifierFound {
			return errOraclePageName
		}
	}
	return errOraclePageName
}
