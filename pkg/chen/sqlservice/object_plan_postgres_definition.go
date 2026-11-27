package sqlservice

import (
	"context"
	"database/sql"
	"strconv"
)

// PostgreSQLDefinitionStep builds a selectable, read-only Console command.
// The worker sends it only after the existing warning/review and audit path.
func PostgreSQLDefinitionStep(ctx context.Context, conn *sql.Conn, ref ObjectRef, oid uint32) (ObjectStep, error) {
	q, _, err := postgresDefinitionSource(ctx, conn, ref, oid)
	if err != nil {
		return ObjectStep{}, err
	}
	query := "SELECT pg_catalog.left(body," + strconv.Itoa(MaxDocumentCodeUnits) + ") AS definition" + q.from
	return ObjectStep{SQL: q.policySQL(query), BoundSQL: query, Args: q.args, ResultRole: "definition"}, nil
}
