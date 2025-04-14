// Authentication functionality for the Aruba Central API client.
// This file handles token management, authentication flows, and token refresh.
package arubacentral

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"
)

// Authentication-related constants
const (
	// TokenRefreshBuffer is the default token refresh buffer in seconds (15 minutes).
	// Tokens will be refreshed when they have less than this amount of time remaining.
	TokenRefreshBuffer = 900
)

// tokenMutex protects token-related operations to ensure thread safety.
var tokenMutex sync.Mutex

// ReadConfig reads the client configuration from a JSON file.
//
// Parameters:
//   - filename: Path to the configuration file
//
// Returns:
//   - The parsed configuration
//   - An error if the file cannot be read or parsed
func ReadConfig(filename string) (*Config, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, WrapError(err, fmt.Sprintf("cannot read config file %s", filename))
	}

	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, NewConfigError(err, "cannot unmarshal config")
	}

	return &config, nil
}

// SaveConfig saves the client configuration to a JSON file.
//
// Parameters:
//   - config: Configuration to save
//   - filename: Path to the configuration file
//
// Returns:
//   - An error if the configuration cannot be marshaled or the file cannot be written
func SaveConfig(config *Config, filename string) error {
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return WrapError(err, "failed to marshal config")
	}

	if err := os.WriteFile(filename, data, 0o600); err != nil {
		return WrapError(err, fmt.Sprintf("failed to write config to %s", filename))
	}

	return nil
}

// EnsureValidToken ensures the client has a valid access token.
// It checks the existing token's expiry and refreshes it if needed,
// or performs a full authentication flow if no token is available.
//
// Parameters:
//   - tenantID: ID of the Aruba Central tenant
//
// Returns:
//   - An error if authentication fails
func (c *Client) EnsureValidToken(tenantID string) error {
	tokenMutex.Lock()
	defer tokenMutex.Unlock()

	// Determine refresh buffer (default to 15 minutes if not specified or zero)
	refreshBuffer := TokenRefreshBuffer
	if c.Config.API.RefreshBuffer > 0 {
		refreshBuffer = c.Config.API.RefreshBuffer
		c.DebugLogger("Using configured token refresh buffer: %d seconds", refreshBuffer)
	} else {
		c.DebugLogger("Using default token refresh buffer: %d seconds", refreshBuffer)
	}

	// Check if we have a cached token that's still valid
	if c.Config.TokenCache.AccessToken != "" {
		var timeLeft int64
		var creationSource string

		// If created_at is 0, we need to handle this differently
		if c.Config.TokenCache.CreatedAt == 0 {
			// Check if we have a file modification time we can use
			fileInfo, err := os.Stat(c.ConfigPath)
			if err == nil {
				// Use file modification time as a proxy for when the token was last refreshed
				modTime := fileInfo.ModTime().Unix()
				// Calculate time left based on file modification time
				timeLeft = modTime + int64(c.Config.TokenCache.ExpiresIn) - time.Now().Unix()
				creationSource = "file modification time"
				c.DebugLogger("Using %s to estimate token expiration: %d seconds left", creationSource, timeLeft)
			} else {
				// If we can't get file info, assume token needs refresh
				c.DebugLogger("Cannot determine token age, assuming refresh needed")
				timeLeft = -1
				creationSource = "unknown (assuming expired)"
				c.DebugLogger("Token source: %s", creationSource)
			}
		} else {
			// Normal calculation - created_at is in milliseconds, convert to seconds
			creationTime := c.Config.TokenCache.CreatedAt / 1000
			expiresAt := creationTime + int64(c.Config.TokenCache.ExpiresIn)
			timeLeft = expiresAt - time.Now().Unix()
			creationSource = fmt.Sprintf("token creation timestamp (%s)",
				time.Unix(creationTime, 0).Format(time.RFC3339))
			c.DebugLogger("Token created at: %s", time.Unix(creationTime, 0).Format(time.RFC3339))
			c.DebugLogger("Token expires at: %s", time.Unix(expiresAt, 0).Format(time.RFC3339))
			c.DebugLogger("Token expires in %d seconds (based on %s)", timeLeft, creationSource)
		}

		if timeLeft > int64(refreshBuffer) {
			c.DebugLogger("Using cached token (expires in %d seconds, refresh buffer is %d seconds)",
				timeLeft, refreshBuffer)
			return nil
		}

		if timeLeft <= 0 {
			c.DebugLogger("Token has expired (%d seconds ago), refreshing", -timeLeft)
		} else {
			c.DebugLogger("Token expiring soon (%d seconds left, refresh buffer is %d seconds), refreshing",
				timeLeft, refreshBuffer)
		}
		return c.handleTokenExpiry(tenantID)
	}

	// No token available, perform full authentication
	c.DebugLogger("No token available, starting full authentication flow")
	return c.AuthenticateAndGetToken(tenantID)
}

// handleTokenExpiry handles token expiry by first attempting to refresh the token,
// then falling back to full authentication if refresh fails.
//
// Parameters:
//   - tenantID: ID of the Aruba Central tenant
//
// Returns:
//   - An error if both refresh and full authentication fail
func (c *Client) handleTokenExpiry(tenantID string) error {
	c.DebugLogger("Handling token expiry...")

	// Try to refresh the token if we have a refresh token
	if c.Config.TokenCache.RefreshToken != "" {
		if err := c.refreshExistingToken(tenantID); err != nil {
			// If refresh failed with invalid grant, try full authentication
			if strings.Contains(err.Error(), "invalid_grant") {
				c.DebugLogger("Invalid refresh token, falling back to full authentication flow")
				return c.AuthenticateAndGetToken(tenantID)
			}
			return err
		}
		return nil
	}

	// No refresh token available, perform full authentication
	c.DebugLogger("No refresh token available, starting full authentication flow")
	return c.AuthenticateAndGetToken(tenantID)
}

// refreshExistingToken attempts to refresh the token using the refresh token.
// It makes a direct API call to the token endpoint.
//
// Parameters:
//   - tenantID: ID of the Aruba Central tenant
//
// Returns:
//   - An error if the token refresh fails
func (c *Client) refreshExistingToken(tenantID string) error {
	c.DebugLogger("Attempting to refresh token using refresh_token: %s... (first 10 chars)",
		c.Config.TokenCache.RefreshToken[:min(10, len(c.Config.TokenCache.RefreshToken))])

	// Construct token endpoint
	tokenEndpoint := fmt.Sprintf("%s/oauth2/token", c.Config.API.Endpoint)

	// Make a direct API call to refresh the token
	ctx, cancel := context.WithTimeout(context.Background(), c.getTimeout("auth"))
	defer cancel()

	data := url.Values{}
	data.Set("client_id", c.Config.API.ClientID)
	data.Set("client_secret", c.Config.API.ClientSecret)
	data.Set("grant_type", "refresh_token")
	data.Set("refresh_token", c.Config.TokenCache.RefreshToken)

	req, err := http.NewRequestWithContext(ctx, "POST", tokenEndpoint, strings.NewReader(data.Encode()))
	if err != nil {
		c.DebugLogger("Error creating refresh token request: %v", err)
		return fmt.Errorf("error creating refresh token request: %w", err)
	}

	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Add("Accept", "application/json")
	req.Header.Add("User-Agent", UserAgent)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		c.DebugLogger("Error making refresh token request: %v", err)
		return fmt.Errorf("error making refresh token request: %w", err)
	}
	defer resp.Body.Close()

	c.logResponseRateLimits(resp)

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		c.DebugLogger("Refresh token request failed with status %d: %s", resp.StatusCode, string(body))

		// Check for rate limiting
		if resp.StatusCode == 429 {
			retryAfter := 0
			if retryHeader := resp.Header.Get("Retry-After"); retryHeader != "" {
				retryAfter, _ = strconv.Atoi(retryHeader)
			}
			return fmt.Errorf("rate limited during token refresh, retry after %d seconds", retryAfter)
		}

		// For other errors, just return the error message
		return fmt.Errorf("refresh token request failed with status %d: %s", resp.StatusCode, string(body))
	}

	return c.processTokenResponse(resp)
}

// processTokenResponse processes the token response and updates the config.
// It parses the JSON response and saves the token information.
//
// Parameters:
//   - resp: HTTP response containing token information
//
// Returns:
//   - An error if the response cannot be parsed or the token is invalid
func (c *Client) processTokenResponse(resp *http.Response) error {
	// Parse the response
	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		TokenType    string `json:"token_type"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		c.DebugLogger("Error decoding refresh token response: %v", err)
		return WrapError(err, "error decoding token response")
	}

	// Check if we got a valid access token
	if tokenResp.AccessToken == "" {
		c.DebugLogger("Received empty access token in response")
		return NewEmptyResponseError("access token")
	}

	c.updateConfigFromTokenResponse(tokenResp)

	// Save the updated token to the config file
	if err := SaveConfig(c.Config, c.ConfigPath); err != nil {
		c.DebugLogger("Failed to save updated token: %v", err)
	} else {
		c.DebugLogger("Token successfully saved to config file")
	}
	return nil
}

// updateConfigFromTokenResponse updates the config with the token response.
// It handles token creation timestamps and determines if the token has changed.
//
// Parameters:
//   - tokenResp: Token response from the API
func (c *Client) updateConfigFromTokenResponse(tokenResp struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	TokenType    string `json:"token_type"`
}) {
	// Check if the token has actually changed
	if tokenResp.AccessToken == c.Config.TokenCache.AccessToken {
		c.DebugLogger("Token source returned same token - no actual refresh occurred")

		// Even if the token didn't change, update the expiry if it's different
		if tokenResp.ExpiresIn != c.Config.TokenCache.ExpiresIn {
			oldExpiry := c.Config.TokenCache.ExpiresIn
			c.Config.TokenCache.ExpiresIn = tokenResp.ExpiresIn
			c.DebugLogger("Token has not changed, keeping existing creation time")
			c.DebugLogger("Updating token expiry from %d to %d seconds", oldExpiry, tokenResp.ExpiresIn)
		} else {
			c.DebugLogger("Token and expiry unchanged, no updates needed")
		}
	} else {
		// Token has changed, update everything
		c.DebugLogger("Received new access token: %s... (first 10 chars)",
			tokenResp.AccessToken[:min(10, len(tokenResp.AccessToken))])

		// Update the token in the config
		c.Config.TokenCache.AccessToken = tokenResp.AccessToken
		c.Config.TokenCache.ExpiresIn = tokenResp.ExpiresIn
		c.DebugLogger("New token expires in %d seconds", tokenResp.ExpiresIn)

		// Always update the refresh token if provided
		if tokenResp.RefreshToken != "" {
			c.DebugLogger("Received new refresh token: %s... (first 10 chars)",
				tokenResp.RefreshToken[:min(10, len(tokenResp.RefreshToken))])
			c.Config.TokenCache.RefreshToken = tokenResp.RefreshToken
		} else {
			c.DebugLogger("No new refresh token provided, keeping existing refresh token")
		}

		// Update the creation timestamp (in milliseconds)
		c.Config.TokenCache.CreatedAt = time.Now().UnixNano() / int64(time.Millisecond)
		c.DebugLogger("Updated token creation timestamp to: %s",
			time.Unix(c.Config.TokenCache.CreatedAt/1000, 0).Format(time.RFC3339))
	}
}

// AuthenticateAndGetToken performs the full authentication flow to get a new token.
// This implements the complete OAuth2 flow for Aruba Central.
//
// Parameters:
//   - tenantID: ID of the Aruba Central tenant
//
// Returns:
//   - An error if any step of the authentication flow fails
func (c *Client) AuthenticateAndGetToken(tenantID string) error {
	c.DebugLogger("Starting full authentication flow for tenant: %s", tenantID)

	// Step 1: Login and get CSRF token
	loginResp, err := c.login()
	if err != nil {
		// Error is already handled in login method
		return WrapError(err, "authentication failed")
	}

	if loginResp.CSRFToken == "" || loginResp.Session == "" {
		c.DebugLogger("Login response missing CSRF token or session")
		return NewAuthError("login response missing required tokens", nil)
	}

	c.DebugLogger("Login successful, obtained CSRF token and session")

	// Add a small delay before requesting auth code (to avoid potential timing issues)
	time.Sleep(500 * time.Millisecond)

	// Step 2: Get authorization code
	authCode, err := c.getAuthCode(loginResp, "all", tenantID)
	if err != nil {
		// Error is already handled in getAuthCode method
		return WrapError(err, "authorization failed")
	}

	c.DebugLogger("Authorization code obtained: %s", authCode)

	if authCode == "" {
		return NewEmptyResponseError("authorization code")
	}

	// Add a small delay before exchanging auth code (to avoid potential timing issues)
	time.Sleep(500 * time.Millisecond)

	// Step 3: Exchange auth code for token using oauth2 library
	return c.exchangeAuthCodeWithOAuth2(authCode)
}

// login performs the login step of the authentication flow.
// It exchanges username and password for a CSRF token and session cookie.
//
// Returns:
//   - Login response containing CSRF token and session
//   - An error if login fails
func (c *Client) login() (*LoginResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.getTimeout("auth"))
	defer cancel()

	loginURL := fmt.Sprintf("%s/oauth2/authorize/central/api/login?client_id=%s",
		c.Config.API.Endpoint, c.Config.API.ClientID)
	c.DebugLogger("Login URL: %s", loginURL)

	payload := map[string]string{
		"username": c.Config.API.Username,
		"password": c.Config.API.Password,
	}
	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return nil, WrapError(err, "failed to marshal login payload")
	}

	req, err := http.NewRequestWithContext(ctx, "POST", loginURL,
		bytes.NewBuffer(jsonPayload))
	if err != nil {
		return nil, WrapError(err, "failed to create login request")
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", UserAgent)

	c.DebugLogger("Sending login request:\n%s", c.dumpRequest(req))
	requestStart := time.Now()
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, WrapError(err, "login request failed")
	}
	c.timeTrack(requestStart, "Login HTTP request")
	defer resp.Body.Close()

	c.logResponseRateLimits(resp)

	c.DebugLogger("Received login response:\n%s", c.dumpResponse(resp))

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)

		// Check for rate limiting
		if resp.StatusCode == http.StatusTooManyRequests {
			retryAfter := 0
			retryAfterStr := resp.Header.Get("Retry-After")
			if retryAfterStr != "" {
				retryAfter, _ = strconv.Atoi(retryAfterStr)
			}
			return nil, NewRateLimitError(retryAfter, "login rate limited")
		}

		return nil, NewHTTPError(resp.StatusCode, string(body))
	}

	// Extract CSRF token and session from cookies
	var csrfToken, session string
	for _, cookie := range resp.Cookies() {
		c.DebugLogger("Received cookie: %s=%s", cookie.Name, cookie.Value)
		switch cookie.Name {
		case "csrftoken":
			csrfToken = cookie.Value
		case "session":
			session = cookie.Value
		}
	}

	return &LoginResponse{
		CSRFToken: csrfToken,
		Session:   session,
	}, nil
}

// getAuthCode gets an authorization code using the CSRF token and session.
// This is step 2 of the OAuth2 authentication flow.
//
// Parameters:
//   - loginResp: Login response from the login step
//   - scope: OAuth2 scope to request (usually "all")
//   - tenantID: ID of the Aruba Central tenant
//
// Returns:
//   - Authorization code string
//   - An error if the authorization fails
func (c *Client) getAuthCode(loginResp *LoginResponse, scope, tenantID string) (string, error) {
	c.DebugLogger("Getting auth code")
	ctx, cancel := context.WithTimeout(context.Background(), c.getTimeout("auth"))
	defer cancel()

	authURL := fmt.Sprintf("%s/oauth2/authorize/central/api?client_id=%s&response_type=code&scope=%s",
		c.Config.API.Endpoint, c.Config.API.ClientID, scope)
	c.DebugLogger("Auth URL: %s", authURL)

	payload := map[string]string{
		"customer_id": tenantID,
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return "", WrapError(err, "failed to marshal auth code request payload")
	}

	// print payload
	c.DebugLogger("Payload: %s", string(jsonPayload))

	req, err := http.NewRequestWithContext(ctx, "POST", authURL,
		bytes.NewBuffer(jsonPayload))
	if err != nil {
		return "", WrapError(err, "failed to create auth code request")
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", fmt.Sprintf("session=%s", loginResp.Session))
	req.Header.Set("X-CSRF-Token", loginResp.CSRFToken)
	req.Header.Set("User-Agent", UserAgent)

	c.DebugLogger("Sending auth code request")
	requestStart := time.Now()
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", WrapError(err, "auth code request failed")
	}
	c.timeTrack(requestStart, "Auth code HTTP request")
	defer resp.Body.Close()

	c.logResponseRateLimits(resp)

	c.DebugLogger("Received auth code response with status: %d", resp.StatusCode)

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)

		// Check for rate limiting
		if resp.StatusCode == http.StatusTooManyRequests {
			retryAfter := 0
			retryAfterStr := resp.Header.Get("Retry-After")
			if retryAfterStr != "" {
				retryAfter, _ = strconv.Atoi(retryAfterStr)
			}
			return "", NewRateLimitError(retryAfter, "auth code request rate limited")
		}

		return "", NewHTTPError(resp.StatusCode, string(body))
	}

	var authResp AuthResponse
	if err := json.NewDecoder(resp.Body).Decode(&authResp); err != nil {
		return "", WrapError(err, "failed to decode auth code response")
	}

	if authResp.AuthCode == "" {
		c.DebugLogger("Received empty auth code in response")
		return "", NewEmptyResponseError("auth code")
	}

	c.DebugLogger("Successfully obtained auth code: %s", authResp.AuthCode)
	return authResp.AuthCode, nil
}

// exchangeAuthCodeWithOAuth2 exchanges an auth code for a token.
// This is step 3 of the OAuth2 authentication flow.
//
// Parameters:
//   - authCode: Authorization code from the getAuthCode step
//
// Returns:
//   - An error if the exchange fails
func (c *Client) exchangeAuthCodeWithOAuth2(authCode string) error {
	// Create OAuth2 configuration
	tokenEndpoint := fmt.Sprintf("%s/oauth2/token", c.Config.API.Endpoint)
	c.DebugLogger("Token endpoint for exchange: %s", tokenEndpoint)

	oauthConfig := &oauth2.Config{
		ClientID:     c.Config.API.ClientID,
		ClientSecret: c.Config.API.ClientSecret,
		Endpoint: oauth2.Endpoint{
			TokenURL: tokenEndpoint,
		},
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), c.getTimeout("auth"))
	defer cancel()

	// Exchange authorization code for token
	c.DebugLogger("Exchanging authorization code for token")
	token, err := oauthConfig.Exchange(ctx, authCode)
	if err != nil {
		// Check for rate limiting
		if strings.Contains(err.Error(), "429") {
			retryAfter := ExtractRetryAfter(err.Error())
			c.DebugLogger("Token exchange rate limited, retry after %d seconds", retryAfter)
			return NewRateLimitError(retryAfter, "token exchange rate limited")
		}

		// Log detailed error information
		c.DebugLogger("Token exchange failed with error: %v", err)
		return NewAuthError("failed to exchange authorization code for token", err)
	}

	// Validate the token
	if token.AccessToken == "" {
		return NewEmptyResponseError("access token")
	}

	c.DebugLogger("Successfully obtained new token")
	c.DebugLogger("Token expiry: %s", token.Expiry.Format(time.RFC3339))
	c.DebugLogger("Access token: %s (first 10 chars)", token.AccessToken[:min(10, len(token.AccessToken))]+"...")
	if token.RefreshToken != "" {
		c.DebugLogger("Refresh token: %s (first 10 chars)", token.RefreshToken[:min(10, len(token.RefreshToken))]+"...")
	} else {
		c.DebugLogger("No refresh token received")
	}

	// Update token in config
	c.updateTokenFromOAuth2(token)
	return nil
}

// updateTokenFromOAuth2 updates the token information in the config from an oauth2.Token.
// It also saves the updated config to the config file.
//
// Parameters:
//   - token: OAuth2 token from the token exchange
func (c *Client) updateTokenFromOAuth2(token *oauth2.Token) {
	// Check if the token has actually changed
	tokenChanged := token.AccessToken != c.Config.TokenCache.AccessToken

	if tokenChanged {
		c.DebugLogger("Token has changed, updating all token fields")
		// Update token values
		c.Config.TokenCache.AccessToken = token.AccessToken
		c.Config.TokenCache.TokenType = token.TokenType

		// Set created_at to current time in milliseconds ONLY if token has changed
		c.Config.TokenCache.CreatedAt = time.Now().UnixNano() / int64(time.Millisecond)

		// Calculate expires_in from expiry time
		if !token.Expiry.IsZero() {
			c.Config.TokenCache.ExpiresIn = int(time.Until(token.Expiry).Seconds())
			c.DebugLogger("Token expires in %d seconds", c.Config.TokenCache.ExpiresIn)
		}

		// Set other fields with default values if they're empty
		if c.Config.TokenCache.Scope == "" {
			c.Config.TokenCache.Scope = "all"
		}

		if c.Config.TokenCache.AppName == "" {
			c.Config.TokenCache.AppName = "nms"
		}
	} else {
		c.DebugLogger("Token has not changed, keeping existing creation time")
		// Only update expiry if it's provided and different
		if !token.Expiry.IsZero() {
			newExpiresIn := int(time.Until(token.Expiry).Seconds())
			if newExpiresIn != c.Config.TokenCache.ExpiresIn {
				c.DebugLogger("Updating token expiry from %d to %d seconds",
					c.Config.TokenCache.ExpiresIn, newExpiresIn)
				c.Config.TokenCache.ExpiresIn = newExpiresIn
			}
		}
	}

	// Always update refresh token if it's provided and not empty
	if token.RefreshToken != "" {
		// Check if refresh token has changed
		if token.RefreshToken != c.Config.TokenCache.RefreshToken {
			c.DebugLogger("Updating refresh token: %s... (first 10 chars)",
				token.RefreshToken[:min(10, len(token.RefreshToken))])
			c.Config.TokenCache.RefreshToken = token.RefreshToken
		} else {
			c.DebugLogger("Refresh token unchanged")
		}
	} else {
		c.DebugLogger("No refresh token provided in token, keeping existing refresh token")
	}

	// Save the updated config
	if err := SaveConfig(c.Config, c.ConfigPath); err != nil {
		c.DebugLogger("Failed to save updated token: %v", err)
	} else {
		c.DebugLogger("Token successfully saved to config file")
	}
}
