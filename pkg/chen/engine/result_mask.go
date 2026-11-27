package engine

import (
	"github.com/jumpserver/koko/pkg/chen/dialect"
	"github.com/jumpserver/koko/pkg/chen/sqlservice"
)

func (e *execution) resultMaskColumns(d dialect.ID, labels []string) []string {
	if e.objectPlan != nil {
		// A routine controls the origin of its output columns. Labels alone
		// cannot prove that an alias excludes a protected source column.
		return make([]string, len(labels))
	}
	if e.browse != nil && e.browse.plan != nil && !e.browse.plan.table.View {
		// Browsing selects the verified table's physical columns without
		// aliases; this also covers full exports and continuous scroll batches.
		return append([]string(nil), labels...)
	}
	p := e.querySource
	if e.queryPage != nil {
		p = e.queryPage
	}
	if p == nil {
		return append([]string(nil), labels...)
	}
	return sqlservice.ResultMaskColumns(d, p.original, p.options, labels)
}
