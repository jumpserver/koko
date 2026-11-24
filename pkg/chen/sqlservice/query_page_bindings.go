package sqlservice

// These lexical scopes describe only the closed replay grammar. They are not
// edit provenance and never cross the wire. The worker resolves base columns
// from the same connection before treating an Oracle bare name as a column.
type QueryPageScope struct {
	Parent      *QueryPageScope
	Sources     []QueryPageBinding
	Projections []QueryPageProjection
	References  []QueryPageReference
}
type QueryPageBinding struct {
	Alias    string
	Relation *QueryPageRelation
	Query    *QueryPageScope
	Columns  []string // explicit CTE/derived header; nil means derive projection
}
type QueryPageProjection struct {
	Name      string
	Star      bool
	Qualifier string
}
type QueryPageReference struct {
	Parts       []string
	OutputAlias bool // only a standalone final ORDER BY output identifier
}
