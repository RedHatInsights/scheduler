package identity

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// buildXRHIDHeader builds a base64-encoded x-rh-identity envelope for tests.
func buildXRHIDHeader(orgID, userID string) string {
	identity := map[string]interface{}{
		"identity": map[string]interface{}{
			"account_number": "account-789",
			"org_id":         orgID,
			"type":           "User",
			"auth_type":      "jwt-auth",
			"internal": map[string]interface{}{
				"org_id": orgID,
			},
			"user": map[string]interface{}{
				"username":  "testuser",
				"user_id":   userID,
				"email":     "testuser@example.com",
				"is_active": true,
			},
		},
	}
	identityJSON, _ := json.Marshal(identity)
	return base64.StdEncoding.EncodeToString(identityJSON)
}

func TestParsecUserValidator_GenerateIdentityHeader(t *testing.T) {
	var (
		methodReceived      string
		pathReceived        string
		contentTypeReceived string
		requestIDReceived   string
		subjectReceived     string
	)

	expectedHeader := buildXRHIDHeader("org-123", "user-456")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methodReceived = r.Method
		pathReceived = r.URL.Path
		contentTypeReceived = r.Header.Get("Content-Type")
		requestIDReceived = r.Header.Get("x-rh-insights-request-id")

		// Decode the request body and the nested subject_token JSON string.
		body, _ := io.ReadAll(r.Body)
		var req parsecExchangeRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("failed to unmarshal request body: %v", err)
		}
		if req.GrantType != parsecGrantType {
			t.Errorf("expected grant_type %s, got %s", parsecGrantType, req.GrantType)
		}
		if req.SubjectTokenType != parsecSubjectTokenType {
			t.Errorf("expected subject_token_type %s, got %s", parsecSubjectTokenType, req.SubjectTokenType)
		}
		if req.RequestedTokenType != parsecRequestedTokenType {
			t.Errorf("expected requested_token_type %s, got %s", parsecRequestedTokenType, req.RequestedTokenType)
		}
		var subject map[string]string
		if err := json.Unmarshal([]byte(req.SubjectToken), &subject); err != nil {
			t.Errorf("subject_token is not a JSON string containing an object: %v", err)
		}
		subjectReceived = subject["sub"]

		resp := parsecExchangeResponse{AccessToken: expectedHeader}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	validator := NewParsecUserValidator(server.URL, 5*time.Second)

	header, err := validator.GenerateIdentityHeader(context.Background(), "org-123", "user-456")
	if err != nil {
		t.Fatalf("GenerateIdentityHeader failed: %v", err)
	}

	if header != expectedHeader {
		t.Errorf("expected header to be returned as-is; got %q want %q", header, expectedHeader)
	}
	if methodReceived != "POST" {
		t.Errorf("expected POST, got %s", methodReceived)
	}
	if pathReceived != parsecTokenPath {
		t.Errorf("expected path %s, got %s", parsecTokenPath, pathReceived)
	}
	if contentTypeReceived != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", contentTypeReceived)
	}
	if requestIDReceived == "" {
		t.Error("expected x-rh-insights-request-id header to be sent")
	}
	if want := parsecSubjectPrefix + "user-456"; subjectReceived != want {
		t.Errorf("expected sub %q, got %q", want, subjectReceived)
	}
}

func TestParsecUserValidator_EmptyAccessToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer server.Close()

	validator := NewParsecUserValidator(server.URL, 5*time.Second)

	_, err := validator.GenerateIdentityHeader(context.Background(), "org-123", "user-456")
	if err == nil {
		t.Error("expected error for empty access token, got nil")
	}
}

func TestParsecUserValidator_BadBase64(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"accessToken":"!!!not-base64!!!"}`))
	}))
	defer server.Close()

	validator := NewParsecUserValidator(server.URL, 5*time.Second)

	_, err := validator.GenerateIdentityHeader(context.Background(), "org-123", "user-456")
	if err == nil {
		t.Error("expected error for invalid base64, got nil")
	}
}

func TestParsecUserValidator_OrgIDMismatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := parsecExchangeResponse{AccessToken: buildXRHIDHeader("org-999", "user-456")}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	validator := NewParsecUserValidator(server.URL, 5*time.Second)

	_, err := validator.GenerateIdentityHeader(context.Background(), "org-123", "user-456")
	if err == nil {
		t.Error("expected error for org_id mismatch, got nil")
	}
}

func TestParsecUserValidator_UserIDMismatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := parsecExchangeResponse{AccessToken: buildXRHIDHeader("org-123", "user-999")}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	validator := NewParsecUserValidator(server.URL, 5*time.Second)

	_, err := validator.GenerateIdentityHeader(context.Background(), "org-123", "user-456")
	if err == nil {
		t.Error("expected error for user_id mismatch, got nil")
	}
}

func TestParsecUserValidator_WrongType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A non-User principal (e.g. ServiceAccount) that still carries a matching user block
		identity := map[string]interface{}{
			"identity": map[string]interface{}{
				"account_number": "account-789",
				"org_id":         "org-123",
				"type":           "ServiceAccount",
				"auth_type":      "jwt-auth",
				"internal":       map[string]interface{}{"org_id": "org-123"},
				"user": map[string]interface{}{
					"username":  "testuser",
					"user_id":   "user-456",
					"email":     "testuser@example.com",
					"is_active": true,
				},
			},
		}
		identityJSON, _ := json.Marshal(identity)
		resp := parsecExchangeResponse{AccessToken: base64.StdEncoding.EncodeToString(identityJSON)}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	validator := NewParsecUserValidator(server.URL, 5*time.Second)

	_, err := validator.GenerateIdentityHeader(context.Background(), "org-123", "user-456")
	if err == nil {
		t.Error("expected error for non-User identity type, got nil")
	}
}

func TestParsecUserValidator_NilUser(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity := map[string]interface{}{
			"identity": map[string]interface{}{
				"account_number": "account-789",
				"org_id":         "org-123",
				"type":           "User",
				"auth_type":      "jwt-auth",
				"internal":       map[string]interface{}{"org_id": "org-123"},
				// No user field
			},
		}
		identityJSON, _ := json.Marshal(identity)
		resp := parsecExchangeResponse{AccessToken: base64.StdEncoding.EncodeToString(identityJSON)}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	validator := NewParsecUserValidator(server.URL, 5*time.Second)

	_, err := validator.GenerateIdentityHeader(context.Background(), "org-123", "user-456")
	if err == nil {
		t.Error("expected error for nil user, got nil")
	}
}

func TestParsecUserValidator_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("invalid_request"))
	}))
	defer server.Close()

	validator := NewParsecUserValidator(server.URL, 5*time.Second)

	_, err := validator.GenerateIdentityHeader(context.Background(), "org-123", "user-456")
	if err == nil {
		t.Error("expected error for HTTP 400, got nil")
	}
	if !strings.Contains(err.Error(), "user validation service returned an error") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestParsecUserValidator_EmptyParams(t *testing.T) {
	validator := NewParsecUserValidator("http://localhost:8080", 5*time.Second)

	tests := []struct {
		name    string
		orgID   string
		userID  string
		wantErr bool
	}{
		{name: "empty orgID", orgID: "", userID: "123", wantErr: true},
		{name: "empty userID", orgID: "org", userID: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := validator.GenerateIdentityHeader(context.Background(), tt.orgID, tt.userID)
			if (err != nil) != tt.wantErr {
				t.Errorf("wantErr=%v, got err=%v", tt.wantErr, err)
			}
		})
	}
}

func TestNewParsecUserValidator(t *testing.T) {
	v := NewParsecUserValidator("http://parsec:8080", 3*time.Second)
	if v.baseURL != "http://parsec:8080" {
		t.Errorf("unexpected baseURL: %s", v.baseURL)
	}
	if v.httpClient == nil {
		t.Error("expected non-nil httpClient")
	}
	if v.httpClient.Timeout != 3*time.Second {
		t.Errorf("unexpected timeout: %v", v.httpClient.Timeout)
	}
}

func TestNewParsecUserValidatorWithClient(t *testing.T) {
	client := &http.Client{Timeout: 7 * time.Second}
	v := NewParsecUserValidatorWithClient("http://parsec:8080", client)
	if v.baseURL != "http://parsec:8080" {
		t.Errorf("unexpected baseURL: %s", v.baseURL)
	}
	if v.httpClient != client {
		t.Error("expected the provided httpClient to be used")
	}
}

func TestParsecUserValidator_Interface(t *testing.T) {
	var _ UserValidator = (*ParsecUserValidator)(nil)
}
