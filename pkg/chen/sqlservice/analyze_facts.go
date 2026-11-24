package sqlservice

import "github.com/jumpserver/koko/pkg/chen/sqlanalysis"

func analysisFromFacts(document string, facts sqlanalysis.Result, parseErr error) (Analysis, error) {
	out := Analysis{Syntax: facts.Syntax, Compatibility: facts.Compatibility, Diagnostics: facts.Diagnostics, Statements: []Statement{}}
	if parseErr != nil {
		return out, parseErr
	}
	last := 0
	for i := range facts.Statements {
		f := &facts.Statements[i]
		// Adapter ranges are validated before exposing any executable source.
		if !f.Span.ValidFor(document) || f.Span.Start < last {
			return Analysis{Syntax: sqlanalysis.Unknown, Compatibility: sqlanalysis.Unknown, Statements: []Statement{}}, &sqlanalysis.Error{Diagnostic: sqlanalysis.Diagnostic{Code: sqlanalysis.Unsupported, Message: "Parser returned an invalid original source range"}}
		}
		last = f.Span.End
		s := Statement{SQL: document[f.Span.Start:f.Span.End], Start: f.Span.Start, End: f.Span.End, Kind: string(f.Kind), ReturnsRows: f.ReturnsRows, MayWrite: f.MayWrite, Facts: f}
		for _, p := range f.PolicySpans {
			if !p.ValidFor(document) || p.Start < f.Span.Start || p.End > f.Span.End || len(s.PolicySQL) >= 1000 {
				return Analysis{}, &sqlanalysis.Error{Diagnostic: sqlanalysis.Diagnostic{Code: sqlanalysis.Unsupported, Message: "Nested SQL does not have a valid policy-visible source range"}}
			}
			s.PolicySQL = append(s.PolicySQL, document[p.Start:p.End])
		}
		out.Statements = append(out.Statements, s)
	}
	return out, nil
}
