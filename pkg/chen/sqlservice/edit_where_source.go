package sqlservice

import (
	"fmt"
	"strings"

	"github.com/jumpserver/koko/pkg/chen/sqlanalysis"
)

// editWhereSubquerySource restores direct-column result editing when only the
// predicate contains a subquery. The original statement must be fully parsed;
// removing its outer WHERE is only a source-identity proof, never executable SQL.
// The result edit path still resolves the live base table, actual result labels,
// primary key and original values, and never replays this predicate on writes.
func editWhereSubquerySource(d Dialect, document string, ts []token, analysis Analysis, opts AnalyzeOptions) (EditableSource, bool, error) {
	fail := func(reason string) (EditableSource, bool, error) {
		return EditableSource{}, true, fmt.Errorf("%w: %s", ErrNotEditable, reason)
	}
	depth, where, end := 0, -1, len(document)
	inWhere, subquery, outsideSubquery := false, false, false
	unsafe := false
	for _, t := range ts {
		if t.kind == ';' {
			return fail("multiple statements")
		}
		if t.kind == '(' {
			depth++
			continue
		}
		if t.kind == ')' {
			depth--
			if depth < 0 {
				return fail("unbalanced parentheses")
			}
			continue
		}
		if !t.ident() || t.quoted {
			continue
		}
		// These operations remain outside editable-query admission, even in
		// a nested predicate. Do not reinterpret a locking read as harmless.
		switch strings.ToUpper(t.value) {
		case "INTO", "FOR", "LOCK", "UPDLOCK", "XLOCK", "HOLDLOCK", "TABLOCKX", "SERIALIZABLE", "NEXTVAL", "CURRVAL":
			unsafe = true
		}
		if depth == 0 {
			if t.is("WHERE") {
				if where >= 0 {
					return fail("multiple outer predicates")
				}
				where, inWhere = t.start, true
			} else if inWhere && (t.is("ORDER") || t.is("LIMIT") || t.is("OFFSET") || t.is("FETCH") || t.is("GROUP") || t.is("HAVING") || t.is("UNION") || t.is("INTERSECT") || t.is("EXCEPT") || t.is("MINUS") || t.is("WINDOW") || t.is("QUALIFY") || t.is("CONNECT") || t.is("START") || t.is("MODEL")) {
				end, inWhere = t.start, false
			}
		} else if t.is("SELECT") || t.is("WITH") {
			if inWhere {
				subquery = true
			} else {
				outsideSubquery = true
			}
		}
	}
	if !subquery {
		return EditableSource{}, false, nil
	}
	if depth != 0 || outsideSubquery || unsafe || where < 0 || end <= where {
		return fail("subqueries are allowed only in an unlocked outer WHERE")
	}
	statement := analysis.Statements[0]
	if statement.Facts != nil {
		for _, feature := range statement.Facts.Features {
			switch feature {
			case sqlanalysis.Locking, sqlanalysis.Sequence, sqlanalysis.SelectInto, sqlanalysis.OutputInto, sqlanalysis.Assignment, sqlanalysis.Returning:
				return fail("query has side effects or locking")
			}
		}
	} else if statement.MayWrite {
		return fail("query may write")
	}
	if analysis.Syntax != sqlanalysis.Checked {
		// The ordinary fallback scanner proves only statement boundaries. Use
		// the existing closed grammar to prove every token of this new shape;
		// unknown vendor extensions remain read-only rather than being skipped.
		if _, err := AnalyzeQueryPage(d, document, opts); err != nil {
			return fail("predicate subquery syntax is not proven")
		}
	}
	// Preserve every outer modifier, projection, source and trailing clause.
	// The recursive call contains no predicate subquery, so it uses the existing
	// narrow source proof (including Oracle/T-SQL AST and quoting restrictions).
	source, err := AnalyzeEditableQuery(d, document[:where]+document[end:], opts)
	if err != nil {
		return EditableSource{}, true, err
	}
	return source, true, nil
}
