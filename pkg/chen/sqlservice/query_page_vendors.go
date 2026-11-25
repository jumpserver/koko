package sqlservice

import (
	"errors"
	"fmt"
	"strings"
)

func (p *QueryPageSQL) pageAliases(columns []string) (aliases, projection []string) {
	aliases = make([]string, len(columns))
	projection = make([]string, len(columns))
	quote := byte('"')
	if p.Dialect == SQLServer {
		quote = '['
	}
	for i, name := range columns {
		aliases[i] = fmt.Sprintf("c%d", i)
		projection[i] = aliases[i] + " AS " + quoteIdentifier(name, p.Dialect, quote)
	}
	return
}
func (p *QueryPageSQL) pageSourceName() string {
	used := map[string]bool{}
	for name := range p.CTEs {
		used[strings.ToUpper(name)] = true
	}
	for _, r := range p.Relations {
		used[strings.ToUpper(r.Name)] = true
	}
	name := "CHEN_QUERY_SOURCE"
	for i := 1; used[name]; i++ {
		name = fmt.Sprintf("CHEN_QUERY_SOURCE_%d", i)
	}
	return name
}
func (p *QueryPageSQL) oraclePrefix(body string, columns int) (string, string) {
	aliases := make([]string, columns)
	for i := range aliases {
		aliases[i] = fmt.Sprintf("c%d", i)
	}
	name := p.pageSourceName()
	prefix := strings.TrimSpace(p.Prefix)
	if prefix == "" {
		prefix = "WITH "
	} else {
		prefix += ", "
	}
	return prefix + name + "(" + strings.Join(aliases, ",") + ") AS (\n" + body + "\n) ", name
}
func (p *QueryPageSQL) vendorPage(offset, size int, columns []string) string {
	body := p.Body
	if body == "" {
		body = p.Original
	}
	aliases, projection := p.pageAliases(columns)
	if p.Dialect == Dameng {
		if !p.HasWindow {
			return p.Prefix + fmt.Sprintf("%s LIMIT %d OFFSET %d", body, size, offset)
		}
		prefix, source := p.oraclePrefix(body, len(columns))
		order, err := p.derivedOrder(columns)
		if err != nil {
			return ""
		}
		return prefix + fmt.Sprintf("SELECT %s FROM %s%s LIMIT %d OFFSET %d", strings.Join(projection, ","), source, order, size, offset)
	}
	if p.Dialect == ClickHouse {
		if !p.HasWindow {
			return p.Prefix + fmt.Sprintf("%s LIMIT %d OFFSET %d", body, size, offset)
		}
		order, err := p.derivedOrder(columns)
		if err != nil {
			return ""
		}
		return p.Prefix + fmt.Sprintf("SELECT * FROM (\n%s\n) AS CHEN_QUERY_PAGE%s LIMIT %d OFFSET %d", body, order, size, offset)
	}
	if p.Dialect == SQLServer || p.Dialect == DB2 {
		input := p.bodyWithoutOuterOrder()
		order, err := p.derivedOrder(columns)
		if err != nil {
			return ""
		}
		window := strings.TrimPrefix(order, " ")
		if window == "" && p.Dialect == SQLServer {
			window = "ORDER BY (SELECT NULL)"
		}
		return p.Prefix + fmt.Sprintf("SELECT %s FROM (SELECT CHEN_INPUT.*,ROW_NUMBER() OVER (%s) AS CHEN_ROW_NUMBER FROM (\n%s\n) AS CHEN_INPUT(%s)) AS CHEN_NUMBERED WHERE CHEN_ROW_NUMBER>%d AND CHEN_ROW_NUMBER<=%d ORDER BY CHEN_ROW_NUMBER", strings.Join(projection, ","), window, input, strings.Join(aliases, ","), offset, offset+size)
	}

	prefix, source := p.oraclePrefix(body, len(columns))
	order, err := p.derivedOrder(columns)
	if err != nil {
		return ""
	}
	// ROWNUM is assigned after an explicit ordered inline view. A CTE's
	// internal ORDER BY alone does not promise its consumer's sequence.
	input := "(SELECT * FROM " + source + order + ")"
	return prefix + fmt.Sprintf("SELECT %s FROM (SELECT CHEN_INPUT.*,ROWNUM CHEN_ROW_NUMBER FROM %s CHEN_INPUT WHERE ROWNUM<=%d) WHERE CHEN_ROW_NUMBER>%d ORDER BY CHEN_ROW_NUMBER", strings.Join(projection, ","), input, offset+size, offset)
}
func (p *QueryPageSQL) vendorCount(columns int) string {
	query := p.Body
	if query == "" {
		query = p.Original
	}
	if IsOracleDialect(p.Dialect) {
		prefix, source := p.oraclePrefix(query, columns)
		return prefix + "SELECT COUNT(*) FROM " + source
	}
	if p.Dialect == SQLServer || p.Dialect == DB2 {
		query = p.bodyWithoutOuterOrder()
	}
	aliases := make([]string, columns)
	for i := range aliases {
		aliases[i] = fmt.Sprintf("c%d", i)
	}
	suffix := ""
	if (p.Dialect == SQLServer || p.Dialect == DB2) && columns > 0 {
		suffix = "(" + strings.Join(aliases, ",") + ")"
	}
	count := "COUNT(*)"
	if p.Dialect == SQLServer {
		count = "COUNT_BIG(*)"
	}
	if p.Dialect == DB2 {
		count = "SYSIBM.COUNT_BIG(*)"
	}
	return p.Prefix + "SELECT " + count + " FROM (\n" + query + "\n) AS CHEN_QUERY_COUNT" + suffix
}

// A wrapped query's inner ORDER BY selects its original TOP/FETCH subset; it
// does not promise the outer sequence. Map only proven output expressions,
// exact output aliases or ordinals, otherwise refuse that paging shape.
func (p *QueryPageSQL) derivedOrder(columns []string) (string, error) {
	if !p.Ordered {
		return "", nil
	}
	parts := []string{}
	for _, sort := range p.Order {
		index := -1
		if sort.Ordinal > 0 && sort.Ordinal <= len(columns) {
			index = sort.Ordinal - 1
		}
		if index < 0 && sort.Name != "" {
			for i, name := range columns {
				if name == sort.Name {
					if index >= 0 {
						if len(p.ProjectionKeys) == len(columns) && p.ProjectionKeys[i] == sort.Key && p.ProjectionKeys[index] == sort.Key {
							continue
						}
						return "", errors.New("Database paging cannot preserve an ambiguous output order; use unique projected SQL aliases")
					}
					index = i
				}
			}
		}
		if index < 0 && len(p.ProjectionKeys) == len(columns) {
			for i, key := range p.ProjectionKeys {
				if key == sort.Key {
					if index >= 0 {
						continue
					}
					index = i
				}
			}
		}
		if index < 0 {
			return "", errors.New("Database paging cannot preserve this original ORDER BY through its row window; project and order by explicit unique SQL aliases or use cached results")
		}
		name := fmt.Sprintf("c%d", index)
		if p.Dialect == ClickHouse {
			name = quoteIdentifier(columns[index], p.Dialect, '`')
		}
		if sort.Desc {
			name += " DESC"
		} else {
			name += " ASC"
		}
		if sort.Nulls != "" {
			name += " NULLS " + sort.Nulls
		}
		parts = append(parts, name)
	}
	if len(parts) == 0 {
		return "", errors.New("Database paging has no verified outer order mapping")
	}
	return " ORDER BY " + strings.Join(parts, ","), nil
}
func (p *QueryPageSQL) ValidatePageOrder(columns []string) error {
	if !p.Ordered || p.Dialect == PostgreSQL || IsMySQLDialect(p.Dialect) {
		return nil
	}
	if !p.HasWindow && !IsOracleDialect(p.Dialect) && p.Dialect != SQLServer && p.Dialect != DB2 {
		return nil
	}
	if p.Dialect == Dameng && !p.HasWindow {
		return nil
	}
	_, err := p.derivedOrder(columns)
	return err
}

// The grammar records this exact outer ORDER span. Only an unwindowed outer
// order may be removed before ROW_NUMBER/COUNT; nested orders, TOP and original
// row windows are preserved verbatim. No textual ORDER/LIMIT matching is used.
func (p *QueryPageSQL) bodyWithoutOuterOrder() string {
	body := p.Body
	if body == "" {
		body = p.Original
	}
	if p.HasWindow || !p.Ordered {
		return body
	}
	start, end := p.OrderStart-len(p.Prefix), p.OrderEnd-len(p.Prefix)
	if start < 0 || end < start || end > len(body) {
		return ""
	}
	return body[:start] + body[end:]
}
