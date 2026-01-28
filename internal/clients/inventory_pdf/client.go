package inventory_pdf

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"
)

const (
	// RequestIDHeader is the header name for request ID tracking
	RequestIDHeader = "x-insights-request-id"
)

// Client represents the Inventory PDF generator service REST client
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient creates a new Inventory PDF generator service client
func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// SetHTTPClient allows setting a custom HTTP client
func (c *Client) SetHTTPClient(client *http.Client) {
	c.httpClient = client
}

// createRequestWithIdentity creates an HTTP request with a custom identity header
func (c *Client) createRequestWithIdentity(ctx context.Context, method, endpoint string, body interface{}, identityHeader string) (*http.Request, error) {
	var reqBody io.Reader

	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		reqBody = bytes.NewBuffer(jsonBody)
	}

	url := c.baseURL + endpoint
	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Generate a unique request ID for tracing
	requestID := uuid.New().String()

	// Set content type for requests with body
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	// Set the provided Red Hat identity header
	req.Header.Set("x-rh-identity", identityHeader)

	// Set the request ID header for distributed tracing
	req.Header.Set(RequestIDHeader, requestID)

	log.Printf("[DEBUG] Inventory PDF Generator - %s %s - Request-ID: %s", method, endpoint, requestID)

	return req, nil
}

// doRequest executes an HTTP request and handles the response
func (c *Client) doRequest(req *http.Request, result interface{}) error {
	requestID := req.Header.Get(RequestIDHeader)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		log.Printf("[DEBUG] Inventory PDF Generator - Request failed - Request-ID: %s, Error: %v", requestID, err)
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	log.Printf("[DEBUG] Inventory PDF Generator - Response received - Request-ID: %s, Status: %d", requestID, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode >= 400 {
		var errResp map[string]string
		if err := json.Unmarshal(body, &errResp); err != nil {
			return fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
		}
		return fmt.Errorf("API error (status %d): %s - %s", resp.StatusCode, errResp["error"], errResp["message"])
	}

	if result != nil {
		if err := json.Unmarshal(body, result); err != nil {
			return fmt.Errorf("failed to unmarshal response: %w", err)
		}
	}

	return nil
}

// CreatePdf creates a new Inventory PDF generation request with the provided identity header
// Returns the statusID for tracking
func (c *Client) CreatePdf(ctx context.Context, req interface{}, identityHeader string) (string, error) {
	httpReq, err := c.createRequestWithIdentity(ctx, "POST", "/v2/create", req, identityHeader)
	if err != nil {
		return "", err
	}

	var result map[string]string
	if err := c.doRequest(httpReq, &result); err != nil {
		return "", fmt.Errorf("failed to create Inventory PDF: %w", err)
	}

	statusID, ok := result["statusID"]
	if !ok {
		return "", fmt.Errorf("statusID not found in response")
	}

	return statusID, nil
}

// GetPdfStatus retrieves the status of a specific Inventory PDF generation request
// Returns status string and filepath
func (c *Client) GetPdfStatus(ctx context.Context, statusID string, identityHeader string) (string, string, error) {
	endpoint := fmt.Sprintf("/v2/status/%s", statusID)

	req, err := c.createRequestWithIdentity(ctx, "GET", endpoint, nil, identityHeader)
	if err != nil {
		return "", "", err
	}

	var result map[string]interface{}
	if err := c.doRequest(req, &result); err != nil {
		return "", "", fmt.Errorf("failed to get Inventory PDF status: %w", err)
	}

	// Extract nested status object
	statusObj, ok := result["status"].(map[string]interface{})
	if !ok {
		return "", "", fmt.Errorf("invalid status response format")
	}

	status, _ := statusObj["status"].(string)
	filepath, _ := statusObj["filepath"].(string)

	return status, filepath, nil
}

// DownloadPdf downloads the generated Inventory PDF file
func (c *Client) DownloadPdf(ctx context.Context, statusID string, identityHeader string) ([]byte, error) {
	endpoint := fmt.Sprintf("/v2/download/%s", statusID)

	req, err := c.createRequestWithIdentity(ctx, "GET", endpoint, nil, identityHeader)
	if err != nil {
		return nil, err
	}

	requestID := req.Header.Get(RequestIDHeader)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		log.Printf("[DEBUG] Inventory PDF Generator - Download request failed - Request-ID: %s, Error: %v", requestID, err)
		return nil, fmt.Errorf("download request failed: %w", err)
	}
	defer resp.Body.Close()

	log.Printf("[DEBUG] Inventory PDF Generator - Download response received - Request-ID: %s, Status: %d", requestID, resp.StatusCode)

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		var errResp map[string]string
		if err := json.Unmarshal(body, &errResp); err != nil {
			return nil, fmt.Errorf("download failed (status %d): %s", resp.StatusCode, string(body))
		}
		return nil, fmt.Errorf("download failed (status %d): %s - %s", resp.StatusCode, errResp["error"], errResp["message"])
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read download data: %w", err)
	}

	log.Printf("[DEBUG] Inventory PDF Generator - Downloaded %d bytes - Request-ID: %s", len(data), requestID)

	return data, nil
}

// GetPdfDownloadURL returns the full download URL for an Inventory PDF
func (c *Client) GetPdfDownloadURL(statusID string) string {
	return c.baseURL + fmt.Sprintf("/v2/download/%s", statusID)
}

// WaitForPdfCompletion polls an Inventory PDF generation request until it's complete or failed
func WaitForPdfCompletion(client *Client, ctx context.Context, statusID string, identityHeader string, maxRetries int, pollInterval time.Duration) (string, string, error) {
	for attempt := 0; attempt < maxRetries; attempt++ {
		status, filepath, err := client.GetPdfStatus(ctx, statusID, identityHeader)
		if err != nil {
			return "", "", fmt.Errorf("failed to get Inventory PDF status: %w", err)
		}

		switch status {
		case "Generated":
			return status, filepath, nil
		case "Failed":
			return status, filepath, fmt.Errorf("Inventory PDF generation failed")
		case "Generating":
			// Continue waiting
			if attempt < maxRetries-1 {
				log.Printf("Inventory PDF generation in progress (attempt %d/%d), waiting %s...", attempt+1, maxRetries, pollInterval)
				time.Sleep(pollInterval)
			}
		case "NotFound":
			return status, filepath, fmt.Errorf("Inventory PDF not found")
		default:
			return status, filepath, fmt.Errorf("unknown Inventory PDF status: %s", status)
		}
	}

	return "", "", fmt.Errorf("Inventory PDF generation did not complete after %d polling attempts", maxRetries)
}
