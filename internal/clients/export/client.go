package export

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

/*
IDENTITY := $(shell echo -n '{"identity": {"account_number": "000202", "internal": {"org_id": "000101"}, "type": "User", "org_id": "000101", "auth_type": "jwt-auth", "user":{"username": "wilma", "user_id": "wilma-1"}}}' | base64 -w 0)
*/

// Identity represents the Red Hat identity structure
type Identity struct {
	AccountNumber string `json:"account_number"`
	OrgID         string `json:"org_id"`
	Type          string `json:"type"`
	AuthType      string `json:"auth_type"`
	Internal      struct {
		OrgID string `json:"org_id"`
	} `json:"internal"`
	User struct {
		Username string `json:"username"`
		UserID   string `json:"user_id"`
	} `json:"user"`
	System struct {
		CN       string `json:"cn"`
		CertType string `json:"cert_type"`
	} `json:"system"`
}

// IdentityHeader represents the x-rh-identity header structure
type IdentityHeader struct {
	Identity Identity `json:"identity"`
}

// Client represents the export service REST client
type Client struct {
	baseURL    string
	httpClient *http.Client
	identity   IdentityHeader
}

// NewClient creates a new export service client
func NewClient(baseURL string, accountNumber string, orgID string) *Client {
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},

		identity: IdentityHeader{
			Identity: Identity{
				AccountNumber: accountNumber,
				OrgID:         orgID,
				Type:          "User",
				AuthType:      "jwt-auth",
				Internal: struct {
					OrgID string `json:"org_id"`
				}{
					OrgID: orgID,
				},
				User: struct {
					Username string `json:"username"`
					UserID   string `json:"user_id"`
				}{
					Username: "fred.flintstone",
					UserID:   "4321-34-32",
				},
			},
		},
	}
}

// SetHTTPClient allows setting a custom HTTP client
func (c *Client) SetHTTPClient(client *http.Client) {
	c.httpClient = client
}

// createRequest creates an HTTP request with proper headers using default identity
func (c *Client) createRequest(ctx context.Context, method, endpoint string, body interface{}) (*http.Request, error) {
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

	// Set content type for requests with body
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	// Set Red Hat identity header
	identityJSON, err := json.Marshal(c.identity)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal identity: %w", err)
	}
	identityHeader := base64.StdEncoding.EncodeToString(identityJSON)
	req.Header.Set("x-rh-identity", identityHeader)

	return req, nil
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

	// Set content type for requests with body
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	// Set the provided Red Hat identity header
	req.Header.Set("x-rh-identity", identityHeader)

	return req, nil
}

// doRequest executes an HTTP request and handles the response
func (c *Client) doRequest(req *http.Request, result interface{}) error {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode >= 400 {
		var errResp ErrorResponse
		if err := json.Unmarshal(body, &errResp); err != nil {
			return fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
		}
		return fmt.Errorf("API error (status %d): %s - %s", resp.StatusCode, errResp.Error, errResp.Message)
	}

	if result != nil {
		if err := json.Unmarshal(body, result); err != nil {
			return fmt.Errorf("failed to unmarshal response: %w", err)
		}
	}

	return nil
}

// CreateExport creates a new export request with the provided identity header
func (c *Client) CreateExport(ctx context.Context, req ExportRequest, identityHeader string) (*ExportStatusResponse, error) {
	httpReq, err := c.createRequestWithIdentity(ctx, "POST", "/exports", req, identityHeader)
	if err != nil {
		return nil, err
	}

	var result ExportStatusResponse
	if err := c.doRequest(httpReq, &result); err != nil {
		return nil, fmt.Errorf("failed to create export: %w", err)
	}

	return &result, nil
}

// ListExports retrieves a list of export requests
func (c *Client) ListExports(ctx context.Context, params *ListParams) (*ExportListResponse, error) {
	endpoint := "/exports"

	if params != nil {
		queryParams := url.Values{}

		if params.Name != nil {
			queryParams.Add("name", *params.Name)
		}
		if params.CreatedAt != nil {
			queryParams.Add("created_at", params.CreatedAt.Format(time.RFC3339))
		}
		if params.Application != nil {
			queryParams.Add("application", string(*params.Application))
		}
		if params.Status != nil {
			queryParams.Add("status", string(*params.Status))
		}
		if params.Limit != nil {
			queryParams.Add("limit", strconv.Itoa(*params.Limit))
		}
		if params.Offset != nil {
			queryParams.Add("offset", strconv.Itoa(*params.Offset))
		}

		if len(queryParams) > 0 {
			endpoint += "?" + queryParams.Encode()
		}
	}

	req, err := c.createRequest(ctx, "GET", endpoint, nil)
	if err != nil {
		return nil, err
	}

	var result ExportListResponse
	if err := c.doRequest(req, &result); err != nil {
		return nil, fmt.Errorf("failed to list exports: %w", err)
	}

	return &result, nil
}

// GetExportStatus retrieves the status of a specific export
func (c *Client) GetExportStatus(ctx context.Context, exportID string) (*ExportStatusResponse, error) {
	endpoint := fmt.Sprintf("/exports/%s/status", exportID)

	req, err := c.createRequest(ctx, "GET", endpoint, nil)
	if err != nil {
		return nil, err
	}

	var result ExportStatusResponse
	if err := c.doRequest(req, &result); err != nil {
		return nil, fmt.Errorf("failed to get export status: %w", err)
	}

	return &result, nil
}

// DownloadExport downloads the exported data as a zip file
func (c *Client) DownloadExport(ctx context.Context, exportID string) ([]byte, error) {
	endpoint := fmt.Sprintf("/exports/%s", exportID)

	req, err := c.createRequest(ctx, "GET", endpoint, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		var errResp ErrorResponse
		if err := json.Unmarshal(body, &errResp); err != nil {
			return nil, fmt.Errorf("download failed (status %d): %s", resp.StatusCode, string(body))
		}
		return nil, fmt.Errorf("download failed (status %d): %s - %s", resp.StatusCode, errResp.Error, errResp.Message)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read download data: %w", err)
	}

	return data, nil
}

// GetExportDownloadURL returns the full download URL for an export
func (c *Client) GetExportDownloadURL(exportID string) string {
	return c.baseURL + fmt.Sprintf("/exports/%s", exportID)
}

// DeleteExport deletes an export request
func (c *Client) DeleteExport(ctx context.Context, exportID string) error {
	endpoint := fmt.Sprintf("/exports/%s", exportID)

	req, err := c.createRequest(ctx, "DELETE", endpoint, nil)
	if err != nil {
		return err
	}

	if err := c.doRequest(req, nil); err != nil {
		return fmt.Errorf("failed to delete export: %w", err)
	}

	return nil
}
