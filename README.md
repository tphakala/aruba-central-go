<p align="center">
  <img src="aruba-central-go.webp" />
</p>

 <a href="https://goreportcard.com/report/github.com/tphakala/aruba-central-go">
    <img src="https://goreportcard.com/badge/github.com/tphakala/aruba-central-go?style=flat-square">
 </a>
<a href="https://coderabbit.ai">
    <img src="https://img.shields.io/coderabbit/prs/github/tphakala/aruba-central-go?utm_source=oss&utm_medium=github&utm_campaign=tphakala%2Faruba-central-go&labelColor=171717&color=FF570A&link=https%3A%2F%2Fcoderabbit.ai&label=CodeRabbit+Reviews">
</a>


# Aruba Central Go Client

A Go client library and CLI application for interacting with the Aruba Central API.

## Features

- Complete OAuth2 authentication flow implementation
- Automatic token refresh
- Rate limit detection and handling
- Structured error types with proper error handling
- CLI application for easy interaction

## Installation

To install the library and CLI tool, run:

```bash
go get github.com/tphakala/aruba-central-go
```

## Configuration

Create a `config.json` file with your Aruba Central API credentials:

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

## CLI Usage

The package includes a CLI tool called `arubactl` for interacting with Aruba Central.

### Building the CLI

```bash
go build -o arubactl ./cmd/arubactl
```

### Example Commands

List all access points:

```bash
./arubactl --config config.json --tenant your_tenant_id --list
```

Get detailed status for a specific access point:

```bash
./arubactl --config config.json --tenant your_tenant_id --serial AP_SERIAL_NUMBER
```

Enable debug output:

```bash
./arubactl --config config.json --tenant your_tenant_id --serial AP_SERIAL_NUMBER --debug
```

Adjust timeout:

```bash
./arubactl --config config.json --tenant your_tenant_id --list --timeout 120
```

Enable automatic retry when rate limited:

```bash
./arubactl --config config.json --tenant your_tenant_id --list --auto-retry --max-retries 5
```

### Rate Limit Handling

The CLI tool provides built-in rate limit handling with the following features:

- Detailed rate limit error messages showing limits and remaining quota
- Automatic retry capability with the `--auto-retry` flag
- Configurable retry attempts with `--max-retries` (default: 3)
- Automatic waiting for the recommended retry period

## Using the Library in Your Code

### Initializing the Client

```go
import "github.com/tphakala/aruba-central-go/pkg/central"

// Create a new client with debug logging enabled
client, err := central.NewClient("config.json", true)
if err != nil {
    // Handle error
}

// Ensure we have a valid token for the specified tenant
err = client.EnsureValidToken("tenant-id")
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
    // Check for specific error types
    if central.IsRateLimited(err) {
        retryAfter := central.GetRetryAfter(err)
        fmt.Printf("Rate limited. Retry after %d seconds\n", retryAfter)
    } else if central.IsNotFound(err) {
        fmt.Println("AP not found")
    } else {
        fmt.Printf("Error: %v\n", err)
    }
    return
}

// Print AP status
fmt.Printf("AP %s status: %s\n", status.Name, status.Status)
```

### Getting All APs

```go
// Get status for a specific AP and retrieve all APs at once
status, allAPs, err := client.GetAPStatusAndAll(ctx, "AP-SERIAL", "TENANT-ID")
if err != nil {
    // Handle error (see above for error type checking)
}

// Process all APs
fmt.Printf("Retrieved %d access points\n", len(allAPs))
for _, ap := range allAPs {
    // Work with each AP
    fmt.Printf("AP %s: %s (Status: %s)\n", ap.Serial, ap.Name, ap.Status)
}
```

## License

[MIT License](LICENSE) 