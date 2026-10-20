/*
Copyright 2022 Red Hat Inc.
SPDX-License-Identifier: Apache-2.0
*/
package http

import (
	"context"
	"net/http"
	"strings"

	"github.com/redhatinsights/platform-go-middlewares/v2/identity"
)

type userIdentityKey int

// UserIdentityKey is the context key under which the authenticated User is stored.
const UserIdentityKey userIdentityKey = iota

// userType is the only x-rh-identity type permitted to call the REST API.
// Service accounts ("serviceaccount"), certificate/system identities ("system"),
// and any other principal type are rejected.
const userType = "user"

// User is the authenticated human user extracted from the x-rh-identity header.
type User struct {
	AccountID      string
	OrganizationID string
	Username       string
}

// EnforceUserIdentity is a middleware that requires the request to be made by a
// human user (identity type "user"). Requests authenticated as service accounts,
// certificates/systems, or any other principal type are rejected with 403.
//
// It must be chained after identity.EnforceIdentity, which is responsible for
// decoding and validating the x-rh-identity header into the request context.
// On success, the parsed User is stored in the request context under
// UserIdentityKey.
func EnforceUserIdentity(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := identity.Get(r.Context())

		identityType := strings.ToLower(id.Identity.Type)
		if identityType != userType {
			GetLogger(r).Warn("user-identity: access denied for non-user principal",
				"identity_type", id.Identity.Type,
				"org_id", id.Identity.OrgID,
			)
			respondWithError(w, http.StatusForbidden,
				"Forbidden",
				"This API may only be called by users; service accounts and certificates are not permitted",
			)
			return
		}

		if id.Identity.User == nil {
			respondWithError(w, http.StatusBadRequest,
				"Invalid Identity",
				"The user identity is missing user data",
			)
			return
		}

		if len(id.Identity.User.Username) == 0 {
			// The security model is currently based on the username, so verify we
			// are getting a valid (non-empty) username.
			respondWithError(w, http.StatusBadRequest,
				"Invalid Identity",
				"The user identity is missing a username",
			)
			return
		}

		user := User{
			AccountID:      id.Identity.AccountNumber,
			OrganizationID: id.Identity.OrgID,
			Username:       id.Identity.User.Username,
		}

		ctx := context.WithValue(r.Context(), UserIdentityKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// GetUserIdentity returns the authenticated User stored in the request context
// by EnforceUserIdentity. The second return value is false if no user is present.
func GetUserIdentity(ctx context.Context) (User, bool) {
	user, ok := ctx.Value(UserIdentityKey).(User)
	return user, ok
}
