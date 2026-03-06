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

// ThreeScaleUserInfo represents the user information returned from the 3scale service
type ThreeScaleUserInfo struct {
	UserID        string `json:"user_id"`
	Username      string `json:"username"`
	Email         string `json:"email"`
	AccountNumber string `json:"account_number"`
	OrgID         string `json:"org_id"`
	IsActive      bool   `json:"is_active"`
	FirstName     string `json:"first_name,omitempty"`
	LastName      string `json:"last_name,omitempty"`
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
	url := fmt.Sprintf("%s/v1/users/%s?org_id=%s", v.baseURL, username, orgID)

	// Create HTTP request with context
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers including request-id
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-Id", requestID)
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
		log.Printf("[ThreeScaleUserValidator] Validation failed - request_id=%s status=%d body=%s",
			requestID, resp.StatusCode, string(bodyBytes))
		return "", fmt.Errorf("validation service returned status %d (request_id=%s): %s",
			resp.StatusCode, requestID, string(bodyBytes))
	}

	// Parse response
	var userInfo ThreeScaleUserInfo
	if err := json.NewDecoder(resp.Body).Decode(&userInfo); err != nil {
		log.Printf("[ThreeScaleUserValidator] Failed to decode response - request_id=%s error=%v",
			requestID, err)
		return "", fmt.Errorf("failed to decode response (request_id=%s): %w", requestID, err)
	}

	// Validate user information
	if !userInfo.IsActive {
		log.Printf("[ThreeScaleUserValidator] User is inactive - request_id=%s username=%s",
			requestID, username)
		return "", fmt.Errorf("user is inactive (request_id=%s)", requestID)
	}

	if userInfo.OrgID != orgID {
		log.Printf("[ThreeScaleUserValidator] OrgID mismatch - request_id=%s expected=%s got=%s",
			requestID, orgID, userInfo.OrgID)
		return "", fmt.Errorf("org_id mismatch (request_id=%s): expected %s, got %s",
			requestID, orgID, userInfo.OrgID)
	}

	if userInfo.UserID != userID {
		log.Printf("[ThreeScaleUserValidator] UserID mismatch - request_id=%s expected=%s got=%s",
			requestID, userID, userInfo.UserID)
		return "", fmt.Errorf("user_id mismatch (request_id=%s): expected %s, got %s",
			requestID, userID, userInfo.UserID)
	}

	log.Printf("[ThreeScaleUserValidator] User validated successfully - request_id=%s org_id=%s username=%s",
		requestID, userInfo.OrgID, userInfo.Username)

	// Build the identity header using the HTTP response
	identity := platformIdentity.XRHID{
		Identity: platformIdentity.Identity{
			AccountNumber: userInfo.AccountNumber,
			OrgID:         userInfo.OrgID,
			Type:          "User",
			AuthType:      "jwt-auth",
			Internal: platformIdentity.Internal{
				OrgID: userInfo.OrgID,
			},
			User: &platformIdentity.User{
				Username: userInfo.Username,
				UserID:   userInfo.UserID,
				Email:    userInfo.Email,
			},
		},
	}

	identityJSON, err := json.Marshal(identity)
	if err != nil {
		return "", fmt.Errorf("failed to marshal identity (request_id=%s): %w", requestID, err)
	}

	return base64.StdEncoding.EncodeToString(identityJSON), nil
}
