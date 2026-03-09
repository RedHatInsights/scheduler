package identity

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"
	platformIdentity "github.com/redhatinsights/platform-go-middlewares/v2/identity"
)

// ThreeScaleUserValidator implements UserValidator by calling the 3scale API Management service via GET
type ThreeScaleUserValidator struct {
	baseURL    string
	httpClient *http.Client
	apiToken   string
}

// NewThreeScaleUserValidator creates a new ThreeScaleUserValidator with the given base URL and credentials
func NewThreeScaleUserValidator(baseURL, apiToken string) *ThreeScaleUserValidator {
	return &ThreeScaleUserValidator{
		baseURL:  baseURL,
		apiToken: apiToken,
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

// NewThreeScaleUserValidatorWithClient creates a new ThreeScaleUserValidator with a custom HTTP client
func NewThreeScaleUserValidatorWithClient(baseURL, apiToken string, client *http.Client) *ThreeScaleUserValidator {
	return &ThreeScaleUserValidator{
		baseURL:    baseURL,
		apiToken:   apiToken,
		httpClient: client,
	}
}

// ThreeScaleResponse represents the response from the 3scale service
type ThreeScaleResponse struct {
	XRHIdentity string `json:"x-rh-identity"`
}

// ThreeScaleError represents an error response from the 3scale service
type ThreeScaleError struct {
	Errors []struct {
		Meta struct {
			ResponseBy string `json:"response_by"`
		} `json:"meta"`
		Status int    `json:"status"`
		Detail string `json:"detail"`
	} `json:"errors"`
}

// GenerateIdentityHeader calls an HTTP GET service to validate user and generate identity header
func (v *ThreeScaleUserValidator) GenerateIdentityHeader(ctx context.Context, orgID, username, userID string) (string, error) {
	if orgID == "" {
		return "", fmt.Errorf("orgID cannot be empty")
	}
	if username == "" {
		return "", fmt.Errorf("username cannot be empty")
	}
	if userID == "" {
		return "", fmt.Errorf("userID cannot be empty")
	}

	// Generate UUID for request tracking
	requestID := uuid.New().String()
	log.Printf("[ThreeScaleUserValidator] Validating user - request_id=%s org_id=%s username=%s user_id=%s",
		requestID, orgID, username, userID)

	// Construct the request URL with query parameters
	url := fmt.Sprintf("%s/internal/userIdentity", v.baseURL)

	// Create HTTP request with context
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers including request-id and user-id
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-rh-insights-request-id", requestID)
	req.Header.Set("X-Rh-User-Id", userID)

	if v.apiToken != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", v.apiToken))
	}

	// Record start time for metrics
	startTime := time.Now()

	// Make the request
	resp, err := v.httpClient.Do(req)

	// Calculate duration for metrics
	duration := time.Since(startTime)

	// Record metrics
	statusCode := "error"
	if resp != nil {
		statusCode = fmt.Sprintf("%d", resp.StatusCode)
	}

	// Record metrics
	UserValidationHTTPDuration.WithLabelValues("GET", statusCode).Observe(duration.Seconds())
	UserValidationHTTPRequestsTotal.WithLabelValues("GET", statusCode).Inc()

	// Log the call result
	log.Printf("[ThreeScaleUserValidator] HTTP call completed - request_id=%s status=%s duration=%v",
		requestID, statusCode, duration)

	if err != nil {
		return "", fmt.Errorf("failed to call validation service (request_id=%s): %w", requestID, err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)

		// Try to parse as structured error response
		var errorResp ThreeScaleError
		if err := json.Unmarshal(bodyBytes, &errorResp); err == nil && len(errorResp.Errors) > 0 {
			firstError := errorResp.Errors[0]
			log.Printf("[ThreeScaleUserValidator] Validation failed - request_id=%s status=%d error_status=%d detail=%s response_by=%s",
				requestID, resp.StatusCode, firstError.Status, firstError.Detail, firstError.Meta.ResponseBy)
			return "", fmt.Errorf("validation service error (request_id=%s): %s", requestID, firstError.Detail)
		}

		// Fallback to raw body if not structured error
		log.Printf("[ThreeScaleUserValidator] Validation failed - request_id=%s status=%d body=%s",
			requestID, resp.StatusCode, string(bodyBytes))
		return "", fmt.Errorf("validation service returned status %d (request_id=%s): %s",
			resp.StatusCode, requestID, string(bodyBytes))
	}

	// Parse response
	var response ThreeScaleResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		log.Printf("[ThreeScaleUserValidator] Failed to decode response - request_id=%s error=%v",
			requestID, err)
		return "", fmt.Errorf("failed to decode response (request_id=%s): %w", requestID, err)
	}

	// Validate that we got an identity header
	if response.XRHIdentity == "" {
		log.Printf("[ThreeScaleUserValidator] Empty identity header - request_id=%s",
			requestID)
		return "", fmt.Errorf("empty identity header in response (request_id=%s)", requestID)
	}

	// Decode and validate the identity header
	identityJSON, err := base64.StdEncoding.DecodeString(response.XRHIdentity)
	if err != nil {
		log.Printf("[ThreeScaleUserValidator] Failed to decode identity header - request_id=%s error=%v",
			requestID, err)
		return "", fmt.Errorf("failed to decode identity header (request_id=%s): %w", requestID, err)
	}

	// Parse the identity to validate it
	var identity platformIdentity.XRHID
	if err := json.Unmarshal(identityJSON, &identity); err != nil {
		log.Printf("[ThreeScaleUserValidator] Failed to parse identity JSON - request_id=%s error=%v",
			requestID, err)
		return "", fmt.Errorf("failed to parse identity JSON (request_id=%s): %w", requestID, err)
	}

	// Validate org_id matches
	if identity.Identity.OrgID != orgID {
		log.Printf("[ThreeScaleUserValidator] OrgID mismatch - request_id=%s expected=%s got=%s",
			requestID, orgID, identity.Identity.OrgID)
		return "", fmt.Errorf("org_id mismatch (request_id=%s): expected %s, got %s",
			requestID, orgID, identity.Identity.OrgID)
	}

	// Validate user_id matches (if user is present)
	if identity.Identity.User != nil && identity.Identity.User.UserID != userID {
		log.Printf("[ThreeScaleUserValidator] UserID mismatch - request_id=%s expected=%s got=%s",
			requestID, userID, identity.Identity.User.UserID)
		return "", fmt.Errorf("user_id mismatch (request_id=%s): expected %s, got %s",
			requestID, userID, identity.Identity.User.UserID)
	}

	log.Printf("[ThreeScaleUserValidator] User validated successfully - request_id=%s org_id=%s username=%s",
		requestID, identity.Identity.OrgID, username)

	// Return the base64-encoded identity header as-is
	return response.XRHIdentity, nil
}
