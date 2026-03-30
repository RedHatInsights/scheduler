package identity

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestThreeScaleUserValidator_GenerateIdentityHeader(t *testing.T) {
	// Create a test server
	requestIDReceived := ""
	userIDHeaderReceived := ""

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Capture headers
		requestIDReceived = r.Header.Get("x-rh-insights-request-id")
		userIDHeaderReceived = r.Header.Get("X-Rh-User-Id")

		// Verify it's a GET request
		if r.Method != "GET" {
			t.Errorf("Expected GET request, got %s", r.Method)
		}

		// Verify URL path
		expectedPath := "/internal/userIdentity"
		if r.URL.Path != expectedPath {
			t.Errorf("Expected path %s, got %s", expectedPath, r.URL.Path)
		}

		// Build identity header
		identity := map[string]interface{}{
			"identity": map[string]interface{}{
				"account_number": "account-789",
				"org_id":         "org-123",
				"type":           "User",
				"auth_type":      "jwt-auth",
				"internal": map[string]interface{}{
					"org_id": "org-123",
				},
				"user": map[string]interface{}{
					"username":  "testuser",
					"user_id":   "user-456",
					"email":     "testuser@example.com",
					"is_active": true,
				},
			},
		}

		identityJSON, _ := json.Marshal(identity)
		identityHeader := base64.StdEncoding.EncodeToString(identityJSON)

		// Return response with x-rh-identity header
		response := ThreeScaleResponse{
			XRHIdentity: identityHeader,
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// Create validator with test server URL
	validator := NewThreeScaleUserValidator(server.URL, 5*time.Second)

	// Test GenerateIdentityHeader
	header, err := validator.GenerateIdentityHeader(
		context.Background(),
		"org-123",
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
		t.Error("Expected x-rh-insights-request-id header to be sent")
	}
	t.Logf("Request ID sent: %s", requestIDReceived)

	// Verify user-id header
	expectedUserID := "user-456"
	if userIDHeaderReceived != expectedUserID {
		t.Errorf("Expected X-Rh-User-Id header '%s', got '%s'", expectedUserID, userIDHeaderReceived)
	}
}

func TestThreeScaleUserValidator_InactiveUser(t *testing.T) {

	t.Skip("skipping inactive test for now.")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Build identity header with inactive user
		identity := map[string]interface{}{
			"identity": map[string]interface{}{
				"account_number": "account-789",
				"org_id":         "org-123",
				"type":           "User",
				"auth_type":      "jwt-auth",
				"internal": map[string]interface{}{
					"org_id": "org-123",
				},
				"user": map[string]interface{}{
					"username":  "testuser",
					"user_id":   "user-456",
					"email":     "testuser@example.com",
					"is_active": false, // Inactive user
				},
			},
		}

		identityJSON, _ := json.Marshal(identity)
		identityHeader := base64.StdEncoding.EncodeToString(identityJSON)

		response := ThreeScaleResponse{
			XRHIdentity: identityHeader,
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	validator := NewThreeScaleUserValidator(server.URL, 5*time.Second)

	_, err := validator.GenerateIdentityHeader(
		context.Background(),
		"org-123",
		"user-456",
	)

	if err == nil {
		t.Error("Expected error for inactive user, got nil")
	}

	if !strings.Contains(err.Error(), "not active") {
		t.Errorf("Expected error to contain 'not active', got: %v", err)
	}

	t.Logf("Got expected error: %v", err)
}

func TestThreeScaleUserValidator_OrgIDMismatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Build identity header with different org_id
		identity := map[string]interface{}{
			"identity": map[string]interface{}{
				"account_number": "account-789",
				"org_id":         "org-999", // Different org
				"type":           "User",
				"auth_type":      "jwt-auth",
				"internal": map[string]interface{}{
					"org_id": "org-999",
				},
				"user": map[string]interface{}{
					"username":  "testuser",
					"user_id":   "user-456",
					"email":     "testuser@example.com",
					"is_active": true,
				},
			},
		}

		identityJSON, _ := json.Marshal(identity)
		identityHeader := base64.StdEncoding.EncodeToString(identityJSON)

		response := ThreeScaleResponse{
			XRHIdentity: identityHeader,
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	validator := NewThreeScaleUserValidator(server.URL, 5*time.Second)

	_, err := validator.GenerateIdentityHeader(
		context.Background(),
		"org-123",
		"user-456",
	)

	if err == nil {
		t.Error("Expected error for org_id mismatch, got nil")
	}

	t.Logf("Got expected error: %v", err)
}

func TestThreeScaleUserValidator_NilUser(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Build identity header without user field
		identity := map[string]interface{}{
			"identity": map[string]interface{}{
				"account_number": "account-789",
				"org_id":         "org-123",
				"type":           "User",
				"auth_type":      "jwt-auth",
				"internal": map[string]interface{}{
					"org_id": "org-123",
				},
				// No user field
			},
		}

		identityJSON, _ := json.Marshal(identity)
		identityHeader := base64.StdEncoding.EncodeToString(identityJSON)

		response := ThreeScaleResponse{
			XRHIdentity: identityHeader,
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	validator := NewThreeScaleUserValidator(server.URL, 5*time.Second)

	_, err := validator.GenerateIdentityHeader(
		context.Background(),
		"org-123",
		"user-456",
	)

	if err == nil {
		t.Error("Expected error for nil user, got nil")
	}

	if !strings.Contains(err.Error(), "unable to process response from user validation service") {
		t.Errorf("Expected error to contain 'unable to process response from user validation service', got: %v", err)
	}

	t.Logf("Got expected error: %v", err)
}

func TestThreeScaleUserValidator_UserIDMismatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Build identity header with different user_id
		identity := map[string]interface{}{
			"identity": map[string]interface{}{
				"account_number": "account-789",
				"org_id":         "org-123",
				"type":           "User",
				"auth_type":      "jwt-auth",
				"internal": map[string]interface{}{
					"org_id": "org-123",
				},
				"user": map[string]interface{}{
					"username":  "testuser",
					"user_id":   "user-999", // Different user_id
					"email":     "testuser@example.com",
					"is_active": true,
				},
			},
		}

		identityJSON, _ := json.Marshal(identity)
		identityHeader := base64.StdEncoding.EncodeToString(identityJSON)

		response := ThreeScaleResponse{
			XRHIdentity: identityHeader,
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	validator := NewThreeScaleUserValidator(server.URL, 5*time.Second)

	_, err := validator.GenerateIdentityHeader(
		context.Background(),
		"org-123",
		"user-456",
	)

	if err == nil {
		t.Error("Expected error for user_id mismatch, got nil")
	}

	t.Logf("Got expected error: %v", err)
}

func TestThreeScaleUserValidator_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("User not found"))
	}))
	defer server.Close()

	validator := NewThreeScaleUserValidator(server.URL, 5*time.Second)

	_, err := validator.GenerateIdentityHeader(
		context.Background(),
		"org-123",
		"user-456",
	)

	if err == nil {
		t.Error("Expected error for HTTP 404, got nil")
	}

	t.Logf("Got expected error: %v", err)
}

func TestThreeScaleUserValidator_StructuredErrorResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return structured error response matching the spec
		errorResponse := `{
			"errors": [{
				"meta": {"response_by": "gateway"},
				"status": 400,
				"detail": "unable to retrieve user data"
			}]
		}`
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(errorResponse))
	}))
	defer server.Close()

	validator := NewThreeScaleUserValidator(server.URL, 5*time.Second)

	_, err := validator.GenerateIdentityHeader(
		context.Background(),
		"org-123",
		"user-456",
	)

	if err == nil {
		t.Error("Expected error for structured error response, got nil")
	}

	if !strings.Contains(err.Error(), "user validation service error") {
		t.Errorf("Expected error to contain 'user validation service error', got: %v", err)
	}

	t.Logf("Got expected error: %v", err)
}

func TestThreeScaleUserValidator_EmptyParams(t *testing.T) {
	validator := NewThreeScaleUserValidator("http://localhost:8080", 5*time.Second)

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
				tt.userID,
			)

			if (err != nil) != tt.wantErr {
				t.Errorf("wantErr=%v, got err=%v", tt.wantErr, err)
			}
		})
	}
}
