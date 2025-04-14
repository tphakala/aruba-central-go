# Aruba Central API Client for Go

A Go package for interacting with the Aruba Central API. This package provides a complete client implementation with authentication, token management, and API operations for managing and monitoring Aruba devices.

## Overview

This package offers a reusable client for Aruba Central's REST API with the following features:

- Complete OAuth2 authentication flow implementation
- Automatic token refresh
- Rate limit detection and handling
- Configurable timeouts
- Structured API response types
- Detailed debug logging

## Usage

### Creating a Client

```go
import "github.com/tphakala/aruba-central-monitoring/internal/arubacentral"

// Create a new client with debug logging enabled
client, err := arubacentral.NewClient("config.json", true)
if err != nil {
    // Handle error
}
```

### Authentication

The client handles all aspects of authentication:

```go
// Ensure we have a valid token for the specified tenant
err := client.EnsureValidToken("tenant-id")
if err != nil {
    // Handle authentication error
}
```

### Getting AP Status

```go
import "context"

// Create a context with timeout
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

// Get status for a specific AP
status, err := client.GetAPStatus(ctx, "AP-SERIAL", "TENANT-ID")
if err != nil {
    // Handle error
}

// Print AP status
fmt.Printf("AP %s status: %s\n", status.Name, status.Status)
```

### Getting All APs

```go
// Get status for a specific AP and retrieve all APs at once
status, allAPs, err := client.GetAPStatusAndAll(ctx, "AP-SERIAL", "TENANT-ID")
if err != nil {
    // Handle error
}

// Process all APs
fmt.Printf("Retrieved %d access points\n", len(allAPs))
for _, ap := range allAPs {
    // Work with each AP
    fmt.Printf("AP %s: %s (Status: %s)\n", ap.Serial, ap.Name, ap.Status)
}
```

## Configuration

The client requires a JSON configuration file with the following structure:

```json
{
  "api": {
    "endpoint": "https://apigw-prod2.central.arubanetworks.com",
    "client_id": "your_client_id",
    "client_secret": "your_client_secret",
    "username": "your_username",
    "password": "your_password",
    "timeouts": {
      "default": 60,
      "auth": 30,
      "status": 60
    },
    "refresh_buffer": 900
  },
  "token_cache": {},
  "cache": {
    "enabled": true,
    "ttl": 300,
    "path": "ap_cache.bolt"
  }
}
```

## Authentication Flow

The client implements Aruba Central's complete authentication flow:

1. **Login**: 
   - POST username/password to obtain session cookie and CSRF token
   - Endpoint: `/oauth2/authorize/central/api/login`

2. **Authorization Code**:
   - POST with session cookie and CSRF token to get authorization code
   - Endpoint: `/oauth2/authorize/central/api`

3. **Token Exchange**:
   - Exchange authorization code for access/refresh tokens
   - Endpoint: `/oauth2/token`

4. **Token Refresh**:
   - Refresh tokens when they're about to expire
   - Falls back to full authentication if refresh fails

## Rate Limiting

The client detects and handles API rate limits:

- Parses rate limit headers (X-RateLimit-*)
- Extracts retry information from responses
- Provides detailed error messages with retry advice

## Error Handling

The client uses structured error handling with custom error types:

```go
if err != nil {
    // Check if this is a rate limiting error
    if arubacentral.IsRateLimited(err) {
        retryAfter := arubacentral.GetRetryAfter(err)
        fmt.Printf("Rate limited. Retry after %d seconds\n", retryAfter)
        time.Sleep(time.Duration(retryAfter) * time.Second)
        // Retry request...
    }

    // Check for timeout errors
    if arubacentral.IsTimeout(err) {
        fmt.Println("Request timed out, try with a longer timeout")
    }

    // Check for not found errors
    if arubacentral.IsNotFound(err) {
        fmt.Println("Resource not found")
    }

    // General error handling
    fmt.Printf("Error: %v\n", err)
}
```

The package provides these error checking functions:

- `IsRateLimited(err)`: Check if the error is due to rate limiting
- `IsTimeout(err)`: Check if the error is due to a timeout
- `IsNotFound(err)`: Check if the error is due to a resource not being found
- `GetRetryAfter(err)`: Extract the retry-after value from a rate limit error

These functions work with the standard Go error interfaces, so they're compatible with regular error handling patterns.

## Debug Logging

When debugging is enabled, the client provides detailed logs:

- HTTP request/response details (with sensitive data redacted)
- Authentication events
- Token management
- Rate limit information
- Timing metrics

## Types

The package provides Go types for Aruba Central API responses:

- `Config`: Client configuration
- `APStatus`: Access point status
- `Radio`: Radio status
- `APIResponse`: Full API response

## Thread Safety

The client uses mutex locks to ensure thread safety when refreshing tokens. 