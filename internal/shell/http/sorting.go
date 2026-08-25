package http

import (
	"net/url"

	"insights-scheduler/internal/core/domain"
)

// sortQueryParam is the query parameter used to request a sort order.
// The value has the form "field:direction", e.g. "name:asc" or "created_at:desc".
// The direction is optional and defaults to ascending.
const sortQueryParam = "sort_by"

// parseSortParam reads the sort_by query parameter and validates it against the
// supplied allowlist, returning def when the parameter is absent. Parsing and
// validation live in the functional core (domain.ParseSortSpec); this shell
// helper only extracts the raw string from the request URL.
func parseSortParam(u *url.URL, allowed map[string]struct{}, def domain.SortSpec) (domain.SortSpec, error) {
	return domain.ParseSortSpec(u.Query().Get(sortQueryParam), allowed, def)
}
