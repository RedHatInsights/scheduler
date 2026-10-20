package http

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/redhatinsights/platform-go-middlewares/v2/identity"
)

// buildUserMiddlewareChain wires identity.EnforceIdentity (which decodes and
// validates the x-rh-identity header) in front of EnforceUserIdentity, exactly
// as the REST API router does. The final handler records whether it was reached
// and what User the middleware placed in the context.
func buildUserMiddlewareChain(t *testing.T, wantUser User) (http.Handler, *bool) {
	t.Helper()

	called := false
	final := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true

		user, ok := GetUserIdentity(r.Context())
		if !ok {
			t.Error("expected a User in the request context")
			return
		}
		if user.AccountID != wantUser.AccountID {
			t.Errorf("AccountID: expected %q, got %q", wantUser.AccountID, user.AccountID)
		}
		if user.OrganizationID != wantUser.OrganizationID {
			t.Errorf("OrganizationID: expected %q, got %q", wantUser.OrganizationID, user.OrganizationID)
		}
		if user.Username != wantUser.Username {
			t.Errorf("Username: expected %q, got %q", wantUser.Username, user.Username)
		}

		w.WriteHeader(http.StatusOK)
	})

	return identity.EnforceIdentity(EnforceUserIdentity(final)), &called
}

func TestEnforceUserIdentity(t *testing.T) {
	tests := []struct {
		name string
		// identityJSON is the raw x-rh-identity JSON; it is base64 encoded and
		// sent in the X-Rh-Identity header. Empty means no header at all.
		identityJSON   string
		wantStatus     int
		wantUser       User
		wantHandlerHit bool
	}{
		{
			name:         "no identity header is rejected",
			identityJSON: "",
			wantStatus:   http.StatusBadRequest, // rejected by identity.EnforceIdentity
		},
		{
			name:           "valid user is allowed",
			identityJSON:   `{ "identity": {"account_number": "540155", "auth_type": "jwt-auth", "org_id": "1979710", "internal": {"org_id": "1979710"}, "type": "User", "user": {"username": "username", "email": "boring@boring.mail.com", "first_name": "Jake", "last_name": "Logan", "is_active": true, "is_org_admin": false, "is_internal": true, "locale": "North America", "user_id": "1010101"} } }`,
			wantStatus:     http.StatusOK,
			wantUser:       User{AccountID: "540155", OrganizationID: "1979710", Username: "username"},
			wantHandlerHit: true,
		},
		{
			name:         "service account is forbidden",
			identityJSON: `{ "identity": {"account_number": "540155", "auth_type": "jwt-auth", "org_id": "1979710", "internal": {"org_id": "1979710"}, "type": "ServiceAccount", "service_account": { "client_id": "b69eaf9e-e6a6-4f9e-805e-02987daddfbd", "username": "service-account-username" } } }`,
			wantStatus:   http.StatusForbidden,
		},
		{
			name:         "cert/system identity is forbidden",
			identityJSON: `{ "identity": {"account_number": "540155", "auth_type": "cert-auth", "org_id": "1979710", "internal": {"org_id": "1979710"}, "type": "System", "system": { "cn": "deadbeef-e6a6-4f9e-805e-02987daddfbd" } } }`,
			wantStatus:   http.StatusForbidden,
		},
		{
			name:         "associate identity is forbidden",
			identityJSON: `{ "identity": {"account_number": "540155", "auth_type": "jwt-auth", "org_id": "1979710", "internal": {"org_id": "1979710"}, "type": "Associate", "user": {"username": "username", "email": "boring@boring.mail.com", "is_active": true, "user_id": "1010101"} } }`,
			wantStatus:   http.StatusForbidden,
		},
		{
			name:         "user with empty username is a bad request",
			identityJSON: `{ "identity": {"account_number": "540155", "auth_type": "jwt-auth", "org_id": "1979710", "internal": {"org_id": "1979710"}, "type": "User", "user": {"username": "", "email": "boring@boring.mail.com", "is_active": true, "user_id": "1010101"} } }`,
			wantStatus:   http.StatusBadRequest,
		},
		{
			name:         "user with missing user object is a bad request",
			identityJSON: `{ "identity": {"account_number": "540155", "auth_type": "jwt-auth", "org_id": "1979710", "internal": {"org_id": "1979710"}, "type": "User" } }`,
			wantStatus:   http.StatusBadRequest,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			handler, called := buildUserMiddlewareChain(t, tc.wantUser)

			req := httptest.NewRequest("GET", "/test", nil)
			if tc.identityJSON != "" {
				encoded := base64.StdEncoding.EncodeToString([]byte(tc.identityJSON))
				req.Header.Set("X-Rh-Identity", encoded)
			}

			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != tc.wantStatus {
				t.Errorf("expected status %d, got %d (body: %s)", tc.wantStatus, rr.Code, rr.Body.String())
			}
			if *called != tc.wantHandlerHit {
				t.Errorf("expected handler-called=%v, got %v", tc.wantHandlerHit, *called)
			}
		})
	}
}
