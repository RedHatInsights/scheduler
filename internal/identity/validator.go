package identity

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
)

// UserValidator defines the interface for validating users and generating identity headers
type UserValidator interface {
	// GenerateIdentityHeader creates a Red Hat identity header from org-id and username
	GenerateIdentityHeader(orgID, username string) (string, error)
}

// Identity represents the Red Hat identity structure
type Identity struct {
	AccountNumber string `json:"account_number"`
	OrgID         string `json:"org_id"`
	Type          string `json:"type"`
	AuthType      string `json:"auth_type"`
	Internal      struct {
		OrgID string `json:"org_id"`
	} `json:"internal"`
	User struct {
		Username string `json:"username"`
		UserID   string `json:"user_id"`
	} `json:"user"`
	System struct {
		CN       string `json:"cn"`
		CertType string `json:"cert_type"`
	} `json:"system"`
}

// IdentityHeader represents the x-rh-identity header structure
type IdentityHeader struct {
	Identity Identity `json:"identity"`
}

// DefaultUserValidator is a concrete implementation of UserValidator
type DefaultUserValidator struct {
	defaultAccountNumber string
}

// NewDefaultUserValidator creates a new DefaultUserValidator with a default account number
func NewDefaultUserValidator(defaultAccountNumber string) *DefaultUserValidator {
	fmt.Println("Using FAKE user validator")
	return &DefaultUserValidator{
		defaultAccountNumber: defaultAccountNumber,
	}
}

// GenerateIdentityHeader creates an identity header from org-id and username
func (v *DefaultUserValidator) GenerateIdentityHeader(orgID, username string) (string, error) {
	if orgID == "" {
		return "", fmt.Errorf("orgID cannot be empty")
	}
	if username == "" {
		return "", fmt.Errorf("username cannot be empty")
	}

	identity := IdentityHeader{
		Identity: Identity{
			AccountNumber: v.defaultAccountNumber,
			OrgID:         orgID,
			Type:          "User",
			AuthType:      "jwt-auth",
			Internal: struct {
				OrgID string `json:"org_id"`
			}{
				OrgID: orgID,
			},
			User: struct {
				Username string `json:"username"`
				UserID   string `json:"user_id"`
			}{
				Username: username,
				UserID:   username + "-id",
			},
		},
	}

	identityJSON, err := json.Marshal(identity)
	if err != nil {
		return "", fmt.Errorf("failed to marshal identity: %w", err)
	}

	return base64.StdEncoding.EncodeToString(identityJSON), nil
}
