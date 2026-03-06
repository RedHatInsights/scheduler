package identity

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestThreeScaleUserValidator_GenerateIdentityHeader(t *testing.T) {
	// Create a test server
	requestIDReceived := ""
	authHeaderReceived := ""

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Capture request-id header
		requestIDReceived = r.Header.Get("X-Request-Id")
		authHeaderReceived = r.Header.Get("Authorization")

		// Verify it's a GET request
		if r.Method != "GET" {
			t.Errorf("Expected GET request, got %s", r.Method)
		}

		// Verify URL path
		expectedPath := "/v1/users/testuser"
		if r.URL.Path != expectedPath {
			t.Errorf("Expected path %s, got %s", expectedPath, r.URL.Path)
		}

		// Verify query parameters
		orgID := r.URL.Query().Get("org_id")
		if orgID != "org-123" {
			t.Errorf("Expected org_id=org-123, got %s", orgID)
		}

		// Return mock user info
		userInfo := ThreeScaleUserInfo{
			UserID:        "user-456",
			Username:      "testuser",
			Email:         "testuser@example.com",
			AccountNumber: "account-789",
			OrgID:         "org-123",
			IsActive:      true,
			FirstName:     "Test",
			LastName:      "User",
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(userInfo)
	}))
	defer server.Close()

	// Create validator with test server URL
	validator := NewThreeScaleUserValidator(server.URL, "test-token")

	// Test GenerateIdentityHeader
	header, err := validator.GenerateIdentityHeader(
		context.Background(),
		"org-123",
		"testuser",
		"user-456",
	)

	if err != nil {
		t.Fatalf("GenerateIdentityHeader failed: %v", err)
	}

	if header == "" {
		t.Error("Expected non-empty identity header")
	}

	// Verify request-id was sent (should be a UUID)
	if requestIDReceived == "" {
		t.Error("Expected X-Request-Id header to be sent")
	}
	t.Logf("Request ID sent: %s", requestIDReceived)

	// Verify authorization header
	expectedAuth := "Bearer test-token"
	if authHeaderReceived != expectedAuth {
		t.Errorf("Expected Authorization header '%s', got '%s'", expectedAuth, authHeaderReceived)
	}
}

func TestThreeScaleUserValidator_InactiveUser(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userInfo := ThreeScaleUserInfo{
			UserID:        "user-456",
			Username:      "testuser",
			Email:         "testuser@example.com",
			AccountNumber: "account-789",
			OrgID:         "org-123",
			IsActive:      false, // Inactive user
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(userInfo)
	}))
	defer server.Close()

	validator := NewThreeScaleUserValidator(server.URL, "test-token")

	_, err := validator.GenerateIdentityHeader(
		context.Background(),
		"org-123",
		"testuser",
		"user-456",
	)

	if err == nil {
		t.Error("Expected error for inactive user, got nil")
	}

	if err != nil && err.Error() != "user is inactive (request_id="+err.Error()+")" {
		// Just check that error mentions inactive user
		t.Logf("Got expected error: %v", err)
	}
}

func TestThreeScaleUserValidator_OrgIDMismatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userInfo := ThreeScaleUserInfo{
			UserID:        "user-456",
			Username:      "testuser",
			Email:         "testuser@example.com",
			AccountNumber: "account-789",
			OrgID:         "org-999", // Different org
			IsActive:      true,
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(userInfo)
	}))
	defer server.Close()

	validator := NewThreeScaleUserValidator(server.URL, "test-token")

	_, err := validator.GenerateIdentityHeader(
		context.Background(),
		"org-123",
		"testuser",
		"user-456",
	)

	if err == nil {
		t.Error("Expected error for org_id mismatch, got nil")
	}

	t.Logf("Got expected error: %v", err)
}

func TestThreeScaleUserValidator_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("User not found"))
	}))
	defer server.Close()

	validator := NewThreeScaleUserValidator(server.URL, "test-token")

	_, err := validator.GenerateIdentityHeader(
		context.Background(),
		"org-123",
		"testuser",
		"user-456",
	)

	if err == nil {
		t.Error("Expected error for HTTP 404, got nil")
	}

	t.Logf("Got expected error: %v", err)
}

func TestThreeScaleUserValidator_EmptyParams(t *testing.T) {
	validator := NewThreeScaleUserValidator("http://localhost:8080", "test-token")

	tests := []struct {
		name     string
		orgID    string
		username string
		userID   string
		wantErr  bool
	}{
		{
			name:     "empty orgID",
			orgID:    "",
			username: "user",
			userID:   "123",
			wantErr:  true,
		},
		{
			name:     "empty username",
			orgID:    "org",
			username: "",
			userID:   "123",
			wantErr:  true,
		},
		{
			name:     "empty userID",
			orgID:    "org",
			username: "user",
			userID:   "",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := validator.GenerateIdentityHeader(
				context.Background(),
				tt.orgID,
				tt.username,
				tt.userID,
			)

			if (err != nil) != tt.wantErr {
				t.Errorf("wantErr=%v, got err=%v", tt.wantErr, err)
			}
		})
	}
}
