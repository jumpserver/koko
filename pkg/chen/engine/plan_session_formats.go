package engine

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
)

func normalizeSessionPlan(p *ExecutionPlan, r *Result) {
	var roots []*PlanNode
	var err error
	switch p.Database {
	case "oracle":
		p.RawFormatVersion = planString("oracle-plan-table")
		roots, err = parseTablePlan(r, "oracle")
	case "db2":
		p.RawFormatVersion = planString("db2-luw-explain")
		roots, err = parseTablePlan(r, "db2")
	case "sqlserver":
		p.RawFormatVersion = planString("sqlserver-showplan-xml")
		roots, err = parseSQLServerPlan(p.RawText)
	case "dameng":
		p.RawFormatVersion = planString("dm-explain-text")
		roots, err = parseDamengPlan(p.RawText)
	}
	if err != nil {
		p.warn("plan_parse_failed", err.Error())
		return
	}
	p.Roots = roots
	p.Status = "SUCCESS"
	p.Capabilities.Structured = true
	p.Capabilities.Hierarchy = true
	var facts func([]*PlanNode)
	facts = func(nodes []*PlanNode) {
		for _, n := range nodes {
			p.Capabilities.Cost = p.Capabilities.Cost || n.Cost != nil
			p.Capabilities.EstimatedRows = p.Capabilities.EstimatedRows || n.Rows != nil
			p.Capabilities.Predicates = p.Capabilities.Predicates || len(n.Predicates) > 0
			facts(n.Children)
		}
	}
	facts(roots)
}

func nativePlanType(op string) string {
	u := strings.ToUpper(op)
	switch {
	case strings.Contains(u, "HASH") && strings.Contains(u, "JOIN"):
		return "HASH_JOIN"
	case strings.Contains(u, "MERGE") && strings.Contains(u, "JOIN"):
		return "MERGE_JOIN"
	case strings.Contains(u, "NESTED LOOPS") || u == "NLJOIN":
		return "NESTED_LOOP"
	case strings.Contains(u, "JOIN"):
		return "JOIN"
	case strings.Contains(u, "INDEX") || strings.Contains(u, "IXSCAN") || strings.Contains(u, "SSEK") || strings.Contains(u, "BLKUP"):
		return "INDEX_SCAN"
	case strings.Contains(u, "SCAN") || strings.Contains(u, "TABLE ACCESS") || strings.Contains(u, "CSCN") || strings.Contains(u, "SSCN"):
		return "SCAN"
	case strings.Contains(u, "SORT"):
		return "SORT"
	case strings.Contains(u, "AGG") || strings.Contains(u, "GROUP"):
		return "AGGREGATE"
	case strings.Contains(u, "FILTER") || strings.Contains(u, "SLCT"):
		return "FILTER"
	case strings.Contains(u, "LIMIT") || strings.Contains(u, "TOP"):
		return "LIMIT"
	case strings.Contains(u, "UNION"):
		return "UNION"
	case strings.Contains(u, "PRJT") || strings.Contains(u, "PROJECT") || strings.Contains(u, "COMPUTE SCALAR"):
		return "PROJECTION"
	}
	return "OTHER"
}

type nativePlanRecord struct {
	node     *PlanNode
	parent   string
	position int
}

func assemblePlan(records map[string]nativePlanRecord, order []string) ([]*PlanNode, error) {
	bad := errors.New("Incomplete, cyclic or multiply-parented native plan; inspect raw output")
	if len(records) == 0 || len(records) > planMaxNodes {
		return nil, bad
	}
	roots := []*PlanNode{}
	for _, id := range order {
		r := records[id]
		seen := map[string]bool{id: true}
		at := r.parent
		depth := 1
		for at != "" {
			parent, ok := records[at]
			if !ok || seen[at] || depth >= planMaxDepth {
				return nil, bad
			}
			seen[at] = true
			depth++
			at = parent.parent
		}
		if r.parent == "" {
			roots = append(roots, r.node)
		} else {
			parent := records[r.parent]
			parent.node.Children = append(parent.node.Children, r.node)
		}
	}
	if len(roots) == 0 {
		return nil, bad
	}
	return roots, nil
}
func planColumnIndex(r *Result) (map[string]int, error) {
	out := map[string]int{}
	for i, c := range r.Columns {
		n := strings.ToUpper(c.Name)
		if _, exists := out[n]; exists {
			return nil, errors.New("Duplicate native plan column")
		}
		out[n] = i
	}
	for _, row := range r.Rows {
		if len(row) != len(r.Columns) {
			return nil, errors.New("Incomplete native plan row")
		}
	}
	return out, nil
}
func parseTablePlan(r *Result, d string) ([]*PlanNode, error) {
	cols, err := planColumnIndex(r)
	if err != nil {
		return nil, err
	}
	idName, opName, costName, rowsName := "ID", "OPERATION", "COST", "CARDINALITY"
	if d == "db2" {
		idName, opName, costName, rowsName = "OPERATOR_ID", "OPERATOR_TYPE", "TOTAL_COST", "ESTIMATED_ROWS"
	}
	for _, n := range []string{idName, opName, "PARENT_ID"} {
		if _, ok := cols[n]; !ok {
			return nil, fmt.Errorf("Missing native plan column %s", n)
		}
	}
	records := map[string]nativePlanRecord{}
	order := []string{}
	for _, row := range r.Rows {
		value := func(name string) any {
			if i, ok := cols[name]; ok {
				return row[i]
			}
			return nil
		}
		id := planText(value(idName))
		op := planText(value(opName))
		parent := planText(value("PARENT_ID"))
		if id == "" || op == "" {
			return nil, errors.New("Missing native plan identity or operator")
		}
		if _, ok := records[id]; ok {
			return nil, errors.New("Native plan has duplicate operators or a DAG; inspect raw output")
		}
		if d == "oracle" {
			if options := planText(value("OPTIONS")); options != "" {
				op += " " + options
			}
		}
		n := newPlanNode("n"+strconv.Itoa(len(order)+1), op)
		n.NativeID = planString(id)
		n.NodeType = nativePlanType(op)
		n.Rows = planNumber(value(rowsName))
		n.Cost = planNumber(value(costName))
		n.RowsMeaning = planString("Optimizer estimated rows")
		if d == "db2" {
			n.CostMeaning = planString("Db2 optimizer total cost (timerons)")
		} else {
			n.CostMeaning = planString("Oracle optimizer cost")
		}
		for name, i := range cols {
			if row[i] != nil {
				n.Attributes[name] = planText(row[i])
			}
		}
		for _, name := range []string{"ACCESS_PREDICATES", "FILTER_PREDICATES"} {
			if v := planText(value(name)); v != "" {
				n.Predicates[name] = v
			}
		}
		if table := planText(value("OBJECT_NAME")); table != "" {
			n.Table = planString(table)
			relation := table
			if owner := planText(value("OBJECT_OWNER")); owner != "" {
				relation = owner + "." + table
			}
			n.Relation = planString(relation)
		}
		position, _ := strconv.Atoi(planText(value("POSITION")))
		records[id] = nativePlanRecord{n, parent, position}
		order = append(order, id)
	}

	return assemblePlan(records, order)
}

type planXMLElement struct {
	name     xml.Name
	attrs    map[string]string
	children []*planXMLElement
}

const showplanNamespace = "http://schemas.microsoft.com/sqlserver/2004/07/showplan"

func parseSQLServerPlan(raw string) ([]*PlanNode, error) {
	bad := errors.New("Unrecognized or incomplete SQL Server SHOWPLAN XML; inspect raw output")
	if len(raw) > planMaxBytes {
		return nil, bad
	}
	decoder := xml.NewDecoder(strings.NewReader(raw))
	stack := []*planXMLElement{}
	var root *planXMLElement
	count := 0
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, bad
		}
		switch t := token.(type) {
		case xml.CharData:
			if len(stack) == 0 && strings.TrimSpace(string(t)) != "" {
				return nil, bad
			}
		case xml.Directive:
			return nil, bad
		case xml.StartElement:
			count++
			if len(stack) > planMaxDepth*4 || count > planMaxNodes*32 || t.Name.Space != showplanNamespace {
				return nil, bad
			}
			el := &planXMLElement{name: t.Name, attrs: map[string]string{}}
			for _, a := range t.Attr {
				if a.Name.Space == "" {
					el.attrs[a.Name.Local] = a.Value
				}
			}
			if len(stack) == 0 {
				if root != nil {
					return nil, bad
				}
				root = el
			} else {
				parent := stack[len(stack)-1]
				parent.children = append(parent.children, el)
			}
			stack = append(stack, el)
		case xml.EndElement:
			if len(stack) == 0 {
				return nil, bad
			}
			stack = stack[:len(stack)-1]
		}
	}
	if root == nil || root.name.Local != "ShowPlanXML" || len(stack) != 0 {
		return nil, bad
	}
	statements := 0
	var stmt *planXMLElement
	var scan func(*planXMLElement)
	scan = func(el *planXMLElement) {
		if el.name.Local == "StmtSimple" {
			statements++
			stmt = el
		}
		for _, c := range el.children {
			scan(c)
		}
	}
	scan(root)
	if statements != 1 || strings.ToUpper(stmt.attrs["StatementType"]) != "SELECT" {
		return nil, bad
	}
	roots := []*PlanNode{}
	seen := map[string]bool{}
	nodes := 0
	var visit func(*planXMLElement, *PlanNode, int) error
	visit = func(el *planXMLElement, parent *PlanNode, depth int) error {
		if el.name.Local == "RelOp" {
			id, op := el.attrs["NodeId"], el.attrs["PhysicalOp"]
			nodes++
			if id == "" || op == "" || seen[id] || nodes > planMaxNodes || depth >= planMaxDepth {
				return bad
			}
			seen[id] = true
			n := newPlanNode("n"+strconv.Itoa(nodes), op)
			n.NativeID = planString(id)
			n.PhysicalOperator = planString(op)
			n.LogicalOperator = planString(el.attrs["LogicalOp"])
			n.NodeType = nativePlanType(op + " " + el.attrs["LogicalOp"])
			n.Rows = planNumber(el.attrs["EstimateRows"])
			n.Cost = planNumber(el.attrs["EstimatedTotalSubtreeCost"])
			n.RowsMeaning = planString("SQL Server estimated rows")
			n.CostMeaning = planString("SQL Server estimated subtree cost")
			for k, v := range el.attrs {
				n.Attributes[k] = v
			}
			var details func(*planXMLElement)
			details = func(x *planXMLElement) {
				if x != el && x.name.Local == "RelOp" {
					return
				}
				if x.name.Local == "Object" && n.Table == nil {
					n.Table = planString(x.attrs["Table"])
					parts := []string{}
					for _, k := range []string{"Database", "Schema", "Table"} {
						if x.attrs[k] != "" {
							parts = append(parts, x.attrs[k])
						}
					}
					n.Relation = planString(strings.Join(parts, "."))
					if x.attrs["Index"] != "" {
						n.Attributes["Index"] = x.attrs["Index"]
					}
				}
				if x.name.Local == "Predicate" || x.name.Local == "SeekPredicates" || x.name.Local == "ProbeResidual" || x.name.Local == "Residual" || x.name.Local == "StartupExpressionPredicate" {
					values := []string{}
					var scalar func(*planXMLElement)
					scalar = func(y *planXMLElement) {
						if y.name.Local == "RelOp" {
							return
						}
						if v := y.attrs["ScalarString"]; v != "" {
							values = append(values, v)
							return
						}
						for _, c := range y.children {
							scalar(c)
						}
					}
					scalar(x)
					if len(values) > 0 {
						n.Predicates[x.name.Local] = strings.Join(values, "; ")
					}
				}
				for _, c := range x.children {
					details(c)
				}
			}
			details(el)
			if parent == nil {
				roots = append(roots, n)
			} else {
				parent.Children = append(parent.Children, n)
			}
			parent = n
			depth++
		}
		for _, c := range el.children {
			if err := visit(c, parent, depth); err != nil {
				return err
			}
		}
		return nil
	}
	if err := visit(stmt, nil, 0); err != nil {
		return nil, err
	}
	if len(roots) != 1 {
		return nil, bad
	}
	return roots, nil
}

var damengPlanLine = regexp.MustCompile(`^(\s*)(\d+)[ \t](\s*)#?([^:\[]+?)\s*:?\s*\[\s*([^,\]]+)\s*,\s*([^,\]]+)\s*,\s*([^\]]+)\s*\](.*)$`)

func parseDamengPlan(raw string) ([]*PlanNode, error) {
	bad := errors.New("Unrecognized or incomplete DM EXPLAIN text; inspect raw output")
	records := map[string]nativePlanRecord{}
	order := []string{}
	stack := []string{}
	indents := []int{}
	base := -1
	visual := func(s string) int {
		n := 0
		for _, c := range s {
			if c == '\t' {
				n += 4
			} else {
				n++
			}
		}
		return n
	}
	for _, line := range strings.Split(raw, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		m := damengPlanLine.FindStringSubmatch(line)
		if len(m) != 9 {
			if len(stack) == 0 {
				return nil, bad
			}
			n := records[stack[len(stack)-1]].node
			detail := strings.TrimSpace(line)
			if n.Detail != nil {
				detail = *n.Detail + "\n" + detail
			}
			n.Detail = planString(detail)
			continue
		}
		indent := visual(m[1]) + visual(m[3])
		if base < 0 {
			base = indent
		}
		if indent < base {
			return nil, bad
		}
		for len(indents) > 0 && indents[len(indents)-1] >= indent {
			indents = indents[:len(indents)-1]
			stack = stack[:len(stack)-1]
		}
		parent := ""
		if len(stack) > 0 {
			parent = stack[len(stack)-1]
		} else if len(order) > 0 {
			return nil, bad
		}
		id := m[2]
		if _, ok := records[id]; ok {
			return nil, bad
		}
		op := strings.TrimSpace(m[4])
		n := newPlanNode("n"+strconv.Itoa(len(order)+1), op)
		n.NativeID = planString(id)
		n.NodeType = nativePlanType(op)
		n.Cost = planNumber(strings.TrimSpace(m[5]))
		n.Rows = planNumber(strings.TrimSpace(m[6]))
		n.CostMeaning = planString("DM optimizer cost")
		n.RowsMeaning = planString("DM estimated rows")
		n.Attributes["bytes"] = strings.TrimSpace(m[7])
		n.Detail = planString(strings.TrimSpace(m[8]))
		records[id] = nativePlanRecord{node: n, parent: parent}
		order = append(order, id)
		stack = append(stack, id)
		indents = append(indents, indent)
	}
	return assemblePlan(records, order)
}

// Db2 reports a graph. Shared operators are rendered per incoming stream with
// their native identity retained and a Shared Subplan attribute, never assigned
// a fabricated unique database parent or counted as independent executions.
func parseDB2Captured(captures map[string]*Result) ([]*PlanNode, error) {
	bad := errors.New("Incomplete or cyclic Db2 operator/stream graph; inspect native tables")
	operators, streams := captures["EXPLAIN_OPERATOR"], captures["EXPLAIN_STREAM"]
	if operators == nil || streams == nil {
		return nil, bad
	}
	oc, err := planColumnIndex(operators)
	if err != nil {
		return nil, err
	}
	sc, err := planColumnIndex(streams)
	if err != nil {
		return nil, err
	}
	for _, k := range []string{"OPERATOR_ID", "OPERATOR_TYPE"} {
		if _, ok := oc[k]; !ok {
			return nil, bad
		}
	}
	for _, k := range []string{"SOURCE_TYPE", "SOURCE_ID", "TARGET_TYPE", "TARGET_ID", "STREAM_COUNT"} {
		if _, ok := sc[k]; !ok {
			return nil, bad
		}
	}
	get := func(row []any, cols map[string]int, k string) string {
		if i, ok := cols[k]; ok {
			return planText(row[i])
		}
		return ""
	}
	ops := map[string][]any{}
	order := []string{}
	children := map[string][][]any{}
	parents := map[string]int{}
	outgoing := map[string][][]any{}
	for _, row := range operators.Rows {
		id := get(row, oc, "OPERATOR_ID")
		if id == "" || get(row, oc, "OPERATOR_TYPE") == "" || ops[id] != nil {
			return nil, bad
		}
		ops[id] = row
		order = append(order, id)
	}
	if len(ops) == 0 || len(ops) > planMaxNodes {
		return nil, bad
	}
	for _, row := range streams.Rows {
		if get(row, sc, "SOURCE_TYPE") == "O" {
			id := get(row, sc, "SOURCE_ID")
			if ops[id] == nil {
				return nil, bad
			}
			outgoing[id] = append(outgoing[id], row)
			if get(row, sc, "TARGET_TYPE") == "O" {
				target := get(row, sc, "TARGET_ID")
				if ops[target] == nil {
					return nil, bad
				}
				children[target] = append(children[target], row)
				parents[id]++
			}
		} else if get(row, sc, "TARGET_TYPE") == "O" && ops[get(row, sc, "TARGET_ID")] == nil {
			return nil, bad
		}
	}
	roots := []*PlanNode{}
	displayCount := 0
	reachable := map[string]bool{}
	var convert func(string, []any, map[string]bool, int) (*PlanNode, error)
	convert = func(id string, incoming []any, path map[string]bool, depth int) (*PlanNode, error) {
		displayCount++
		if path[id] || depth > planMaxDepth || displayCount > planMaxNodes {
			return nil, bad
		}
		path[id] = true
		defer delete(path, id)
		reachable[id] = true
		row := ops[id]
		op := get(row, oc, "OPERATOR_TYPE")
		n := newPlanNode("n"+strconv.Itoa(displayCount), op)
		n.NativeID = planString(id)
		n.NodeType = nativePlanType(op)
		n.Cost = planNumber(get(row, oc, "TOTAL_COST"))
		n.CostMeaning = planString("Db2 optimizer total cost (timerons)")
		n.RowsMeaning = planString("Db2 estimated stream rows")
		for k, i := range oc {
			if row[i] != nil {
				n.Attributes["Operator."+k] = planText(row[i])
			}
		}
		if incoming != nil {
			n.Rows = planNumber(get(incoming, sc, "STREAM_COUNT"))
			for k, i := range sc {
				if incoming[i] != nil {
					n.Attributes["IncomingStream."+k] = planText(incoming[i])
				}
			}
		} else if len(outgoing[id]) == 1 {
			n.Rows = planNumber(get(outgoing[id][0], sc, "STREAM_COUNT"))
		}
		if parents[id] > 1 {
			n.Attributes["Shared Subplan"] = "true"
		}
		for _, edge := range children[id] {
			child, err := convert(get(edge, sc, "SOURCE_ID"), edge, path, depth+1)
			if err != nil {
				return nil, err
			}
			n.Children = append(n.Children, child)
		}
		return n, nil
	}
	for _, id := range order {
		if parents[id] == 0 {
			n, err := convert(id, nil, map[string]bool{}, 1)
			if err != nil {
				return nil, err
			}
			roots = append(roots, n)
		}
	}
	if len(roots) == 0 || len(reachable) != len(ops) {
		return nil, bad
	}
	return roots, nil
}
