package identity

import (
	"bytes"
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

// Token exchange constants for parsec (RFC 8693).
const (
	parsecTokenPath          = "/v1/token"
	parsecGrantType          = "urn:ietf:params:oauth:grant-type:token-exchange"
	parsecSubjectTokenType   = "urn:ietf:params:oauth:token-type:unsigned_json"
	parsecRequestedTokenType = "urn:redhat:params:oauth:token-type:rh-identity"
	// parsecSubjectPrefix namespaces the user id in the subject token. parsec's
	// CEL identity mapper only takes the BOP-enrichment path when the subject
	// starts with this prefix, so it is required.
	parsecSubjectPrefix = "redhat:user:sso:"
)

// ParsecUserValidator implements UserValidator by calling parsec's RFC 8693
// token-exchange endpoint. It exchanges an unsigned-JSON subject token carrying
// the user id for a base64-encoded x-rh-identity envelope (enriched from BOP by
// parsec). Like the 3scale validator, it passes the returned envelope through
// after validating org_id/user_id.
type ParsecUserValidator struct {
	baseURL    string
	httpClient *http.Client
}

// NewParsecUserValidator creates a new ParsecUserValidator with the given base URL and timeout
func NewParsecUserValidator(baseURL string, timeout time.Duration) *ParsecUserValidator {
	return &ParsecUserValidator{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

// NewParsecUserValidatorWithClient creates a new ParsecUserValidator with a custom HTTP client
func NewParsecUserValidatorWithClient(baseURL string, client *http.Client) *ParsecUserValidator {
	return &ParsecUserValidator{
		baseURL:    baseURL,
		httpClient: client,
	}
}

// parsecExchangeRequest is the RFC 8693 token-exchange request body.
type parsecExchangeRequest struct {
	GrantType          string `json:"grant_type"`
	SubjectToken       string `json:"subject_token"`
	SubjectTokenType   string `json:"subject_token_type"`
	RequestedTokenType string `json:"requested_token_type"`
}

// parsecExchangeResponse is the token-exchange response. We always request the
// JSON transport, so parsec returns the token as camelCase accessToken. (The
// form-encoded transport, which we do not use, would return snake_case
// access_token.)
type parsecExchangeResponse struct {
	AccessToken string `json:"accessToken"`
}

// GenerateIdentityHeader exchanges the user id for an x-rh-identity header via parsec's token endpoint
func (v *ParsecUserValidator) GenerateIdentityHeader(ctx context.Context, orgID, userID string) (string, error) {
	if orgID == "" {
		return "", fmt.Errorf("orgID cannot be empty")
	}
	if userID == "" {
		return "", fmt.Errorf("userID cannot be empty")
	}

	// Generate UUID for request tracking
	requestID := uuid.New().String()
	log.Printf("[ParsecUserValidator] Validating user - request_id=%s org_id=%s user_id=%s",
		requestID, orgID, userID)

	// The subject_token is a string containing a JSON object with a "sub" claim.
	subjectTokenBytes, err := json.Marshal(map[string]string{"sub": parsecSubjectPrefix + userID})
	if err != nil {
		log.Printf("[ParsecUserValidator] failed to build subject token - request_id=%s org_id=%s user_id=%s - err: %s",
			requestID, orgID, userID, err)
		return "", fmt.Errorf("failed to create user validation request")
	}

	bodyBytes, err := json.Marshal(parsecExchangeRequest{
		GrantType:          parsecGrantType,
		SubjectToken:       string(subjectTokenBytes),
		SubjectTokenType:   parsecSubjectTokenType,
		RequestedTokenType: parsecRequestedTokenType,
	})
	if err != nil {
		log.Printf("[ParsecUserValidator] failed to marshal request - request_id=%s org_id=%s user_id=%s - err: %s",
			requestID, orgID, userID, err)
		return "", fmt.Errorf("failed to create user validation request")
	}

	url := fmt.Sprintf("%s%s", v.baseURL, parsecTokenPath)

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		log.Printf("[ParsecUserValidator] failed to create request - request_id=%s org_id=%s user_id=%s - err: %s",
			requestID, orgID, userID, err)
		return "", fmt.Errorf("failed to create user validation request")
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-rh-insights-request-id", requestID)

	// Record start time for metrics
	startTime := time.Now()

	resp, err := v.httpClient.Do(req)

	duration := time.Since(startTime)

	statusCode := "error"
	if resp != nil {
		statusCode = fmt.Sprintf("%d", resp.StatusCode)
	}

	// Record metrics (the histogram's _count series doubles as the request total)
	ParsecUserValidationDuration.WithLabelValues("POST", statusCode).Observe(duration.Seconds())

	log.Printf("[ParsecUserValidator] HTTP call completed - request_id=%s status=%s duration=%v",
		requestID, statusCode, duration)

	if err != nil {
		log.Printf("[ParsecUserValidator] HTTP call failed - request_id=%s status=%s duration=%v - err: %s",
			requestID, statusCode, duration, err)
		return "", fmt.Errorf("failed to call user validation service")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		log.Printf("[ParsecUserValidator] Validation failed - request_id=%s status=%d body=%s",
			requestID, resp.StatusCode, string(respBody))
		return "", fmt.Errorf("user validation service returned an error")
	}

	// Parse response
	var response parsecExchangeResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		log.Printf("[ParsecUserValidator] Failed to decode response - request_id=%s error=%v",
			requestID, err)
		return "", fmt.Errorf("unable to process response from user validation service")
	}

	identityHeader := response.AccessToken
	if identityHeader == "" {
		log.Printf("[ParsecUserValidator] Empty identity header - request_id=%s",
			requestID)
		return "", fmt.Errorf("empty response from user validation service")
	}

	// Decode and validate the identity header
	identityJSON, err := base64.StdEncoding.DecodeString(identityHeader)
	if err != nil {
		log.Printf("[ParsecUserValidator] Failed to decode identity header - request_id=%s error=%v",
			requestID, err)
		return "", fmt.Errorf("unable to process response from user validation service")
	}

	var identity platformIdentity.XRHID
	if err := json.Unmarshal(identityJSON, &identity); err != nil {
		log.Printf("[ParsecUserValidator] Failed to parse identity JSON - request_id=%s error=%v",
			requestID, err)
		return "", fmt.Errorf("unable to process response from user validation service")
	}

	// Validate org_id matches the stored value
	if identity.Identity.OrgID != orgID {
		log.Printf("[ParsecUserValidator] OrgID mismatch - request_id=%s expected=%s got=%s",
			requestID, orgID, identity.Identity.OrgID)
		return "", fmt.Errorf("unable to process response from user validation service")
	}

	// Validate this is a user identity (the scheduler only handles user-scheduled jobs)
	if identity.Identity.Type != "User" {
		log.Printf("[ParsecUserValidator] Unexpected identity type - request_id=%s type=%s",
			requestID, identity.Identity.Type)
		return "", fmt.Errorf("unable to process response from user validation service")
	}

	// Validate user is present
	if identity.Identity.User == nil {
		log.Printf("[ParsecUserValidator] User is nil in identity - request_id=%s",
			requestID)
		return "", fmt.Errorf("unable to process response from user validation service")
	}

	// Validate user_id matches the stored value
	if identity.Identity.User.UserID != userID {
		log.Printf("[ParsecUserValidator] UserID mismatch - request_id=%s expected=%s got=%s",
			requestID, userID, identity.Identity.User.UserID)
		return "", fmt.Errorf("unable to process response from user validation service")
	}

	log.Printf("[ParsecUserValidator] User validated successfully - request_id=%s org_id=%s",
		requestID, identity.Identity.OrgID)

	// Return the base64-encoded identity header as-is
	return identityHeader, nil
}
