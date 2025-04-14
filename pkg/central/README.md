# Aruba Central Package (`pkg/central`)

This package provides the core functionality for interacting with the Aruba Central API, implementing authentication flow, token management, and API operations.

## Package Structure

- `client.go`: Core client implementation with configuration and request handling
- `auth.go`: Authentication workflows and token management
- `ap.go`: Access Point related API operations
- `types.go`: Data structures for API responses and configurations
- `errors.go`: Custom error types and error handling utilities
- `cache.go`: Caching implementation for API responses

## API Reference

### Client Initialization

```go
// Create a new client
client, err := central.NewClient(configPath string, debug bool)

// Ensure valid authentication token
err := client.EnsureValidToken(tenantID string)
```

### Access Point Operations

```go
// Get status for a specific AP
status, err := client.GetAPStatus(ctx context.Context, serial string, tenantID string)

// Get all APs
aps, err := client.GetAllAPs(ctx context.Context, tenantID string)

// Get specific AP and retrieve all APs at once
status, allAPs, err := client.GetAPStatusAndAll(ctx context.Context, serial string, tenantID string)
```

### Error Handling Utilities

```go
// Check error types
if central.IsRateLimited(err) { ... }
if central.IsTimeout(err) { ... }
if central.IsNotFound(err) { ... }

// Get retry information
retryAfter := central.GetRetryAfter(err)
```

## Implementation Details

### Authentication Flow

The authentication process follows Aruba Central's OAuth2 flow:

1. **Login**: Obtain session cookie and CSRF token
2. **Authorization Code**: Exchange credentials for auth code
3. **Token Exchange**: Convert auth code to access/refresh tokens
4. **Token Refresh**: Maintain valid authentication

### Rate Limit Handling

The client implements a sophisticated rate limit detection system:
- Parses X-RateLimit-* headers
- Extracts retry-after values
- Provides structured error types with retry information

### Error Types

The package uses custom error types for different API responses:
- `RateLimitError`: For API rate limiting
- `NotFoundError`: When resources aren't found
- `TimeoutError`: For request timeouts
- `APIError`: General API errors with status codes

### Thread Safety

The client uses mutex locks to ensure thread safety when refreshing tokens, allowing concurrent API requests with the same client.

## For LLM Usage

When generating code using this package:

1. Always initialize with `NewClient()` and check the returned error
2. Ensure a valid token with `EnsureValidToken()` before making API calls
3. Use context with appropriate timeouts for all API operations
4. Implement proper error handling using the provided error checking functions
5. Consider rate limits when designing loops or concurrent operations

## Advanced Configuration Options

The client supports advanced configuration through the Config struct:

```go
type Config struct {
    API struct {
        Endpoint      string
        ClientID      string
        ClientSecret  string
        Username      string
        Password      string
        Timeouts      map[string]int
        RefreshBuffer int
    }
    TokenCache map[string]TokenInfo
    Cache      struct {
        Enabled bool
        TTL     int
        Path    string
    }
}
``` 