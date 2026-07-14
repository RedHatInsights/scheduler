package http

import (
	"net/http"

	"github.com/redhatinsights/platform-go-middlewares/v2/identity"

	"insights-scheduler/internal/config"
)

func OrgIDGuardMiddleware(cfg config.OrgIDGuardConfig) func(http.Handler) http.Handler {
	allowed := make(map[string]struct{}, len(cfg.AllowedOrgIDs))
	for _, id := range cfg.AllowedOrgIDs {
		allowed[id] = struct{}{}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			orgID := identity.Get(r.Context()).Identity.OrgID
			if _, ok := allowed[orgID]; !ok {
				logger := GetLogger(r)
				logger.Warn("org-id-guard: access denied", "org_id", orgID)
				respondWithError(w, http.StatusForbidden,
					"Forbidden",
					"Your organization does not have access to this service",
				)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
