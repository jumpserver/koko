package engine

import (
	"encoding/csv"
	"fmt"
	"strings"
)

// Use only the published, masked cell values. The original values retained for
// primary-key writes must never enter session replay or command audit.
func appendResultReplay(e *execution, result *Result) {
	var text strings.Builder
	writer := csv.NewWriter(&text)
	writer.Comma = '\t'
	headers := make([]string, len(result.Columns))
	for i, column := range result.Columns {
		headers[i] = column.Name
	}
	_ = writer.Write(headers)
	for _, row := range result.Rows {
		values := make([]string, len(row))
		for i, value := range row {
			if value == nil {
				values[i] = "NULL"
			} else {
				values[i] = fmt.Sprint(value)
			}
		}
		_ = writer.Write(values)
	}
	writer.Flush()
	if e.replayOutput != "" {
		e.replayOutput += "\n"
	}
	e.replayOutput += text.String()
	e.auditRows += len(result.Rows)
}
