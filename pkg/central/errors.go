// Error-related functionality for the Aruba Central API client.
// This file provides structured error handling and error identification tools.
package arubacentral

import (
	"errors"
	"fmt"
	"net/http"
	"time"
)

// Common error types for easier error checking with errors.Is()
var (
	// ErrNotFound indicates the requested resource was not found.
	ErrNotFound = errors.New("resource not found")

	// ErrEmptyResponse indicates an empty response was received when data was expected.
	ErrEmptyResponse = errors.New("empty response received")

	// ErrInvalidAuth indicates an authentication problem occurred.
	ErrInvalidAuth = errors.New("invalid authentication")

	// ErrConfigError indicates a configuration error was encountered.
	ErrConfigError = errors.New("configuration error")

	// ErrRateLimit indicates a rate limit was reached.
	// Aruba Central implements rate limiting based on:
	// - Per-second request limits (typically 600 requests/second)
	// - Daily API call limits (typically 6000 requests/day)
	// See: https://developer.arubanetworks.com/hpe-aruba-networking-central/docs/usage-and-rate-limits
	ErrRateLimit = errors.New("rate limit exceeded")

	// ErrDailyRateLimit indicates the daily API call limit was reached.
	// Aruba Central typically allows 6000 API calls per day per access token.
	// This limit is shared across applications using the same client_id.
	ErrDailyRateLimit = errors.New("daily rate limit exceeded")

	// ErrSecondRateLimit indicates the per-second API call limit was reached.
	// Aruba Central typically allows 600 API calls per second per access token.
	// This is to prevent excessive load on the platform.
	ErrSecondRateLimit = errors.New("per-second rate limit exceeded")
)

// Error represents a structured API error with additional metadata.
// It implements the error interface while providing richer context.
type Error struct {
	// Original is the original error (may be nil)
	Original error

	// Code is the HTTP status code (if applicable)
	Code int

	// Message is the human-readable error message
	Message string

	// RetryAfter suggests when to retry the request (for rate limiting errors)
	RetryAfter int

	// IsRateLimit indicates if this is a rate limit error
	IsRateLimit bool

	// IsTimeout indicates if this is a timeout error
	IsTimeout bool

	// RateLimitType indicates the type of rate limit (daily or per-second)
	RateLimitType string

	// RateLimitStats contains rate limit statistics if available
	RateLimitStats *RateLimitStats
}

// RateLimitStats holds statistics about rate limiting from API headers
type RateLimitStats struct {
	// DailyLimit is the total number of API calls allowed per day
	DailyLimit int

	// DailyRemaining is the number of API calls remaining for the day
	DailyRemaining int

	// SecondLimit is the number of API calls allowed per second
	SecondLimit int

	// SecondRemaining is the number of API calls remaining for this second
	SecondRemaining int

	// ResetTime is when the rate limit will reset (if provided)
	ResetTime time.Time
}

// Error returns the formatted error message, implementing the error interface.
// It includes additional context like rate limiting or HTTP status codes when available.
func (e *Error) Error() string {
	if e.IsRateLimit && e.RetryAfter > 0 {
		return fmt.Sprintf("%s (rate limited, retry after %d seconds)", e.Message, e.RetryAfter)
	}
	if e.IsRateLimit && e.RateLimitStats != nil {
		if e.RateLimitType == "daily" {
			return fmt.Sprintf("%s (daily limit: %d, remaining: %d)",
				e.Message, e.RateLimitStats.DailyLimit, e.RateLimitStats.DailyRemaining)
		}
		if e.RateLimitType == "second" {
			return fmt.Sprintf("%s (per-second limit: %d, remaining: %d)",
				e.Message, e.RateLimitStats.SecondLimit, e.RateLimitStats.SecondRemaining)
		}
	}
	if e.Code > 0 {
		return fmt.Sprintf("%s (HTTP %d)", e.Message, e.Code)
	}
	return e.Message
}

// Unwrap returns the original error, implementing the unwrap interface.
// This allows using errors.Is and errors.As with wrapped errors.
func (e *Error) Unwrap() error {
	return e.Original
}

// NewRateLimitError creates a new rate limit error with the specified retry time.
// It automatically sets the HTTP status code to 429 Too Many Requests.
//
// This is used when Aruba Central returns a 429 status code but doesn't specify
// which type of rate limit was exceeded.
//
// Parameters:
//   - retryAfter: Seconds to wait before retrying the request
//   - msg: Error message format string
//   - args: Arguments for the format string
//
// Returns:
//   - A formatted Error with rate limit information
func NewRateLimitError(retryAfter int, msg string, args ...interface{}) *Error {
	return &Error{
		Original:    ErrRateLimit,
		Message:     fmt.Sprintf(msg, args...),
		Code:        http.StatusTooManyRequests,
		RetryAfter:  retryAfter,
		IsRateLimit: true,
	}
}

// NewDailyRateLimitError creates a new rate limit error for daily API call limits.
// It includes daily rate limit statistics from the Aruba Central API.
//
// Aruba Central typically limits API calls to 6000 per day per access token.
// When this limit is reached, requests will be rejected with a 429 status code
// and X-RateLimit-* headers containing the limit details.
//
// Parameters:
//   - retryAfter: Seconds to wait before retrying the request
//   - dailyLimit: Maximum number of requests allowed per day
//   - dailyRemaining: Remaining requests for the current day
//   - msg: Error message format string
//   - args: Arguments for the format string
//
// Returns:
//   - A formatted Error with daily rate limit information
func NewDailyRateLimitError(retryAfter, dailyLimit, dailyRemaining int, msg string, args ...interface{}) *Error {
	return &Error{
		Original:      ErrDailyRateLimit,
		Message:       fmt.Sprintf(msg, args...),
		Code:          http.StatusTooManyRequests,
		RetryAfter:    retryAfter,
		IsRateLimit:   true,
		RateLimitType: "daily",
		RateLimitStats: &RateLimitStats{
			DailyLimit:     dailyLimit,
			DailyRemaining: dailyRemaining,
		},
	}
}

// NewSecondRateLimitError creates a new rate limit error for per-second API call limits.
// It includes per-second rate limit statistics from the Aruba Central API.
//
// Aruba Central typically limits API calls to 600 per second per access token.
// When this limit is reached, requests will be rejected with a 429 status code
// and X-RateLimit-* headers containing the limit details.
//
// Parameters:
//   - retryAfter: Seconds to wait before retrying the request
//   - secondLimit: Maximum number of requests allowed per second
//   - secondRemaining: Remaining requests for the current second
//   - msg: Error message format string
//   - args: Arguments for the format string
//
// Returns:
//   - A formatted Error with per-second rate limit information
func NewSecondRateLimitError(retryAfter, secondLimit, secondRemaining int, msg string, args ...interface{}) *Error {
	return &Error{
		Original:      ErrSecondRateLimit,
		Message:       fmt.Sprintf(msg, args...),
		Code:          http.StatusTooManyRequests,
		RetryAfter:    retryAfter,
		IsRateLimit:   true,
		RateLimitType: "second",
		RateLimitStats: &RateLimitStats{
			SecondLimit:     secondLimit,
			SecondRemaining: secondRemaining,
		},
	}
}

// NewTimeoutError creates a new timeout error that includes the duration of the timeout.
// This is used when a request exceeds its allowed execution time.
func NewTimeoutError(duration time.Duration) *Error {
	return &Error{
		Message:   fmt.Sprintf("request timed out after %s", duration),
		IsTimeout: true,
	}
}

// NewNotFoundError creates a new error for a resource that was not found.
// It includes the resource type and identifier for precise error reporting.
func NewNotFoundError(resourceType, identifier string) *Error {
	return &Error{
		Original: ErrNotFound,
		Message:  fmt.Sprintf("%s with %s not found", resourceType, identifier),
		Code:     http.StatusNotFound,
	}
}

// NewEmptyResponseError creates a new error for when an empty response is received.
// It specifies what type of response data was expected but missing.
func NewEmptyResponseError(responseType string) *Error {
	return &Error{
		Original: ErrEmptyResponse,
		Message:  fmt.Sprintf("received empty %s in response", responseType),
	}
}

// NewAuthError creates a new authentication error with an optional underlying error.
// It automatically sets the HTTP status code to 401 Unauthorized.
func NewAuthError(msg string, err error) *Error {
	return &Error{
		Original: errors.Join(ErrInvalidAuth, err),
		Message:  msg,
		Code:     http.StatusUnauthorized,
	}
}

// NewHTTPError creates a new error for failed HTTP requests.
// It includes the status code and response body in the error message.
func NewHTTPError(code int, body string) *Error {
	return &Error{
		Message: fmt.Sprintf("HTTP request failed with status %d: %s", code, body),
		Code:    code,
	}
}

// NewConfigError creates a new configuration error with an underlying cause.
// This is used for problems with the client configuration.
func NewConfigError(err error, msg string) *Error {
	return &Error{
		Original: errors.Join(ErrConfigError, err),
		Message:  msg,
	}
}

// IsNotFound checks if the error indicates a resource was not found.
// It uses errors.Is to check if the error or any wrapped error is an ErrNotFound.
func IsNotFound(err error) bool {
	return errors.Is(err, ErrNotFound)
}

// IsRateLimited checks if the error indicates a rate limit was reached.
// It uses errors.As to check if the error or any wrapped error is a rate limit Error.
func IsRateLimited(err error) bool {
	var apiErr *Error
	if errors.As(err, &apiErr) {
		return apiErr.IsRateLimit
	}
	return false
}

// IsTimeout checks if the error indicates a request timeout occurred.
// It uses errors.As to check if the error or any wrapped error is a timeout Error.
func IsTimeout(err error) bool {
	var apiErr *Error
	if errors.As(err, &apiErr) {
		return apiErr.IsTimeout
	}
	return false
}

// GetRetryAfter extracts the retry-after value from an error, if available.
// It returns 0 if the error is not a rate limit error or doesn't specify a retry time.
func GetRetryAfter(err error) int {
	var apiErr *Error
	if errors.As(err, &apiErr) && apiErr.IsRateLimit {
		return apiErr.RetryAfter
	}
	return 0
}

// WrapError wraps an error with additional context message.
// If the error is already an Error, it preserves the error type and metadata.
func WrapError(err error, msg string) error {
	if err == nil {
		return nil
	}

	var apiErr *Error
	if errors.As(err, &apiErr) {
		// If it's already a Central API error, just update the message
		return &Error{
			Original:    apiErr.Original,
			Code:        apiErr.Code,
			Message:     fmt.Sprintf("%s: %s", msg, apiErr.Message),
			RetryAfter:  apiErr.RetryAfter,
			IsRateLimit: apiErr.IsRateLimit,
			IsTimeout:   apiErr.IsTimeout,
		}
	}

	// Otherwise, wrap the error
	return &Error{
		Original: err,
		Message:  fmt.Sprintf("%s: %v", msg, err),
	}
}
