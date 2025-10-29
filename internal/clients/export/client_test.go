package export

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"insights-scheduler-part-2/internal/identity"
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

	client := NewClient(server.URL, "123456", "org123")

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
	userValidator := identity.NewDefaultUserValidator("123456")
	identityHeader, err := userValidator.GenerateIdentityHeader("org123", "testuser", "test-user-id")
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

	client := NewClient(server.URL, "123456", "org123")

	app := AppAdvisor
	params := &ListParams{
		Application: &app,
	}

	result, err := client.ListExports(context.Background(), params)
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

	client := NewClient(server.URL, "123456", "org123")

	result, err := client.GetExportStatus(context.Background(), "test-123")
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

	client := NewClient(server.URL, "123456", "org123")

	data, err := client.DownloadExport(context.Background(), "test-123")
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

	client := NewClient(server.URL, "123456", "org123")

	err := client.DeleteExport(context.Background(), "test-123")
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

	client := NewClient(server.URL, "123456", "org123")

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
	userValidator := identity.NewDefaultUserValidator("123456")
	identityHeader, err := userValidator.GenerateIdentityHeader("org123", "testuser", "test-user-id")
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
	userValidator := identity.NewDefaultUserValidator("123456")

	identityHeader, err := userValidator.GenerateIdentityHeader("test-org", "testuser", "test-user-id")
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

	var identityStruct identity.IdentityHeader
	if err := json.Unmarshal(decoded, &identityStruct); err != nil {
		t.Fatalf("Failed to unmarshal identity: %v", err)
	}

	if identityStruct.Identity.OrgID != "test-org" {
		t.Errorf("Expected OrgID 'test-org', got %s", identityStruct.Identity.OrgID)
	}

	if identityStruct.Identity.User.Username != "testuser" {
		t.Errorf("Expected Username 'testuser', got %s", identityStruct.Identity.User.Username)
	}

	if identityStruct.Identity.Type != "User" {
		t.Errorf("Expected Type 'User', got %s", identityStruct.Identity.Type)
	}
}

func TestClient_GetExportDownloadURL(t *testing.T) {
	client := NewClient("https://example.com/api/v1", "123456", "org123")

	url := client.GetExportDownloadURL("test-export-123")
	expected := "https://example.com/api/v1/exports/test-export-123"

	if url != expected {
		t.Errorf("Expected URL %s, got %s", expected, url)
	}
}
