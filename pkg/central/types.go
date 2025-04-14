// Package arubacentral provides a client implementation for the Aruba Central API.
//
// It handles authentication, token management, error handling, and API operations
// for managing and monitoring Aruba devices in the Aruba Central cloud management platform.
package arubacentral

// Config represents the configuration for the Aruba Central API client.
// It contains API credentials, endpoint configuration, timeouts, and token cache settings.
type Config struct {
	API struct {
		Endpoint     string `json:"endpoint"`      // Base URL for the API (e.g., "https://apigw-prod2.central.arubanetworks.com")
		ClientID     string `json:"client_id"`     // OAuth2 client ID
		ClientSecret string `json:"client_secret"` // OAuth2 client secret
		Username     string `json:"username"`      // Username for authentication
		Password     string `json:"password"`      // Password for authentication
		Timeouts     struct {
			Default int `json:"default"` // Default timeout in seconds for API requests
			Auth    int `json:"auth"`    // Timeout in seconds for authentication requests
			Status  int `json:"status"`  // Timeout in seconds for status requests
		} `json:"timeouts"`
		RefreshBuffer int `json:"refresh_buffer"` // Token refresh buffer in seconds, default is 15 minutes (900 seconds)
	} `json:"api"`
	TokenCache struct {
		AccessToken       string `json:"access_token"`         // OAuth2 access token
		AppName           string `json:"appname"`              // Application name
		AuthenticatedUser string `json:"authenticated_userid"` // User ID that was authenticated
		CreatedAt         int64  `json:"created_at"`           // Timestamp when the token was created (in milliseconds)
		CredentialID      string `json:"credential_id"`        // Credential ID
		ExpiresIn         int    `json:"expires_in"`           // Token expiration time in seconds
		ID                string `json:"id"`                   // Token ID
		RefreshToken      string `json:"refresh_token"`        // OAuth2 refresh token
		Scope             string `json:"scope"`                // OAuth2 scope
		TokenType         string `json:"token_type"`           // OAuth2 token type (usually "Bearer")
	} `json:"token_cache"`
	Cache struct {
		Enabled bool   `json:"enabled"` // Whether to enable API response caching
		TTL     int64  `json:"ttl"`     // Cache TTL in seconds
		Path    string `json:"path"`    // Path to the cache file
	} `json:"cache"`
}

// LoginResponse represents the login response from the Aruba Central API.
// It contains the CSRF token and session information needed for subsequent requests.
type LoginResponse struct {
	CSRFToken string // CSRF token for protecting against cross-site request forgery
	Session   string // Session cookie value
}

// AuthResponse represents the authorization response during the OAuth2 flow.
// It contains the authorization code needed to exchange for an access token.
type AuthResponse struct {
	AuthCode string `json:"auth_code"` // OAuth2 authorization code
}

// Radio represents the radio status of an access point.
// It contains details about the wireless radio such as band, type, and status.
type Radio struct {
	Band          int    `json:"band"`           // Frequency band (e.g., 2.4GHz = 2, 5GHz = 5)
	Index         int    `json:"index"`          // Radio index
	MacAddr       string `json:"macaddr"`        // MAC address of the radio
	RadioName     string `json:"radio_name"`     // Name of the radio
	RadioType     string `json:"radio_type"`     // Type of the radio (e.g., "a" for 5GHz, "g" for 2.4GHz)
	SpatialStream string `json:"spatial_stream"` // Number of spatial streams
	Status        string `json:"status"`         // Status of the radio (e.g., "Up", "Down")
}

// APStatus represents the access point status response from the Aruba Central API.
// It contains comprehensive details about the access point's current state.
type APStatus struct {
	APDeploymentMode string  `json:"ap_deployment_mode"`     // Deployment mode (e.g., "IAP")
	APGroup          string  `json:"ap_group"`               // AP group name
	FirmwareVersion  string  `json:"firmware_version"`       // Current firmware version
	GroupName        string  `json:"group_name"`             // Group name
	IPAddress        string  `json:"ip_address"`             // IP address of the AP
	MacAddr          string  `json:"macaddr"`                // MAC address of the AP
	Model            string  `json:"model"`                  // AP hardware model
	Name             string  `json:"name"`                   // Name of the AP
	PublicIPAddress  string  `json:"public_ip_address"`      // Public IP address (if applicable)
	Radios           []Radio `json:"radios"`                 // Radio information
	Serial           string  `json:"serial"`                 // Serial number of the AP
	Site             string  `json:"site"`                   // Site name
	Status           string  `json:"status"`                 // Status of the AP (e.g., "Up", "Down")
	SwarmName        string  `json:"swarm_name"`             // Swarm name (for Instant APs)
	LastUpdated      int64   `json:"last_updated,omitempty"` // Timestamp when the status was last updated (in seconds since epoch)
}

// CacheEntry represents a cached AP status with metadata for time-based cache management.
type CacheEntry struct {
	APStatus    *APStatus `json:"ap_status"`    // Cached AP status
	LastUpdated int64     `json:"last_updated"` // Timestamp when the entry was last updated (in seconds since epoch)
}

// TenantCache represents the cache for a specific tenant in a multi-tenant environment.
type TenantCache struct {
	Entries     map[string]CacheEntry `json:"entries"`      // Map of serial number to CacheEntry
	LastUpdated int64                 `json:"last_updated"` // Timestamp when the tenant cache was last updated (in seconds since epoch)
}

// Cache represents the structure of the cache file for storing AP status information.
type Cache struct {
	Tenants map[string]TenantCache `json:"tenants"` // Map of tenant ID to TenantCache
	TTL     int64                  `json:"ttl"`     // TTL in seconds for cache entries
	Stats   CacheStats             `json:"stats"`   // Cache usage statistics
}

// CacheStats represents cache usage statistics for monitoring cache effectiveness.
type CacheStats struct {
	Hits         int64 `json:"hits"`          // Number of cache hits
	Misses       int64 `json:"misses"`        // Number of cache misses
	APsTotal     int   `json:"aps_total"`     // Total number of APs in the cache
	TenantsTotal int   `json:"tenants_total"` // Total number of tenants in the cache
	LastUpdated  int64 `json:"last_updated"`  // Timestamp when the stats were last updated (in seconds since epoch)
}

// APIResponse represents the full API response when requesting AP status information.
type APIResponse struct {
	APs   []APStatus `json:"aps"`   // List of AP status objects
	Count int        `json:"count"` // Total count of APs returned
}
