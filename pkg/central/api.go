package arubacentral

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Client represents an Aruba Central API client with connection settings and utilities.
// It handles authentication, API requests, error handling, and debugging.
type Client struct {
	Config      *Config                       // API configuration including endpoints and credentials
	HTTPClient  *http.Client                  // HTTP client used for making API requests
	Debug       bool                          // Whether debug logging is enabled
	ConfigPath  string                        // Path to the configuration file
	DebugLogger func(format string, v ...any) // Function for logging debug information
}

// NewClient creates a new Aruba Central API client with the specified configuration.
// It reads the configuration from the provided file path and sets up the client.
//
// Parameters:
//   - configPath: Path to the JSON configuration file
//   - debug: Whether to enable debug logging
//
// Returns:
//   - A configured Client instance
//   - An error if the configuration cannot be read or is invalid
func NewClient(configPath string, debug bool) (*Client, error) {
	config, err := ReadConfig(configPath)
	if err != nil {
		return nil, NewConfigError(err, "failed to read config")
	}

	client := &Client{
		Config:     config,
		HTTPClient: &http.Client{},
		Debug:      debug,
		ConfigPath: configPath,
	}

	// Set up debug logger
	if debug {
		client.DebugLogger = func(format string, v ...any) {
			log.Printf("[DEBUG] "+format, v...)
		}
	} else {
		client.DebugLogger = func(format string, v ...any) {}
	}

	return client, nil
}

// GetAPStatus fetches the status of a specific access point by serial number.
// This is a convenience wrapper around GetAPStatusAndAll that returns only the specific AP.
//
// Parameters:
//   - ctx: Context for the request, which can include timeout and cancellation
//   - serial: Serial number of the access point to get status for
//   - tenantID: ID of the Aruba Central tenant
//
// Returns:
//   - The status of the requested access point
//   - An error if the request fails or the AP is not found
func (c *Client) GetAPStatus(ctx context.Context, serial, tenantID string) (*APStatus, error) {
	status, _, err := c.GetAPStatusAndAll(ctx, serial, tenantID)
	return status, err
}

// GetAPStatusAndAll fetches AP status from the API and returns both the requested AP and all APs.
// This is useful for efficiently retrieving a specific AP while also getting the full AP list.
//
// Parameters:
//   - ctx: Context for the request, which can include timeout and cancellation
//   - serial: Serial number of the access point to get status for
//   - tenantID: ID of the Aruba Central tenant
//
// Returns:
//   - The status of the requested access point
//   - A list of all access points from the API response
//   - An error if the request fails or the AP is not found
func (c *Client) GetAPStatusAndAll(ctx context.Context, serial, tenantID string) (ap *APStatus, allAPs []APStatus, err error) {
	// Set timeout from config if not already set in context
	if _, ok := ctx.Deadline(); !ok {
		timeout := c.getTimeout("status")
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	url := fmt.Sprintf("%s/monitoring/v2/aps", c.Config.API.Endpoint)
	c.DebugLogger("AP Status URL: %s", url)

	req, err := http.NewRequestWithContext(ctx, "GET", url, http.NoBody)
	if err != nil {
		return nil, nil, WrapError(err, "failed to create request")
	}

	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("Authorization", "Bearer "+c.Config.TokenCache.AccessToken)
	req.Header.Add("TenantID", tenantID)
	req.Header.Add("User-Agent", UserAgent)

	c.DebugLogger("Sending AP status request:\n%s", c.dumpRequest(req))
	requestStart := time.Now()
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		if os.IsTimeout(err) || errors.Is(err, context.DeadlineExceeded) {
			c.DebugLogger("AP status request timed out after %s", time.Since(requestStart))
			return nil, nil, NewTimeoutError(time.Since(requestStart))
		}
		c.DebugLogger("AP status request failed after %s: %v", time.Since(requestStart), err)
		return nil, nil, WrapError(err, "request failed")
	}
	c.timeTrack(requestStart, "AP status HTTP request")
	defer resp.Body.Close()

	// Log rate limiting headers
	c.logResponseRateLimits(resp)

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)

		// Check for rate limiting response
		if resp.StatusCode == http.StatusTooManyRequests {
			// Build a detailed rate limit error message
			var rateLimitMsg strings.Builder
			rateLimitMsg.WriteString("API rate limit exceeded")

			// Add retry-after if available
			retryAfterStr := resp.Header.Get("Retry-After")
			retryAfter := 0
			if retryAfterStr != "" {
				retryAfter, _ = strconv.Atoi(retryAfterStr)
				rateLimitMsg.WriteString(fmt.Sprintf(" - retry after %s seconds", retryAfterStr))
			}

			// Check for daily rate limits
			limitDay := resp.Header.Get("X-RateLimit-Limit-day")
			remainingDay := resp.Header.Get("X-RateLimit-Remaining-day")
			limitDayInt, limitDayErr := strconv.Atoi(limitDay)
			remainingDayInt, remainingDayErr := strconv.Atoi(remainingDay)

			// Check for per-second rate limits
			limitSecond := resp.Header.Get("X-RateLimit-Limit-second")
			remainingSecond := resp.Header.Get("X-RateLimit-Remaining-second")
			limitSecondInt, limitSecondErr := strconv.Atoi(limitSecond)
			remainingSecondInt, remainingSecondErr := strconv.Atoi(remainingSecond)

			// Add rate limit details to message
			if limitDay != "" && remainingDay != "" {
				rateLimitMsg.WriteString(fmt.Sprintf(" (%s/%s daily requests remaining)", remainingDay, limitDay))
			} else if limitSecond != "" && remainingSecond != "" {
				rateLimitMsg.WriteString(fmt.Sprintf(" (%s/%s per-second requests remaining)", remainingSecond, limitSecond))
			}

			// Add rate limit details from body if available
			var rateLimitResp struct {
				Error      string `json:"error"`
				RetryAfter int    `json:"retry_after"`
			}

			if err := json.Unmarshal(body, &rateLimitResp); err == nil && rateLimitResp.RetryAfter > 0 {
				// If retry-after wasn't provided in headers, use the one from the body
				if retryAfter == 0 {
					retryAfter = rateLimitResp.RetryAfter
				}
			}

			// Create the appropriate rate limit error type
			switch {
			case limitDay != "" && remainingDay != "" && limitDayErr == nil && remainingDayErr == nil:
				return nil, nil, NewDailyRateLimitError(retryAfter, limitDayInt, remainingDayInt, "%s", rateLimitMsg.String())
			case limitSecond != "" && remainingSecond != "" && limitSecondErr == nil && remainingSecondErr == nil:
				return nil, nil, NewSecondRateLimitError(retryAfter, limitSecondInt, remainingSecondInt, "%s", rateLimitMsg.String())
			default:
				return nil, nil, NewRateLimitError(retryAfter, "%s", rateLimitMsg.String())
			}
		}

		return nil, nil, NewHTTPError(resp.StatusCode, string(body))
	}

	decodeStart := time.Now()
	var apiResp APIResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, nil, WrapError(err, "failed to decode API response")
	}
	c.timeTrack(decodeStart, "Response decode")

	c.DebugLogger("Received data for %d APs", apiResp.Count)

	// Find the AP with matching serial number
	var requestedAP *APStatus
	for i := range apiResp.APs {
		if apiResp.APs[i].Serial == serial {
			c.DebugLogger("Found matching AP: %s (Status: %s)", apiResp.APs[i].Name, apiResp.APs[i].Status)
			requestedAP = &apiResp.APs[i]
			break
		}
	}

	if requestedAP == nil {
		return nil, apiResp.APs, NewNotFoundError("AP", fmt.Sprintf("serial %s", serial))
	}

	return requestedAP, apiResp.APs, nil
}

// getTimeout returns the appropriate timeout duration based on the request type.
// It falls back to default values if not configured or zero.
//
// Parameters:
//   - timeoutType: Type of timeout to get ("auth", "status", or any other for default)
//
// Returns:
//   - Timeout duration for the specified type
func (c *Client) getTimeout(timeoutType string) time.Duration {
	var seconds int
	switch timeoutType {
	case "auth":
		seconds = c.Config.API.Timeouts.Auth
	case "status":
		seconds = c.Config.API.Timeouts.Status
	default:
		seconds = c.Config.API.Timeouts.Default
	}

	// If no timeout configured, use a sensible default
	if seconds <= 0 {
		seconds = 60 // Default 60 seconds
	}

	return time.Duration(seconds) * time.Second
}

// timeTrack logs the time taken for an operation if debug mode is enabled.
// This is used for performance tracking and troubleshooting.
//
// Parameters:
//   - start: Start time of the operation
//   - name: Name of the operation to track
func (c *Client) timeTrack(start time.Time, name string) {
	if c.Debug {
		elapsed := time.Since(start)
		c.DebugLogger("%s took %s", name, elapsed)
	}
}

// dumpRequest dumps the HTTP request for debugging, with sensitive data redacted.
// Returns an empty string if debug mode is disabled.
//
// Parameters:
//   - req: HTTP request to dump
//
// Returns:
//   - String representation of the request with sensitive data redacted
func (c *Client) dumpRequest(req *http.Request) string {
	if !c.Debug {
		return ""
	}
	dump, err := httputil.DumpRequestOut(req, true)
	if err != nil {
		return fmt.Sprintf("Error dumping request: %v", err)
	}

	// Redact sensitive information in the request body
	return c.redactSensitiveInfo(string(dump))
}

// dumpResponse dumps the HTTP response for debugging, with sensitive data redacted.
// Returns an empty string if debug mode is disabled.
//
// Parameters:
//   - resp: HTTP response to dump
//
// Returns:
//   - String representation of the response with sensitive data redacted
func (c *Client) dumpResponse(resp *http.Response) string {
	if !c.Debug {
		return ""
	}

	// Print rate limiting headers first if present
	var rateLimitInfo strings.Builder
	rateLimitHeaders := []string{
		"X-RateLimit-Limit-day",
		"X-RateLimit-Remaining-day",
		"X-RateLimit-Limit-second",
		"X-RateLimit-Remaining-second",
		"X-Request-Start",
		"Date",
	}

	if c.hasRateLimitHeaders(resp.Header, rateLimitHeaders) {
		rateLimitInfo.WriteString("\n=== Rate Limit Information ===\n")
		for _, header := range rateLimitHeaders {
			if value := resp.Header.Get(header); value != "" {
				rateLimitInfo.WriteString(fmt.Sprintf("%s: %s\n", header, value))
			}
		}
		rateLimitInfo.WriteString("==============================\n")
	}

	// Then dump the full response
	dump, err := httputil.DumpResponse(resp, true)
	if err != nil {
		return fmt.Sprintf("Error dumping response: %v", err)
	}

	// Redact sensitive information in the response body
	return rateLimitInfo.String() + c.redactSensitiveInfo(string(dump))
}

// hasRateLimitHeaders checks if any of the specified rate limit headers are present.
//
// Parameters:
//   - header: HTTP headers to check
//   - rateLimitHeaders: List of header names to look for
//
// Returns:
//   - true if any of the headers are present, false otherwise
func (c *Client) hasRateLimitHeaders(header http.Header, rateLimitHeaders []string) bool {
	for _, h := range rateLimitHeaders {
		if header.Get(h) != "" {
			return true
		}
	}
	return false
}

// redactSensitiveInfo redacts sensitive information like passwords from strings.
//
// Parameters:
//   - input: String to redact sensitive information from
//
// Returns:
//   - String with sensitive information redacted
func (c *Client) redactSensitiveInfo(input string) string {
	// Redact JSON password fields
	passwordRe := regexp.MustCompile(`"password"\s*:\s*"[^"]*"`)
	return passwordRe.ReplaceAllString(input, `"password":"[REDACTED]"`)
}

// logResponseRateLimits logs rate limiting headers from an HTTP response.
// This helps with debugging rate limiting issues.
//
// Parameters:
//   - resp: HTTP response containing potential rate limit headers
func (c *Client) logResponseRateLimits(resp *http.Response) {
	if !c.Debug {
		return
	}

	rateLimitHeaders := []string{
		"X-RateLimit-Limit-day",
		"X-RateLimit-Remaining-day",
		"X-RateLimit-Limit-second",
		"X-RateLimit-Remaining-second",
		"X-Request-Start",
		"Date",
	}

	var hasHeaders bool
	var sb strings.Builder
	sb.WriteString("\n=== Rate Limit Information ===\n")

	for _, header := range rateLimitHeaders {
		if value := resp.Header.Get(header); value != "" {
			hasHeaders = true
			sb.WriteString(fmt.Sprintf("%s: %s\n", header, value))
		}
	}

	if hasHeaders {
		sb.WriteString("==============================")
		c.DebugLogger(sb.String())
	}
}

// ExtractRetryAfter attempts to extract the retry-after value from error messages.
// It tries multiple regex patterns to find retry time information.
//
// Parameters:
//   - errMsg: Error message to extract retry-after value from
//
// Returns:
//   - The retry-after value in seconds, or 0 if not found
func ExtractRetryAfter(errMsg string) int {
	// Try to find patterns like "retry after X seconds" or "X-Ratelimit-Reset-Second: X"
	patterns := []string{
		`retry[-_\s]+after[-_\s]*:?\s*(\d+)`, // Handles "retry-after: 30", "retry after 30", etc.
		`Retry-After:?\s*(\d+)`,
		`Reset-Second:?\s*(\d+)`,
		`after\s+(\d+)\s+seconds`,
		`(\d+)\s+seconds`,
	}

	for _, pattern := range patterns {
		re := regexp.MustCompile(`(?i)` + pattern) // Case insensitive
		matches := re.FindStringSubmatch(errMsg)
		if len(matches) > 1 {
			if seconds, err := strconv.Atoi(matches[1]); err == nil {
				return seconds
			}
		}
	}
	return 0
}
