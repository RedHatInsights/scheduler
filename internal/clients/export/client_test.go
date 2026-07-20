package export

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	platformIdentity "github.com/redhatinsights/platform-go-middlewares/v2/identity"

	"insights-scheduler/internal/identity"
)

func TestClient_CreateExport(t *testing.T) {
	// Mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/exports" {
			t.Errorf("Expected POST /exports, got %s %s", r.Method, r.URL.Path)
		}

		// Check x-rh-identity header
		identity := r.Header.Get("x-rh-identity")
		if identity == "" {
			t.Error("Missing x-rh-identity header")
		}

		// Verify request body
		var req ExportRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("Failed to decode request: %v", err)
		}

		if req.Name != "Test Export" {
			t.Errorf("Expected name 'Test Export', got %s", req.Name)
		}

		// Return mock response
		response := ExportStatusResponse{
			ID:        "test-export-123",
			Name:      req.Name,
			CreatedAt: time.Now(),
			Format:    req.Format,
			Status:    StatusPending,
			Sources:   []SourceStatus{},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := NewClient(server.URL, server.URL)

	req := ExportRequest{
		Name:   "Test Export",
		Format: FormatJSON,
		Sources: []Source{
			{
				Application: AppAdvisor,
				Resource:    "recommendations",
			},
		},
	}

	// Generate identity header for the test using UserValidator
	userValidator := identity.NewFakeUserValidator()
	identityHeader, err := userValidator.GenerateIdentityHeader(context.Background(), "org123", "test-user-id")
	if err != nil {
		t.Fatalf("GenerateIdentityHeader failed: %v", err)
	}

	result, err := client.CreateExport(context.Background(), req, identityHeader)
	if err != nil {
		t.Fatalf("CreateExport failed: %v", err)
	}

	if result.ID != "test-export-123" {
		t.Errorf("Expected ID 'test-export-123', got %s", result.ID)
	}

	if result.Status != StatusPending {
		t.Errorf("Expected status %s, got %s", StatusPending, result.Status)
	}
}

func TestClient_ListExports(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/exports" {
			t.Errorf("Expected GET /exports, got %s %s", r.Method, r.URL.Path)
		}

		// Check query parameters
		if r.URL.Query().Get("application") != "advisor" {
			t.Error("Expected application=advisor query parameter")
		}

		response := ExportListResponse{
			Data: []ExportStatusResponse{
				{
					ID:     "export-1",
					Name:   "Export 1",
					Status: StatusComplete,
					Format: FormatJSON,
				},
				{
					ID:     "export-2",
					Name:   "Export 2",
					Status: StatusPending,
					Format: FormatCSV,
				},
			},
			Meta: Metadata{
				Count:  2,
				Limit:  10,
				Offset: 0,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// Generate identity header for the test using UserValidator
	userValidator := identity.NewFakeUserValidator()
	identityHeader, err := userValidator.GenerateIdentityHeader(context.Background(), "org123", "test-user-id")
	if err != nil {
		t.Fatalf("GenerateIdentityHeader failed: %v", err)
	}

	client := NewClient(server.URL, server.URL)

	app := AppAdvisor
	params := &ListParams{
		Application: &app,
	}

	result, err := client.ListExports(context.Background(), params, identityHeader)
	if err != nil {
		t.Fatalf("ListExports failed: %v", err)
	}

	if len(result.Data) != 2 {
		t.Errorf("Expected 2 exports, got %d", len(result.Data))
	}

	if result.Meta.Count != 2 {
		t.Errorf("Expected count 2, got %d", result.Meta.Count)
	}
}

func TestClient_GetExportStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/exports/test-123/status" {
			t.Errorf("Expected GET /exports/test-123/status, got %s %s", r.Method, r.URL.Path)
		}

		response := ExportStatusResponse{
			ID:        "test-123",
			Name:      "Test Export",
			Status:    StatusComplete,
			Format:    FormatJSON,
			CreatedAt: time.Now(),
			CompletedAt: func() *time.Time {
				t := time.Now()
				return &t
			}(),
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// Generate identity header for the test using UserValidator
	userValidator := identity.NewFakeUserValidator()
	identityHeader, err := userValidator.GenerateIdentityHeader(context.Background(), "org123", "test-user-id")
	if err != nil {
		t.Fatalf("GenerateIdentityHeader failed: %v", err)
	}

	client := NewClient(server.URL, server.URL)

	result, err := client.GetExportStatus(context.Background(), "test-123", identityHeader)
	if err != nil {
		t.Fatalf("GetExportStatus failed: %v", err)
	}

	if result.ID != "test-123" {
		t.Errorf("Expected ID 'test-123', got %s", result.ID)
	}

	if result.Status != StatusComplete {
		t.Errorf("Expected status %s, got %s", StatusComplete, result.Status)
	}

	if result.CompletedAt == nil {
		t.Error("Expected CompletedAt to be set")
	}
}

func TestClient_DownloadExport(t *testing.T) {
	expectedData := []byte("mock zip file content")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/exports/test-123" {
			t.Errorf("Expected GET /exports/test-123, got %s %s", r.Method, r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/zip")
		w.Write(expectedData)
	}))
	defer server.Close()

	// Generate identity header for the test using UserValidator
	userValidator := identity.NewFakeUserValidator()
	identityHeader, err := userValidator.GenerateIdentityHeader(context.Background(), "org123", "test-user-id")
	if err != nil {
		t.Fatalf("GenerateIdentityHeader failed: %v", err)
	}

	client := NewClient(server.URL, server.URL)

	data, err := client.DownloadExport(context.Background(), "test-123", identityHeader)
	if err != nil {
		t.Fatalf("DownloadExport failed: %v", err)
	}

	if string(data) != string(expectedData) {
		t.Errorf("Expected data %s, got %s", expectedData, data)
	}
}

func TestClient_DeleteExport(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "DELETE" || r.URL.Path != "/exports/test-123" {
			t.Errorf("Expected DELETE /exports/test-123, got %s %s", r.Method, r.URL.Path)
		}

		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	// Generate identity header for the test using UserValidator
	userValidator := identity.NewFakeUserValidator()
	identityHeader, err := userValidator.GenerateIdentityHeader(context.Background(), "org123", "test-user-id")
	if err != nil {
		t.Fatalf("GenerateIdentityHeader failed: %v", err)
	}

	client := NewClient(server.URL, server.URL)

	err = client.DeleteExport(context.Background(), "test-123", identityHeader)
	if err != nil {
		t.Fatalf("DeleteExport failed: %v", err)
	}
}

func TestClient_ErrorHandling(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{
			Error:   "invalid_request",
			Message: "Export name is required",
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, server.URL)

	req := ExportRequest{
		Format: FormatJSON,
		Sources: []Source{
			{
				Application: AppAdvisor,
				Resource:    "recommendations",
			},
		},
	}

	// Generate identity header for the test using UserValidator
	userValidator := identity.NewFakeUserValidator()
	identityHeader, err := userValidator.GenerateIdentityHeader(context.Background(), "org123", "test-user-id")
	if err != nil {
		t.Fatalf("GenerateIdentityHeader failed: %v", err)
	}

	_, err = client.CreateExport(context.Background(), req, identityHeader)
	if err == nil {
		t.Fatal("Expected error for bad request")
	}

	expectedError := "failed to create export: API error (status 400): invalid_request - Export name is required"
	if err.Error() != expectedError {
		t.Errorf("Expected error %s, got %s", expectedError, err.Error())
	}
}

func TestUserValidator_GenerateIdentityHeader(t *testing.T) {
	userValidator := identity.NewFakeUserValidator()

	identityHeader, err := userValidator.GenerateIdentityHeader(context.Background(), "test-org", "test-user-id")
	if err != nil {
		t.Fatalf("GenerateIdentityHeader failed: %v", err)
	}

	if identityHeader == "" {
		t.Error("Expected non-empty identity header")
	}

	// Verify the header can be decoded
	decoded, err := base64.StdEncoding.DecodeString(identityHeader)
	if err != nil {
		t.Fatalf("Failed to decode identity header: %v", err)
	}

	var identityStruct platformIdentity.XRHID
	if err := json.Unmarshal(decoded, &identityStruct); err != nil {
		t.Fatalf("Failed to unmarshal identity: %v", err)
	}

	if identityStruct.Identity.OrgID != "test-org" {
		t.Errorf("Expected OrgID 'test-org', got %s", identityStruct.Identity.OrgID)
	}

	// Username is now derived from userID (user-{userID})
	expectedUsername := "user-test-user-id"
	if identityStruct.Identity.User.Username != expectedUsername {
		t.Errorf("Expected Username '%s', got %s", expectedUsername, identityStruct.Identity.User.Username)
	}

	if identityStruct.Identity.Type != "User" {
		t.Errorf("Expected Type 'User', got %s", identityStruct.Identity.Type)
	}
}

func TestClient_WaitForExportCompletion_FailedWithSourceErrors(t *testing.T) {
	callCount := 0
	sourceErr := "advisor processing error: timeout contacting host inventory"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		response := ExportStatusResponse{
			ID:     "export-abc-123",
			Status: StatusFailed,
			Sources: []SourceStatus{
				{
					Application: AppAdvisor,
					Resource:    "recommendations",
					Status:      "failed",
					Error:       &sourceErr,
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := NewClient(server.URL, server.URL)

	userValidator := identity.NewFakeUserValidator()
	identityHeader, err := userValidator.GenerateIdentityHeader(context.Background(), "org123", "test-user-id")
	if err != nil {
		t.Fatalf("GenerateIdentityHeader failed: %v", err)
	}

	status, err := client.WaitForExportCompletion(context.Background(), "export-abc-123", identityHeader, 3, 1*time.Millisecond)
	if err == nil {
		t.Fatal("Expected error for failed export")
	}

	if status == nil {
		t.Fatal("Expected non-nil status for failed export")
	}

	errMsg := err.Error()
	if !contains(errMsg, "export-abc-123") {
		t.Errorf("Error should contain export ID, got: %s", errMsg)
	}
	if !contains(errMsg, "advisor/recommendations") {
		t.Errorf("Error should contain source details, got: %s", errMsg)
	}
	if !contains(errMsg, "timeout contacting host inventory") {
		t.Errorf("Error should contain source error message, got: %s", errMsg)
	}
	if callCount != 1 {
		t.Errorf("Expected 1 poll attempt for immediate failure, got %d", callCount)
	}
}

func TestClient_WaitForExportCompletion_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := ExportStatusResponse{
			ID:     "export-timeout-456",
			Status: StatusRunning,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := NewClient(server.URL, server.URL)

	userValidator := identity.NewFakeUserValidator()
	identityHeader, err := userValidator.GenerateIdentityHeader(context.Background(), "org123", "test-user-id")
	if err != nil {
		t.Fatalf("GenerateIdentityHeader failed: %v", err)
	}

	_, err = client.WaitForExportCompletion(context.Background(), "export-timeout-456", identityHeader, 3, 1*time.Millisecond)
	if err == nil {
		t.Fatal("Expected error for timed-out export")
	}

	errMsg := err.Error()
	if !contains(errMsg, "export-timeout-456") {
		t.Errorf("Error should contain export ID, got: %s", errMsg)
	}
	if !contains(errMsg, "3 polling attempts") {
		t.Errorf("Error should mention polling attempts, got: %s", errMsg)
	}
}

func TestClient_WaitForExportCompletion_PollError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{
			Error:   "internal_error",
			Message: "database connection lost",
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, server.URL)

	userValidator := identity.NewFakeUserValidator()
	identityHeader, err := userValidator.GenerateIdentityHeader(context.Background(), "org123", "test-user-id")
	if err != nil {
		t.Fatalf("GenerateIdentityHeader failed: %v", err)
	}

	_, err = client.WaitForExportCompletion(context.Background(), "export-poll-789", identityHeader, 3, 1*time.Millisecond)
	if err == nil {
		t.Fatal("Expected error for polling failure")
	}

	errMsg := err.Error()
	if !contains(errMsg, "export-poll-789") {
		t.Errorf("Error should contain export ID, got: %s", errMsg)
	}
	if !contains(errMsg, "500") {
		t.Errorf("Error should contain HTTP status code, got: %s", errMsg)
	}
}

func TestClient_WaitForExportCompletion_UnknownStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := ExportStatusResponse{
			ID:     "export-unknown-101",
			Status: "bogus_status",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := NewClient(server.URL, server.URL)

	userValidator := identity.NewFakeUserValidator()
	identityHeader, err := userValidator.GenerateIdentityHeader(context.Background(), "org123", "test-user-id")
	if err != nil {
		t.Fatalf("GenerateIdentityHeader failed: %v", err)
	}

	_, err = client.WaitForExportCompletion(context.Background(), "export-unknown-101", identityHeader, 3, 1*time.Millisecond)
	if err == nil {
		t.Fatal("Expected error for unknown status")
	}

	errMsg := err.Error()
	if !contains(errMsg, "export-unknown-101") {
		t.Errorf("Error should contain export ID, got: %s", errMsg)
	}
	if !contains(errMsg, "bogus_status") {
		t.Errorf("Error should contain the unknown status value, got: %s", errMsg)
	}
}

func TestExtractSourceErrors(t *testing.T) {
	err1 := "advisor timed out"
	err2 := "compliance data unavailable"

	tests := []struct {
		name     string
		sources  []SourceStatus
		contains []string
	}{
		{
			name:     "no sources",
			sources:  nil,
			contains: []string{"no source error details"},
		},
		{
			name: "single source error",
			sources: []SourceStatus{
				{Application: AppAdvisor, Resource: "recommendations", Error: &err1},
			},
			contains: []string{"advisor/recommendations", "advisor timed out"},
		},
		{
			name: "multiple source errors",
			sources: []SourceStatus{
				{Application: AppAdvisor, Resource: "recommendations", Error: &err1},
				{Application: AppCompliance, Resource: "policies", Error: &err2},
			},
			contains: []string{"advisor/recommendations", "compliance/policies"},
		},
		{
			name: "source with nil error skipped",
			sources: []SourceStatus{
				{Application: AppAdvisor, Resource: "recommendations", Error: nil},
				{Application: AppCompliance, Resource: "policies", Error: &err2},
			},
			contains: []string{"compliance/policies"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractSourceErrors(tt.sources)
			for _, s := range tt.contains {
				if !contains(result, s) {
					t.Errorf("Expected result to contain %q, got: %s", s, result)
				}
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestClient_GetExportDownloadURL(t *testing.T) {
	// Test that public URL is used for download URLs
	internalURL := "http://export-service:8000/api/v1"
	publicURL := "https://console.redhat.com/api/export/v1"
	client := NewClient(internalURL, publicURL)

	url := client.GetExportDownloadURL("test-export-123")
	expected := "https://console.redhat.com/api/export/v1/exports/test-export-123"

	if url != expected {
		t.Errorf("Expected URL %s, got %s", expected, url)
	}
}
