package storage

import "insights-scheduler/internal/core/domain"

// Column allowlists mapping validated API sort fields to physical database
// columns. These are the ONLY strings ever interpolated into an ORDER BY clause.
// Combined with domain-level allowlist validation of the incoming field name,
// this guarantees no untrusted input can reach the SQL statement.
var jobSortColumns = map[string]string{
	"name":        "name",
	"status":      "status",
	"created_at":  "created_at",
	"next_run_at": "next_run_at",
	"last_run_at": "last_run_at",
}

var jobRunSortColumns = map[string]string{
	"start_time": "start_time",
	"end_time":   "end_time",
	"status":     "status",
	"created_at": "created_at",
}

// buildOrderByClause renders a safe "ORDER BY <alias.>col DIR" fragment.
//
// The column is resolved via columns (a fixed allowlist of literals); if the
// field is unrecognized it falls back to defaultColumn, so an unexpected value
// can never be concatenated verbatim. alias, when non-empty, prefixes the column
// for queries that join and alias the table (e.g. "jr").
func buildOrderByClause(sort domain.SortSpec, columns map[string]string, defaultColumn, alias string) string {
	col, ok := columns[sort.Field]
	if !ok {
		col = defaultColumn
	}

	dir := "ASC"
	if sort.Direction == domain.SortDesc {
		dir = "DESC"
	}

	if alias != "" {
		col = alias + "." + col
	}

	return "ORDER BY " + col + " " + dir
}
