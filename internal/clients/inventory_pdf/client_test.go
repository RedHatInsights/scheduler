package inventory_pdf

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"insights-scheduler/internal/identity"
)

func TestClient_CreatePdf(t *testing.T) {
	// Mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/v2/create" {
			t.Errorf("Expected POST /v2/create, got %s %s", r.Method, r.URL.Path)
		}

		// Check x-rh-identity header
		identity := r.Header.Get("x-rh-identity")
		if identity == "" {
			t.Error("Missing x-rh-identity header")
		}

		// Verify request body
		var req map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("Failed to decode request: %v", err)
		}

		// Check payload is an array
		payload, ok := req["payload"].([]interface{})
		if !ok {
			t.Error("Expected payload to be an array")
		}

		if len(payload) == 0 {
			t.Error("Expected at least one payload item")
		}

		// Return mock response
		response := map[string]string{
			"statusID": "test-pdf-123",
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := NewClient(server.URL)

	req := map[string]interface{}{
		"payload": []map[string]interface{}{
			{
				"manifestLocation": "/apps/chrome/federated-modules.json",
				"scope":            "insights",
				"module":           "./RootApp",
			},
		},
	}

	// Generate identity header for the test
	userValidator := identity.NewFakeUserValidator()
	identityHeader, err := userValidator.GenerateIdentityHeader(context.Background(), "org123", "testuser", "test-user-id")
	if err != nil {
		t.Fatalf("GenerateIdentityHeader failed: %v", err)
	}

	statusID, err := client.CreatePdf(context.Background(), req, identityHeader)
	if err != nil {
		t.Fatalf("CreatePdf failed: %v", err)
	}

	if statusID != "test-pdf-123" {
		t.Errorf("Expected statusID 'test-pdf-123', got %s", statusID)
	}
}

func TestClient_GetPdfStatus(t *testing.T) {
	tests := []struct {
		name           string
		statusID       string
		mockStatus     string
		mockFilepath   string
		expectedStatus string
	}{
		{
			name:           "Generating status",
			statusID:       "test-123",
			mockStatus:     "Generating",
			mockFilepath:   "",
			expectedStatus: "Generating",
		},
		{
			name:           "Generated status with filepath",
			statusID:       "test-456",
			mockStatus:     "Generated",
			mockFilepath:   "/tmp/report.pdf",
			expectedStatus: "Generated",
		},
		{
			name:           "Failed status",
			statusID:       "test-789",
			mockStatus:     "Failed",
			mockFilepath:   "",
			expectedStatus: "Failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				expectedPath := "/v2/status/" + tt.statusID
				if r.Method != "GET" || r.URL.Path != expectedPath {
					t.Errorf("Expected GET %s, got %s %s", expectedPath, r.Method, r.URL.Path)
				}

				response := map[string]interface{}{
					"status": map[string]string{
						"status":   tt.mockStatus,
						"filepath": tt.mockFilepath,
					},
				}

				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(response)
			}))
			defer server.Close()

			userValidator := identity.NewFakeUserValidator()
			identityHeader, err := userValidator.GenerateIdentityHeader(context.Background(), "org123", "testuser", "test-user-id")
			if err != nil {
				t.Fatalf("GenerateIdentityHeader failed: %v", err)
			}

			client := NewClient(server.URL)

			status, filepath, err := client.GetPdfStatus(context.Background(), tt.statusID, identityHeader)
			if err != nil {
				t.Fatalf("GetPdfStatus failed: %v", err)
			}

			if status != tt.expectedStatus {
				t.Errorf("Expected status %s, got %s", tt.expectedStatus, status)
			}

			if tt.mockFilepath != "" && filepath != tt.mockFilepath {
				t.Errorf("Expected filepath %s, got %s", tt.mockFilepath, filepath)
			}
		})
	}
}

func TestClient_DownloadPdf(t *testing.T) {
	expectedData := []byte("mock PDF file content")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/v2/download/test-123" {
			t.Errorf("Expected GET /v2/download/test-123, got %s %s", r.Method, r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/pdf")
		w.Write(expectedData)
	}))
	defer server.Close()

	userValidator := identity.NewFakeUserValidator()
	identityHeader, err := userValidator.GenerateIdentityHeader(context.Background(), "org123", "testuser", "test-user-id")
	if err != nil {
		t.Fatalf("GenerateIdentityHeader failed: %v", err)
	}

	client := NewClient(server.URL)

	data, err := client.DownloadPdf(context.Background(), "test-123", identityHeader)
	if err != nil {
		t.Fatalf("DownloadPdf failed: %v", err)
	}

	if string(data) != string(expectedData) {
		t.Errorf("Expected data %s, got %s", expectedData, data)
	}
}

func TestClient_ErrorHandling(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error":   "invalid_request",
			"message": "Payload is required",
		})
	}))
	defer server.Close()

	client := NewClient(server.URL)

	req := map[string]interface{}{
		"payload": []map[string]interface{}{},
	}

	userValidator := identity.NewFakeUserValidator()
	identityHeader, err := userValidator.GenerateIdentityHeader(context.Background(), "org123", "testuser", "test-user-id")
	if err != nil {
		t.Fatalf("GenerateIdentityHeader failed: %v", err)
	}

	_, err = client.CreatePdf(context.Background(), req, identityHeader)
	if err == nil {
		t.Fatal("Expected error for bad request")
	}

	expectedError := "failed to create Inventory PDF: API error (status 400): invalid_request - Payload is required"
	if err.Error() != expectedError {
		t.Errorf("Expected error %s, got %s", expectedError, err.Error())
	}
}

func TestClient_GetPdfDownloadURL(t *testing.T) {
	client := NewClient("https://example.com/api/crc-pdf-generator")

	url := client.GetPdfDownloadURL("test-pdf-123")
	expected := "https://example.com/api/crc-pdf-generator/v2/download/test-pdf-123"

	if url != expected {
		t.Errorf("Expected URL %s, got %s", expected, url)
	}
}

func TestWaitForPdfCompletion_Success(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++

		status := "Generating"
		if attempts >= 3 {
			status = "Generated"
		}

		response := map[string]interface{}{
			"status": map[string]string{
				"status":   status,
				"filepath": "/tmp/report.pdf",
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	userValidator := identity.NewFakeUserValidator()
	identityHeader, err := userValidator.GenerateIdentityHeader(context.Background(), "org123", "testuser", "test-user-id")
	if err != nil {
		t.Fatalf("GenerateIdentityHeader failed: %v", err)
	}

	client := NewClient(server.URL)

	status, filepath, err := WaitForPdfCompletion(client, context.Background(), "test-123", identityHeader, 10, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("WaitForPdfCompletion failed: %v", err)
	}

	if status != "Generated" {
		t.Errorf("Expected status 'Generated', got %s", status)
	}

	if filepath != "/tmp/report.pdf" {
		t.Errorf("Expected filepath '/tmp/report.pdf', got %s", filepath)
	}

	if attempts < 3 {
		t.Errorf("Expected at least 3 polling attempts, got %d", attempts)
	}
}

func TestWaitForPdfCompletion_Failed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := map[string]interface{}{
			"status": map[string]string{
				"status":   "Failed",
				"filepath": "",
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	userValidator := identity.NewFakeUserValidator()
	identityHeader, err := userValidator.GenerateIdentityHeader(context.Background(), "org123", "testuser", "test-user-id")
	if err != nil {
		t.Fatalf("GenerateIdentityHeader failed: %v", err)
	}

	client := NewClient(server.URL)

	_, _, err = WaitForPdfCompletion(client, context.Background(), "test-123", identityHeader, 5, 100*time.Millisecond)
	if err == nil {
		t.Fatal("Expected error for failed PDF generation")
	}

	if err.Error() != "Inventory PDF generation failed" {
		t.Errorf("Expected 'Inventory PDF generation failed', got %s", err.Error())
	}
}

func TestWaitForPdfCompletion_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Always return Generating status
		response := map[string]interface{}{
			"status": map[string]string{
				"status":   "Generating",
				"filepath": "",
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	userValidator := identity.NewFakeUserValidator()
	identityHeader, err := userValidator.GenerateIdentityHeader(context.Background(), "org123", "testuser", "test-user-id")
	if err != nil {
		t.Fatalf("GenerateIdentityHeader failed: %v", err)
	}

	client := NewClient(server.URL)

	_, _, err = WaitForPdfCompletion(client, context.Background(), "test-123", identityHeader, 3, 50*time.Millisecond)
	if err == nil {
		t.Fatal("Expected timeout error")
	}

	expectedError := "Inventory PDF generation did not complete after 3 polling attempts"
	if err.Error() != expectedError {
		t.Errorf("Expected error %s, got %s", expectedError, err.Error())
	}
}
