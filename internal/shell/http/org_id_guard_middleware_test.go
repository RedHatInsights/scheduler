package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/redhatinsights/platform-go-middlewares/v2/identity"

	"insights-scheduler/internal/config"
)

func withIdentity(r *http.Request, orgID string) *http.Request {
	xrhid := identity.XRHID{
		Identity: identity.Identity{
			OrgID: orgID,
			User:  &identity.User{Username: "test", UserID: "123"},
		},
	}
	ctx := identity.WithIdentity(r.Context(), xrhid)
	return r.WithContext(ctx)
}

func TestOrgIDGuardMiddleware_AllowedOrg(t *testing.T) {
	cfg := config.OrgIDGuardConfig{
		Enabled:       true,
		AllowedOrgIDs: []string{"org1", "org2"},
	}

	called := false
	handler := OrgIDGuardMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req = withIdentity(req, "org1")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	if !called {
		t.Error("expected next handler to be called")
	}
}

func TestOrgIDGuardMiddleware_DeniedOrg(t *testing.T) {
	cfg := config.OrgIDGuardConfig{
		Enabled:       true,
		AllowedOrgIDs: []string{"org1", "org2"},
	}

	called := false
	handler := OrgIDGuardMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req = withIdentity(req, "org999")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rr.Code)
	}
	if called {
		t.Error("expected next handler NOT to be called")
	}

	var errResp ErrorResponse
	if err := json.NewDecoder(rr.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if len(errResp.Errors) != 1 {
		t.Fatalf("expected 1 error, got %d", len(errResp.Errors))
	}
	if errResp.Errors[0].Title != "Forbidden" {
		t.Errorf("expected title 'Forbidden', got '%s'", errResp.Errors[0].Title)
	}
}

func TestOrgIDGuardMiddleware_EmptyAllowlist(t *testing.T) {
	cfg := config.OrgIDGuardConfig{
		Enabled:       true,
		AllowedOrgIDs: []string{},
	}

	handler := OrgIDGuardMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("expected next handler NOT to be called")
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req = withIdentity(req, "org1")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rr.Code)
	}
}
