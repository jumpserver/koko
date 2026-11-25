package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	"github.com/jumpserver/koko/pkg/chen/dialect"
)

var planVersionNumber = regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+)`)

func mysqlReturnsOceanBasePlan(dialect string, r *Result) bool {
	return dialect == "mysql" && len(r.Columns) == 1 && strings.EqualFold(r.Columns[0].Name, "Query Plan")
}

// These are format compatibility families, not a claim of live testing every
// release. New families must provide versioned output evidence before admission.
func supportedPlanVersion(d, version string) bool {
	switch d {
	case "mariadb":
		if !strings.Contains(strings.ToLower(version), "mariadb") {
			return false
		}
		version = strings.TrimPrefix(version, "5.5.5-")
	case "oceanbase-mysql", "oceanbase-oracle":
		v, ok := dialect.ParseOceanBaseVersion(version)
		return ok && v.Major == 4 && (v.Minor == 2 || v.Minor == 3 || d == "oceanbase-mysql" && v.Minor == 4)
	}
	match := planVersionNumber.FindStringSubmatch(version)
	if len(match) != 4 {
		return false
	}
	major, _ := strconv.Atoi(match[1])
	minor, _ := strconv.Atoi(match[2])
	switch d {
	case "mariadb":
		return major == 10 && minor == 11 || major == 11 && minor <= 8
	case "clickhouse":
		return major == 21 && minor >= 6 || major >= 22 && major <= 26
	}
	return false
}

func normalizeVendorPlan(p *ExecutionPlan, r *Result) {
	if p.ServerVersion == nil || !supportedPlanVersion(p.Database, *p.ServerVersion) {
		p.warn("plan_version_unverified", "Server product/version is outside the verified plan format families; inspect the raw output")
		return
	}
	var roots []*PlanNode
	var err error
	switch p.Database {
	case "mariadb":
		p.RawFormatVersion = planString("mariadb-classic")
		roots, err = parseMariaDBPlan(r)
	case "oceanbase-mysql", "oceanbase-oracle":
		p.RawFormatVersion = planString("oceanbase-explain-text")
		if !planSingleTextColumn(r, "Query Plan") {
			err = errors.New("Expected OceanBase Query Plan text")
		} else {
			roots, err = parseOceanBasePlan(p.RawText)
		}
	case "clickhouse":
		p.RawFormatVersion = planString("clickhouse-plan-json")
		if !planSingleTextColumn(r, "explain") || len(r.Rows) != 1 {
			err = errors.New("Expected one ClickHouse PLAN JSON row")
		} else {
			roots, err = parseClickHousePlan(p.RawText)
		}
	}
	if err != nil {
		p.warn("plan_parse_failed", err.Error())
		return
	}
	p.Roots, p.Status = roots, "SUCCESS"
	p.Capabilities.Structured = true
	p.Capabilities.Hierarchy = p.Database != "mariadb"
	var facts func([]*PlanNode)
	facts = func(nodes []*PlanNode) {
		for _, n := range nodes {
			p.Capabilities.EstimatedRows = p.Capabilities.EstimatedRows || n.Rows != nil
			p.Capabilities.Cost = p.Capabilities.Cost || n.Cost != nil
			p.Capabilities.Predicates = p.Capabilities.Predicates || len(n.Predicates) > 0
			facts(n.Children)
		}
	}
	facts(roots)
	if p.Database == "mariadb" {
		p.warn("plan_flat_summary", "MariaDB classic rows are independent facts; parent/child hierarchy and costs are unavailable")
	}
}

func planSingleTextColumn(r *Result, name string) bool {
	if len(r.Columns) != 1 || !strings.EqualFold(r.Columns[0].Name, name) {
		return false
	}
	for _, row := range r.Rows {
		if len(row) != 1 {
			return false
		}
		if _, ok := row[0].(string); !ok {
			return false
		}
	}
	return true
}

// MariaDB's documented classic contract shares the access columns with MySQL,
// but not MySQL's cost model, JSON format, or inferred join hierarchy.
func parseMariaDBPlan(r *Result) ([]*PlanNode, error) {
	seen := map[string]bool{}
	for _, c := range r.Columns {
		key := strings.ToLower(c.Name)
		if seen[key] || strings.HasPrefix(key, "r_") {
			return nil, errors.New("Duplicate or ANALYZE columns in MariaDB estimated plan")
		}
		seen[key] = true
	}
	if !seen["rows"] || !seen["extra"] {
		return nil, errors.New("Incomplete MariaDB classic columns")
	}
	roots, err := parseMySQLPlan(r)
	if err != nil {
		return nil, err
	}
	for _, n := range roots {
		n.RowsMeaning = nil
		if n.Rows != nil {
			n.RowsMeaning = planString("MariaDB estimated rows examined per table lookup")
		}
	}
	return roots, nil
}

func parseClickHousePlan(raw string) ([]*PlanNode, error) {
	fail := errors.New("Unrecognized or incomplete ClickHouse PLAN JSON; inspect the raw output")
	// Token validation bounds unknown attributes as well as the node tree.
	d := json.NewDecoder(strings.NewReader(raw))
	d.UseNumber()
	depth := 0
	for {
		token, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fail
		}
		if delim, ok := token.(json.Delim); ok {
			if delim == '[' || delim == '{' {
				depth++
				if depth > planMaxDepth*3 {
					return nil, errors.New("Plan JSON exceeds the nesting limit")
				}
			} else {
				depth--
			}
		}
	}
	d = json.NewDecoder(strings.NewReader(raw))
	d.UseNumber()
	var items []map[string]json.RawMessage
	if d.Decode(&items) != nil || len(items) == 0 {
		return nil, fail
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return nil, fail
	}
	count := 0
	var convert func(json.RawMessage, int) (*PlanNode, error)
	convert = func(raw json.RawMessage, depth int) (*PlanNode, error) {
		count++
		if count > planMaxNodes || depth > planMaxDepth {
			return nil, errors.New("Plan exceeds the node or depth limit")
		}
		var m map[string]json.RawMessage
		if json.Unmarshal(raw, &m) != nil {
			return nil, fail
		}
		var op string
		if json.Unmarshal(m["Node Type"], &op) != nil || strings.TrimSpace(op) == "" {
			return nil, fail
		}
		n := newPlanNode(fmt.Sprintf("n%d", count), op)
		n.NodeType = vendorNodeType(op)
		n.PhysicalOperator = planString(op)
		for key, value := range m {
			if key == "Plans" {
				continue
			}
			var s string
			if json.Unmarshal(value, &s) != nil {
				s = string(value)
			}
			n.Attributes[key] = s
			switch key {
			case "Node Id":
				n.NativeID = planString(s)
			case "Description":
				n.Detail = planString(s)
			}
		}
		if children, ok := m["Plans"]; ok {
			var plans []json.RawMessage
			if string(children) == "null" || json.Unmarshal(children, &plans) != nil {
				return nil, fail
			}
			for _, child := range plans {
				node, err := convert(child, depth+1)
				if err != nil {
					return nil, err
				}
				n.Children = append(n.Children, node)
			}
		}
		// PLAN does not supply row/cost estimates. Parts, granules and thread counts
		// remain native attributes and are never promoted to estimated output rows.
		return n, nil
	}
	roots := []*PlanNode{}
	for _, item := range items {
		if len(item) != 1 || item["Plan"] == nil {
			return nil, fail
		}
		node, err := convert(item["Plan"], 1)
		if err != nil {
			return nil, err
		}
		roots = append(roots, node)
	}
	return roots, nil
}

var oceanBaseFilter = regexp.MustCompile(`(?:^|, )filter\((.*)\)(?:, rowset=\d+)?$`)
var oceanBaseDetail = regexp.MustCompile(`^\s*(\d+)\s+-\s+(.*)$`)
var planUnsignedID = regexp.MustCompile(`^\d+$`)

func parseOceanBasePlan(raw string) ([]*PlanNode, error) {
	fail := errors.New("Unrecognized or incomplete OceanBase EXPLAIN text; inspect the raw output")
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	if len(lines) < 5 {
		return nil, fail
	}
	// The native result is one text cell, not the mysql client's outer ASCII box.
	// Require both table borders to avoid accepting a partial operator listing.
	line := 0
	for line < len(lines) && strings.TrimSpace(lines[line]) == "" {
		line++
	}
	border := func(s string, char rune) bool {
		s = strings.TrimSpace(s)
		return len(s) >= 5 && strings.Trim(s, string(char)) == ""
	}
	if line >= len(lines) || !border(lines[line], '=') {
		return nil, fail
	}
	line++
	if line >= len(lines) {
		return nil, fail
	}
	cells := strings.Split(strings.TrimSpace(lines[line]), "|")
	if len(cells) != 7 {
		return nil, fail
	}
	for i, key := range []string{"ID", "OPERATOR", "NAME", "EST.ROWS", "EST.TIME(us)"} {
		if strings.TrimSpace(cells[i+1]) != key {
			return nil, fail
		}
	}
	line++
	if line >= len(lines) || !border(lines[line], '-') {
		return nil, fail
	}
	line++
	roots := []*PlanNode{}
	stack := []*PlanNode{}
	byID := map[string]*PlanNode{}
	for ; line < len(lines) && !border(lines[line], '='); line++ {
		cells = strings.Split(strings.TrimRight(lines[line], " \t"), "|")
		if len(cells) != 7 || cells[0] != "" || cells[6] != "" {
			return nil, fail
		}
		id := strings.TrimSpace(cells[1])
		if !planUnsignedID.MatchString(id) || byID[id] != nil {
			return nil, fail
		}
		depth, op, ok := oceanBaseOperator(cells[2])
		if !ok || depth > len(stack) || depth >= planMaxDepth || len(byID) >= planMaxNodes {
			return nil, fail
		}
		n := newPlanNode("n"+id, op)
		n.NativeID = planString(id)
		n.NodeType = vendorNodeType(op)
		n.PhysicalOperator = planString(op)
		name := strings.TrimSpace(cells[3])
		n.Relation = planString(name)
		if n.NodeType == "SCAN" || n.NodeType == "INDEX_SCAN" {
			n.Table = n.Relation
		}
		for i, key := range []string{"ID", "OPERATOR", "NAME", "EST.ROWS", "EST.TIME(us)"} {
			n.Attributes[key] = strings.TrimSpace(cells[i+1])
		}
		n.Rows, n.Cost = planNumber(strings.TrimSpace(cells[4])), planNumber(strings.TrimSpace(cells[5]))
		for i, number := range []*string{n.Rows, n.Cost} {
			val := strings.TrimSpace(cells[i+4])
			if number == nil && val != "" && val != "NULL" && val != "-" {
				return nil, fail
			}
		}
		if n.Rows != nil {
			n.RowsMeaning = planString("OceanBase estimated operator output rows")
		}
		if n.Cost != nil {
			n.CostMeaning = planString("OceanBase optimizer estimated execution time (microseconds), not measured time")
		}
		if depth == 0 {
			roots = append(roots, n)
		} else {
			parent := stack[depth-1]
			parent.Children = append(parent.Children, n)
		}
		stack = append(stack[:depth], n)
		byID[id] = n
	}
	if line >= len(lines) || len(roots) == 0 {
		return nil, fail
	}
	line++
	var current *PlanNode
	details := false
	for ; line < len(lines); line++ {
		text := strings.TrimSpace(lines[line])
		if text == "" {
			continue
		}
		if text == "Outputs & filters:" {
			details = true
			continue
		}
		if !details {
			return nil, fail
		}
		if border(text, '-') {
			continue
		}
		if match := oceanBaseDetail.FindStringSubmatch(lines[line]); match != nil {
			current = byID[match[1]]
			if current == nil {
				return nil, fail
			}
			current.Detail = planString(match[2])
		} else if current != nil && strings.HasPrefix(lines[line], "    ") {
			detail := *current.Detail + "\n" + text
			current.Detail = &detail
		} else {
			return nil, fail
		}
	}
	for _, n := range byID {
		if n.Detail == nil {
			return nil, fail
		}
		// Keep all vendor detail verbatim. Only the explicit first-line filter
		// clause is promoted; access/range expressions remain native detail.
		first, _, _ := strings.Cut(*n.Detail, "\n")
		if match := oceanBaseFilter.FindStringSubmatch(first); match != nil && match[1] != "nil" {
			n.Predicates["Filter"] = match[1]
		}
	}
	return roots, nil
}

// OceanBase 4.2 uses one indentation space per level; 4.3 also emits
// two-character box-drawing branches. IDs are labels, never parent IDs.
func oceanBaseOperator(value string) (int, string, bool) {
	s := strings.TrimRight(value, " ")
	prefix := []rune(s)
	pos := 0
	for pos < len(prefix) && strings.ContainsRune(" │├└─", prefix[pos]) {
		pos++
	}
	if pos == len(prefix) {
		return 0, "", false
	}
	indent := string(prefix[:pos])
	depth := pos
	if strings.ContainsAny(indent, "│├└─") {
		if pos%2 != 0 || !(strings.HasSuffix(indent, "├─") || strings.HasSuffix(indent, "└─")) {
			return 0, "", false
		}
		for i := 0; i < pos-2; i += 2 {
			if string(prefix[i:i+2]) != "  " && string(prefix[i:i+2]) != "│ " {
				return 0, "", false
			}
		}
		depth = pos / 2
	}
	op := string(prefix[pos:])
	if strings.ContainsAny(op, "\t\r") {
		return 0, "", false
	}
	return depth, op, true
}

func vendorNodeType(op string) string {
	switch op {
	case "ALL", "TABLE FULL SCAN", "ReadFromMergeTree", "ReadFromStorage", "ReadFromSystemNumbers", "ReadFromMemoryStorage":
		return "SCAN"
	case "TABLE GET", "TABLE RANGE SCAN":
		return "INDEX_SCAN"
	case "HASH JOIN":
		return "HASH_JOIN"
	case "MERGE JOIN":
		return "MERGE_JOIN"
	case "NESTED-LOOP JOIN":
		return "NESTED_LOOP"
	case "Join", "Joining":
		return "JOIN"
	case "SORT", "Sorting", "PartialSorting", "MergeSorting":
		return "SORT"
	case "HASH GROUP BY", "SCALAR GROUP BY", "MERGE GROUP BY", "Aggregating", "MergingAggregated":
		return "AGGREGATE"
	case "LIMIT", "Limit":
		return "LIMIT"
	case "FILTER", "Filter":
		return "FILTER"
	case "UNION ALL", "UNION DISTINCT", "Union":
		return "UNION"
	case "PX COORDINATOR", "EXCHANGE IN DISTR", "EXCHANGE OUT DISTR", "ReadFromRemote":
		return "PARALLEL"
	case "Expression", "Projection":
		return "PROJECTION"
	default:
		return "OTHER"
	}
}
