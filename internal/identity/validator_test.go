package identity

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"

	platformIdentity "github.com/redhatinsights/platform-go-middlewares/identity"
)

func TestFakeUserValidator_GenerateIdentityHeader(t *testing.T) {
	validator := NewFakeUserValidator()

	tests := []struct {
		name     string
		orgID    string
		username string
		userID   string
		wantErr  bool
	}{
		{
			name:     "Valid org and user",
			orgID:    "test-org",
			username: "testuser",
			userID:   "test-user-id",
			wantErr:  false,
		},
		{
			name:     "Empty orgID",
			orgID:    "",
			username: "testuser",
			userID:   "test-user-id",
			wantErr:  true,
		},
		{
			name:     "Empty username",
			orgID:    "test-org",
			username: "",
			userID:   "test-user-id",
			wantErr:  true,
		},
		{
			name:     "Empty userID",
			orgID:    "test-org",
			username: "testuser",
			userID:   "",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			identityHeader, err := validator.GenerateIdentityHeader(context.Background(), tt.orgID, tt.username, tt.userID)

			if tt.wantErr {
				if err == nil {
					t.Errorf("GenerateIdentityHeader() expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("GenerateIdentityHeader() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if identityHeader == "" {
				t.Error("Expected non-empty identity header")
			}

			// Verify the header can be decoded
			decoded, err := base64.StdEncoding.DecodeString(identityHeader)
			if err != nil {
				t.Fatalf("Failed to decode identity header: %v", err)
			}

			var identity platformIdentity.XRHID
			if err := json.Unmarshal(decoded, &identity); err != nil {
				t.Fatalf("Failed to unmarshal identity: %v", err)
			}

			// Verify the content
			if identity.Identity.OrgID != tt.orgID {
				t.Errorf("Expected OrgID '%s', got '%s'", tt.orgID, identity.Identity.OrgID)
			}

			if identity.Identity.User.Username != tt.username {
				t.Errorf("Expected Username '%s', got '%s'", tt.username, identity.Identity.User.Username)
			}

			if identity.Identity.Type != "User" {
				t.Errorf("Expected Type 'User', got '%s'", identity.Identity.Type)
			}

			if identity.Identity.AccountNumber != "000002" {
				t.Errorf("Expected AccountNumber '000002', got '%s'", identity.Identity.AccountNumber)
			}

			if identity.Identity.AuthType != "jwt-auth" {
				t.Errorf("Expected AuthType 'jwt-auth', got '%s'", identity.Identity.AuthType)
			}

			if identity.Identity.Internal.OrgID != tt.orgID {
				t.Errorf("Expected Internal.OrgID '%s', got '%s'", tt.orgID, identity.Identity.Internal.OrgID)
			}

			if identity.Identity.User.UserID != tt.userID {
				t.Errorf("Expected UserID '%s', got '%s'", tt.userID, identity.Identity.User.UserID)
			}
		})
	}
}

func TestNewFakeUserValidator(t *testing.T) {
	validator := NewFakeUserValidator()

	if validator == nil {
		t.Fatal("Expected non-nil validator")
	}
}

func TestFakeUserValidator_Interface(t *testing.T) {
	// Verify that FakeUserValidator implements UserValidator interface
	var _ UserValidator = (*FakeUserValidator)(nil)
}
