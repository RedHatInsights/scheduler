package identity

import (
	"encoding/base64"
	"encoding/json"
	"testing"
)

func TestDefaultUserValidator_GenerateIdentityHeader(t *testing.T) {
	validator := NewDefaultUserValidator("000001")

	tests := []struct {
		name     string
		orgID    string
		username string
		wantErr  bool
	}{
		{
			name:     "Valid org and user",
			orgID:    "test-org",
			username: "testuser",
			wantErr:  false,
		},
		{
			name:     "Empty orgID",
			orgID:    "",
			username: "testuser",
			wantErr:  true,
		},
		{
			name:     "Empty username",
			orgID:    "test-org",
			username: "",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			identityHeader, err := validator.GenerateIdentityHeader(tt.orgID, tt.username)

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

			var identity IdentityHeader
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

			if identity.Identity.AccountNumber != "000001" {
				t.Errorf("Expected AccountNumber '000001', got '%s'", identity.Identity.AccountNumber)
			}

			if identity.Identity.AuthType != "jwt-auth" {
				t.Errorf("Expected AuthType 'jwt-auth', got '%s'", identity.Identity.AuthType)
			}

			if identity.Identity.Internal.OrgID != tt.orgID {
				t.Errorf("Expected Internal.OrgID '%s', got '%s'", tt.orgID, identity.Identity.Internal.OrgID)
			}

			expectedUserID := tt.username + "-id"
			if identity.Identity.User.UserID != expectedUserID {
				t.Errorf("Expected UserID '%s', got '%s'", expectedUserID, identity.Identity.User.UserID)
			}
		})
	}
}

func TestNewDefaultUserValidator(t *testing.T) {
	accountNumber := "123456"
	validator := NewDefaultUserValidator(accountNumber)

	if validator == nil {
		t.Fatal("Expected non-nil validator")
	}

	if validator.defaultAccountNumber != accountNumber {
		t.Errorf("Expected defaultAccountNumber '%s', got '%s'", accountNumber, validator.defaultAccountNumber)
	}
}

func TestDefaultUserValidator_Interface(t *testing.T) {
	// Verify that DefaultUserValidator implements UserValidator interface
	var _ UserValidator = (*DefaultUserValidator)(nil)
}
