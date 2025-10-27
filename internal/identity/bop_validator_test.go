package identity

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBopUserValidator_GenerateIdentityHeader(t *testing.T) {
	tests := []struct {
		name           string
		orgID          string
		username       string
		mockResponse   UserValidationResponse
		mockStatusCode int
		wantErr        bool
	}{
		{
			name:     "successful validation",
			orgID:    "123456",
			username: "testuser",
			mockResponse: UserValidationResponse{
				Users: []UserInfo{
					{
						ID:            1,
						Username:      "testuser",
						AccountNumber: "000001",
						OrgID:         "123456",
						IsActive:      true,
					},
				},
			},
			mockStatusCode: http.StatusOK,
			wantErr:        false,
		},
		{
			name:     "inactive user",
			orgID:    "123456",
			username: "inactiveuser",
			mockResponse: UserValidationResponse{
				Users: []UserInfo{
					{
						ID:            2,
						Username:      "inactiveuser",
						AccountNumber: "000002",
						OrgID:         "123456",
						IsActive:      false,
					},
				},
			},
			mockStatusCode: http.StatusOK,
			wantErr:        true,
		},
		{
			name:     "no users returned",
			orgID:    "123456",
			username: "nouser",
			mockResponse: UserValidationResponse{
				Users: []UserInfo{},
			},
			mockStatusCode: http.StatusOK,
			wantErr:        true,
		},
		{
			name:     "multiple users returned",
			orgID:    "123456",
			username: "duplicateuser",
			mockResponse: UserValidationResponse{
				Users: []UserInfo{
					{
						ID:            1,
						Username:      "duplicateuser",
						AccountNumber: "000001",
						OrgID:         "123456",
						IsActive:      true,
					},
					{
						ID:            2,
						Username:      "duplicateuser",
						AccountNumber: "000002",
						OrgID:         "123456",
						IsActive:      true,
					},
				},
			},
			mockStatusCode: http.StatusOK,
			wantErr:        true,
		},
		{
			name:           "service returns error status",
			orgID:          "123456",
			username:       "testuser",
			mockStatusCode: http.StatusInternalServerError,
			wantErr:        true,
		},
		{
			name:     "empty orgID",
			orgID:    "",
			username: "testuser",
			wantErr:  true,
		},
		{
			name:     "empty username",
			orgID:    "123456",
			username: "",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Skip server setup for validation errors
			if tt.orgID == "" || tt.username == "" {
				validator := NewBopUserValidator("http://test.example.com", "test-token", "test-client", "test-env")
				_, err := validator.GenerateIdentityHeader(tt.orgID, tt.username)
				if (err != nil) != tt.wantErr {
					t.Errorf("GenerateIdentityHeader() error = %v, wantErr %v", err, tt.wantErr)
				}
				return
			}

			// Create mock server
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Verify request method
				if r.Method != http.MethodPost {
					t.Errorf("Expected POST request, got %s", r.Method)
				}

				// Verify URL path
				if r.URL.Path != "/v1/users" {
					t.Errorf("Expected path /v1/users, got %s", r.URL.Path)
				}

				// Verify headers
				if apiToken := r.Header.Get("x-rh-apitoken"); apiToken != "test-token" {
					t.Errorf("Expected x-rh-apitoken header 'test-token', got '%s'", apiToken)
				}
				if clientID := r.Header.Get("x-rh-clientid"); clientID != "test-client" {
					t.Errorf("Expected x-rh-clientid header 'test-client', got '%s'", clientID)
				}
				if insightsEnv := r.Header.Get("x-rh-insights-env"); insightsEnv != "test-env" {
					t.Errorf("Expected x-rh-insights-env header 'test-env', got '%s'", insightsEnv)
				}

				// Verify request body contains username
				bodyBytes, err := io.ReadAll(r.Body)
				if err != nil {
					t.Errorf("Failed to read request body: %v", err)
				}

				// Parse request body
				var reqBody map[string]interface{}
				if err := json.Unmarshal(bodyBytes, &reqBody); err != nil {
					t.Errorf("Failed to parse request body: %v", err)
				}

				// Verify users array
				users, ok := reqBody["users"].([]interface{})
				if !ok || len(users) != 1 {
					t.Errorf("Expected users array with 1 element, got %v", reqBody["users"])
				} else if users[0] != tt.username {
					t.Errorf("Expected username %s in request, got %v", tt.username, users[0])
				}

				// Send response
				w.WriteHeader(tt.mockStatusCode)
				if tt.mockStatusCode == http.StatusOK {
					json.NewEncoder(w).Encode(tt.mockResponse)
				}
			}))
			defer server.Close()

			// Create validator with mock server URL
			validator := NewBopUserValidator(server.URL, "test-token", "test-client", "test-env")

			// Call the method
			_, err := validator.GenerateIdentityHeader(tt.orgID, tt.username)

			// Check error
			if (err != nil) != tt.wantErr {
				t.Errorf("GenerateIdentityHeader() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
		})
	}
}

func TestNewBopUserValidator(t *testing.T) {
	baseURL := "http://test.example.com"
	apiToken := "test-token"
	clientID := "test-client"
	insightsEnv := "test-env"
	validator := NewBopUserValidator(baseURL, apiToken, clientID, insightsEnv)

	if validator == nil {
		t.Fatal("NewBopUserValidator() returned nil")
	}

	if validator.baseURL != baseURL {
		t.Errorf("baseURL = %v, want %v", validator.baseURL, baseURL)
	}

	if validator.apiToken != apiToken {
		t.Errorf("apiToken = %v, want %v", validator.apiToken, apiToken)
	}

	if validator.clientID != clientID {
		t.Errorf("clientID = %v, want %v", validator.clientID, clientID)
	}

	if validator.insightsEnv != insightsEnv {
		t.Errorf("insightsEnv = %v, want %v", validator.insightsEnv, insightsEnv)
	}

	if validator.httpClient == nil {
		t.Error("httpClient is nil")
	}
}

func TestNewBopUserValidatorWithClient(t *testing.T) {
	baseURL := "http://test.example.com"
	apiToken := "test-token"
	clientID := "test-client"
	insightsEnv := "test-env"
	customClient := &http.Client{}
	validator := NewBopUserValidatorWithClient(baseURL, apiToken, clientID, insightsEnv, customClient)

	if validator == nil {
		t.Fatal("NewBopUserValidatorWithClient() returned nil")
	}

	if validator.baseURL != baseURL {
		t.Errorf("baseURL = %v, want %v", validator.baseURL, baseURL)
	}

	if validator.apiToken != apiToken {
		t.Errorf("apiToken = %v, want %v", validator.apiToken, apiToken)
	}

	if validator.clientID != clientID {
		t.Errorf("clientID = %v, want %v", validator.clientID, clientID)
	}

	if validator.insightsEnv != insightsEnv {
		t.Errorf("insightsEnv = %v, want %v", validator.insightsEnv, insightsEnv)
	}

	if validator.httpClient != customClient {
		t.Error("httpClient is not the custom client")
	}
}

func TestBopUserValidator_Interface(t *testing.T) {
	// Verify that BopUserValidator implements UserValidator interface
	var _ UserValidator = (*BopUserValidator)(nil)
}
