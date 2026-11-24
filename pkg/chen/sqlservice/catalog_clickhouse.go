package sqlservice

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"unicode/utf8"

	"github.com/ClickHouse/clickhouse-go/v2"
)

// CapabilityState describes evidence about a metadata feature, not its data.
// A supported feature may still return NULL or an empty result.
type CapabilityState string

const (
	CapabilitySupported   CapabilityState = "supported"
	CapabilityUnsupported CapabilityState = "unsupported"
	CapabilityRestricted  CapabilityState = "restricted"
	CapabilityUnknown     CapabilityState = "unknown"
)

// A request-local query strategy, passed explicitly to the ClickHouse builders.
// No address/user/session cache: another request may use another cluster node,
// connection, role or server version. Only fields consumed by this section are
// probed. This is not a registry of database versions or SQL rewrite rules.
type clickHouseCapabilities struct {
	version                             string
	tableComment, tableRows, tableBytes CapabilityState
	skippingIndexes                     CapabilityState
}

func probeClickHouseCapabilities(ctx context.Context, conn *sql.Conn, section string, overview ...bool) (*clickHouseCapabilities, error) {
	c := &clickHouseCapabilities{tableComment: CapabilityUnknown, tableRows: CapabilityUnknown, tableBytes: CapabilityUnknown, skippingIndexes: CapabilityUnknown}
	var fields []string
	switch section {
	case "objects":
		fields = []string{"comment"}
	case "tables", "views":
		fields = []string{"comment", "total_rows", "total_bytes"}
	case "statistics":
		fields = []string{"total_rows", "total_bytes"}
	case "indexes":
		fields = []string{"database", "table", "name", "expr", "type"}
	case "properties":
		return c, nil // Native database properties/DDL need none of these fields.
	default:
		return nil, fmt.Errorf("unknown ClickHouse metadata section")
	}
	if len(overview) > 0 && overview[0] {
		if err := conn.QueryRowContext(ctx, "SELECT version()").Scan(&c.version); err != nil {
			return c, err
		}
		if c.version == "" || len(c.version) > 256 || !validOverviewText(c.version) {
			return c, fmt.Errorf("invalid ClickHouse metadata server version")
		}
	}
	table := "tables"
	if section == "indexes" {
		table = "data_skipping_indices"
	}
	columns, err := probeClickHouseCatalogColumns(ctx, conn, table, fields)
	if err != nil {
		return c, err
	}
	if section == "indexes" {
		c.skippingIndexes = CapabilitySupported
		for _, name := range fields {
			if columns[name] != CapabilitySupported {
				c.skippingIndexes = columns[name]
				break
			}
		}
	} else {
		if state, ok := columns["comment"]; ok {
			c.tableComment = state
		}
		if state, ok := columns["total_rows"]; ok {
			c.tableRows = state
		}
		if state, ok := columns["total_bytes"]; ok {
			c.tableBytes = state
		}
	}
	return c, nil
}

func clickHousePermissionDenied(err error) bool {
	var exception *clickhouse.Exception
	return errors.As(err, &exception) && exception.Code == 497
}

// A zero-row SELECT returns the server's actual result header and requires full
// SELECT access. Unlike a missing row in system.columns, a missing header field
// after a successful SELECT * is evidence of absence, not visibility filtering.
// Known absence/access errors apply only to this dedicated probe. We never
// retry an ordinary metadata query by replacing SQL after it has failed.
func probeClickHouseCatalogColumns(ctx context.Context, conn *sql.Conn, table string, fields []string) (map[string]CapabilityState, error) {
	states := map[string]CapabilityState{}
	setAll := func(state CapabilityState) {
		for _, field := range fields {
			states[field] = state
		}
	}
	setAll(CapabilityUnknown)
	if table != "tables" && table != "data_skipping_indices" {
		return states, fmt.Errorf("unsupported ClickHouse capability probe")
	}
	probeFailure := func(err error) (map[string]CapabilityState, error) {
		if ctx.Err() != nil {
			return states, ctx.Err()
		}
		if clickHousePermissionDenied(err) {
			setAll(CapabilityRestricted)
			return states, nil
		}
		var exception *clickhouse.Exception
		if table == "data_skipping_indices" && errors.As(err, &exception) && exception.Code == 60 {
			setAll(CapabilityUnsupported)
			return states, nil
		}
		return states, fmt.Errorf("probe ClickHouse system.%s: %w", table, err)
	}
	rows, err := conn.QueryContext(ctx, "SELECT * FROM system."+table+" LIMIT 0")
	if err != nil {
		return probeFailure(err)
	}
	defer rows.Close()
	names, err := rows.Columns()
	if err != nil {
		return probeFailure(err)
	}
	// Empty/malformed evidence remains unknown. Do not turn it into a false
	// unsupported feature or execute a query referencing an unverified field.
	if len(names) > 128 {
		return states, fmt.Errorf("ClickHouse capability column budget exceeded")
	}
	present := map[string]bool{}
	for _, name := range names {
		if name == "" || !utf8.ValidString(name) || utf8.RuneCountInString(name) > 128 {
			return states, fmt.Errorf("invalid ClickHouse catalog column name")
		}
		present[name] = true
	}
	if rows.Next() {
		return states, fmt.Errorf("ClickHouse zero-row capability probe returned data")
	}
	if err := rows.Err(); err != nil {
		return probeFailure(err)
	}
	if err := rows.Close(); err != nil {
		return probeFailure(err)
	}
	if len(names) == 0 {
		return states, nil
	}
	for _, name := range fields {
		states[name] = CapabilityUnsupported
		if present[name] {
			states[name] = CapabilitySupported
		}
	}
	return states, nil
}

func (c *clickHouseCapabilities) value(key string, value OverviewValue) OverviewValue {
	var state CapabilityState
	switch key {
	case "comment":
		state = c.tableComment
	case "estimated_rows":
		state = c.tableRows
	case "total_bytes":
		state = c.tableBytes
	default:
		return value
	}
	if state == CapabilitySupported {
		return value
	}
	// An absent capability does not supply a known empty comment or zero rows.
	valueState := CapabilityUnknown
	if state == CapabilityRestricted {
		valueState = CapabilityRestricted
	}
	return OverviewValue{State: string(valueState), Source: "ClickHouse metadata capability: " + string(state)}
}

func (c *clickHouseCapabilities) supportsValue(key string) bool {
	return key == "estimated_rows" && c.tableRows == CapabilitySupported || key == "total_bytes" && c.tableBytes == CapabilitySupported
}

func (c *clickHouseCapabilities) contract(section string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("clickhouse\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s", c.version, section, c.tableComment, c.tableRows, c.tableBytes, c.skippingIndexes))))
}

func (c *clickHouseCapabilities) indexesQuery(r CatalogColumnRequest) (catalogQuery, []string, bool) {
	if c.skippingIndexes != CapabilitySupported {
		return catalogQuery{}, nil, false
	}
	return catalogQuery{`SELECT name,expr,0,0 FROM system.data_skipping_indices WHERE database=? AND table=? ORDER BY name`, []any{r.Schema, r.Table}}, []string{"name", "column", "ordinal", "unique"}, true
}
