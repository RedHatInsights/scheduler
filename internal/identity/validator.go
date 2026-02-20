package identity

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"

	platformIdentity "github.com/redhatinsights/platform-go-middlewares/v2/identity"
)

// UserValidator defines the interface for validating users and generating identity headers
type UserValidator interface {
	// GenerateIdentityHeader creates a Red Hat identity header from org-id, username, and userID
	GenerateIdentityHeader(ctx context.Context, orgID, username, userID string) (string, error)
}

// FakeUserValidator is a concrete implementation of UserValidator for testing/development
type FakeUserValidator struct{}

// NewFakeUserValidator creates a new FakeUserValidator
func NewFakeUserValidator() *FakeUserValidator {
	return &FakeUserValidator{}
}

// GenerateIdentityHeader creates an identity header from org-id, username, and userID
func (v *FakeUserValidator) GenerateIdentityHeader(ctx context.Context, orgID, username, userID string) (string, error) {
	if orgID == "" {
		return "", fmt.Errorf("orgID cannot be empty")
	}
	if username == "" {
		return "", fmt.Errorf("username cannot be empty")
	}
	if userID == "" {
		return "", fmt.Errorf("userID cannot be empty")
	}

	identity := platformIdentity.XRHID{
		Identity: platformIdentity.Identity{
			AccountNumber: "000002",
			OrgID:         orgID,
			Type:          "User",
			AuthType:      "jwt-auth",
			Internal: platformIdentity.Internal{
				OrgID: orgID,
			},
			User: &platformIdentity.User{
				Username: username,
				UserID:   userID,
				Email:    "testuser@testcorp.com",
			},
		},
	}

	identityJSON, err := json.Marshal(identity)
	if err != nil {
		return "", fmt.Errorf("failed to marshal identity: %w", err)
	}

	return base64.StdEncoding.EncodeToString(identityJSON), nil
}
