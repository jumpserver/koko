package engine

import (
	"context"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"

	"github.com/jumpserver/koko/pkg/chen/dialect"
	"github.com/jumpserver/koko/pkg/chen/sqlservice"
)

func (v *Server) prepareExplain(w http.ResponseWriter, r *http.Request) {
	s := v.auth(w, r, false)
	if s == nil {
		return
	}
	c := v.findConsole(w, r, s)
	if c == nil {
		return
	}
	var request struct {
		SQL string `json:"sql"`
	}
	if !decode(w, r, &request) {
		return
	}
	// Stateful and plan-table dialects must use the execution intent endpoint;
	// returning their original SELECT here would execute user data queries in a
	// legacy prepare-then-execute consumer.
	if specialPlanDialect(s.grant.Dialect()) {
		fail(w, 400, "explain_requires_plan_execution", "This database requires plan_mode=ESTIMATED on the execution endpoint")
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	opts, err := liveOptions(r.Context(), c, s.grant.Dialect())
	if err != nil {
		fail(w, 409, "sql_context_unavailable", "Console SQL context is unavailable")
		return
	}
	sql, _, err := estimatedPlanSQL(r.Context(), c, s.grant.Dialect(), request.SQL, opts)
	if err != nil {
		var explicit *APIError
		if errors.As(err, &explicit) {
			fail(w, 400, explicit.Code, explicit.Message)
			return
		}
		if !strings.HasPrefix(err.Error(), "explain_") {
			fail(w, 409, "sql_context_unavailable", "Console server version is unavailable")
			return
		}
		fail(w, 400, err.Error(), "Estimated plans require one read query and a supported database")
		return
	}
	writeJSON(w, 200, map[string]string{"sql": sql})
}

// Probe only plan-specific metadata on the locked physical console. Unknown
// versions retain the existing raw EXPLAIN; SQL is never retried in another format.
func estimatedPlanSQL(ctx context.Context, c *console, d dialect.ID, document string, opts sqlservice.AnalyzeOptions) (string, string, error) {
	command, err := sqlservice.EstimatedPlanSQL(d, document, opts)
	if err != nil {
		if errors.Is(err, sqlservice.ErrExplainShowNotSupported) {
			return "", "", &APIError{err.Error(), "Estimated plans are unavailable for SHOW statements. Run the statement directly to view the metadata."}
		}
		return "", "", err
	}
	switch d {
	case dialect.SQLServer:
		var version string
		if err := c.conn.QueryRowContext(ctx, "SELECT CAST(SERVERPROPERTY('ProductVersion') AS nvarchar(128))").Scan(&version); err != nil {
			return "", "", err
		}
		return command, version, nil
	case dialect.Oracle:
		table, err := resolveOraclePlanTable(ctx, c)
		if err != nil {
			return "", "", err
		}
		var version string
		if err := c.conn.QueryRowContext(ctx, "SELECT BANNER FROM V$VERSION WHERE ROWNUM=1").Scan(&version); err != nil {
			return "", "", err
		}
		return strings.Replace(command, "EXPLAIN PLAN FOR ", "EXPLAIN PLAN SET STATEMENT_ID = 'chen_"+newID("")[:20]+"' INTO "+table+" FOR ", 1), version, nil
	case dialect.DB2:
		schema, err := resolveDB2PlanSchema(ctx, c)
		if err != nil {
			return "", "", err
		}
		var version string
		if err := c.conn.QueryRowContext(ctx, "SELECT SERVICE_LEVEL FROM TABLE(SYSPROC.ENV_GET_INST_INFO()) AS T").Scan(&version); err != nil {
			return "", "", err
		}
		return strings.Replace(command, "EXPLAIN PLAN FOR ", "EXPLAIN PLAN SET QUERYNO = 1 SET QUERYTAG = 'chen_"+newID("")[:15]+"' FOR ", 1) + db2PlanMarker + hex.EncodeToString([]byte(schema)), version, nil
	case dialect.Dameng, dialect.OceanBaseOracle:
		var version string
		if err := c.conn.QueryRowContext(ctx, "SELECT BANNER FROM V$VERSION WHERE ROWNUM=1").Scan(&version); err != nil {
			return "", "", err
		}
		return command, version, nil
	case dialect.MariaDB, dialect.OceanBaseMySQL:
		version, probeErr := sqlservice.ReadMySQLServerVersion(ctx, c.conn, d)
		if requiredErr := sqlservice.RequireReportedVersion(ctx, probeErr); requiredErr != nil {
			return "", "", requiredErr
		}
		// Only an unavailable optional comment reaches this fallback.
		// Raw EXPLAIN needs a nonempty VERSION() report in that case.
		if probeErr != nil && version.Reported == "" {
			return "", "", probeErr
		}
		return command, version.Product, nil
	case dialect.ClickHouse:
		var version string
		if err := c.conn.QueryRowContext(ctx, "SELECT VERSION()").Scan(&version); err != nil {
			return "", "", err
		}
		if supportedPlanVersion(d, version) {
			command = strings.Replace(command, "EXPLAIN PLAN ", sqlservice.ClickHouseJSONPlanPrefix, 1)
		}
		return command, version, nil
	default:
		return command, "", nil
	}
}
