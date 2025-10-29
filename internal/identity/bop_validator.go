package identity

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// BopUserValidator implements UserValidator by calling an external HTTP service
type BopUserValidator struct {
	baseURL     string
	httpClient  *http.Client
	apiToken    string
	clientID    string
	insightsEnv string
}

// NewBopUserValidator creates a new BopUserValidator with the given base URL and credentials
func NewBopUserValidator(baseURL, apiToken, clientID, insightsEnv string) *BopUserValidator {
	fmt.Println("Using BOP based user validator")
	return &BopUserValidator{
		baseURL:     baseURL,
		apiToken:    apiToken,
		clientID:    clientID,
		insightsEnv: insightsEnv,
		httpClient: &http.Client{
			Timeout: 2 * time.Second,
		},
	}
}

// NewBopUserValidatorWithClient creates a new BopUserValidator with a custom HTTP client
func NewBopUserValidatorWithClient(baseURL, apiToken, clientID, insightsEnv string, client *http.Client) *BopUserValidator {
	return &BopUserValidator{
		baseURL:     baseURL,
		apiToken:    apiToken,
		clientID:    clientID,
		insightsEnv: insightsEnv,
		httpClient:  client,
	}
}

// UserValidationRequest represents the request payload for user validation
type UserValidationRequest struct {
	OrgID    string `json:"org_id"`
	Username string `json:"username"`
}

type UserInfo struct {
	ID            string `json:"id"`
	Username      string `json:"username"`
	AccountNumber string `json:"account_number"`
	OrgID         string `json:"org_id"`
	IsActive      bool   `json:"is_active"`
}

// UserValidationResponse represents the response from the validation service
type UserValidationResponse struct {
	Users []UserInfo
}

// GenerateIdentityHeader calls an HTTP service to generate the identity header
func (v *BopUserValidator) GenerateIdentityHeader(orgID, username, userID string) (string, error) {
	if orgID == "" {
		return "", fmt.Errorf("orgID cannot be empty")
	}
	if username == "" {
		return "", fmt.Errorf("username cannot be empty")
	}
	if userID == "" {
		return "", fmt.Errorf("userID cannot be empty")
	}

	// Construct the request URL
	url := fmt.Sprintf("%s/v1/users", v.baseURL)

	postBody := fmt.Sprintf("{ \"users\": [ \"%s\" ] }", username)
	postBodyReader := strings.NewReader(postBody)

	fmt.Println("BOP URL: ", url)
	fmt.Println("BOP post body: ", postBody)

	// Create HTTP request
	req, err := http.NewRequest("POST", url, postBodyReader)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-rh-apitoken", v.apiToken)
	req.Header.Set("x-rh-clientid", v.clientID)
	req.Header.Set("x-rh-insights-env", v.insightsEnv)

	// Make the request
	resp, err := v.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to call validation service: %w", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("validation service returned status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	// Parse response
	var validationResp []UserInfo
	if err := json.NewDecoder(resp.Body).Decode(&validationResp); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	fmt.Printf("valdationResp: %+v\n", validationResp)

	if len(validationResp) != 1 {
		return "", fmt.Errorf("invalid response: %w", err)
	}

	if !validationResp[0].IsActive {
		return "", fmt.Errorf("inactive user")
	}

	if validationResp[0].OrgID != orgID {
		return "", fmt.Errorf("org-id mismatch...invalid user")
	}

	/*
	    BOP doesn't read the user_id from the attributes...so the user id it returns is kinda random
		if validationResp[0].ID != userID {
			return "", fmt.Errorf("user-id mismatch...invalid user")
		}
	*/

	// This is kind of a hack, but it should work for now
	identityHeaderBuilder := NewDefaultUserValidator(validationResp[0].AccountNumber)

	return identityHeaderBuilder.GenerateIdentityHeader(validationResp[0].OrgID, validationResp[0].Username, validationResp[0].ID)
}
