package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	arubacentral "github.com/tphakala/aruba-central-go/pkg/central"
)

func main() {
	os.Exit(run())
}

func run() int {
	// Define command line flags
	configPath := flag.String("config", "config.json", "Path to the config file")
	debug := flag.Bool("debug", false, "Enable debug output")
	tenantID := flag.String("tenant", "", "Tenant ID to use")
	serialNumber := flag.String("serial", "", "AP serial number for status queries")
	listAPs := flag.Bool("list", false, "List all APs")
	timeout := flag.Int("timeout", 60, "Timeout in seconds for API calls")
	autoRetry := flag.Bool("auto-retry", false, "Automatically retry requests when rate limited")
	maxRetries := flag.Int("max-retries", 3, "Maximum number of retries when rate limited")
	flag.Parse()

	// Validate required parameters
	if *tenantID == "" {
		fmt.Println("Error: tenant ID is required")
		flag.Usage()
		return 1
	}

	// Initialize the client
	client, err := arubacentral.NewClient(*configPath, *debug)
	if err != nil {
		fmt.Printf("Error initializing client: %v\n", err)
		return 1
	}

	// Ensure we have a valid token
	if err := client.EnsureValidToken(*tenantID); err != nil {
		fmt.Printf("Authentication error: %v\n", err)
		return 1
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(*timeout)*time.Second)
	defer cancel()

	// Handle commands
	switch {
	case *listAPs:
		if !listAllAPsWithRetry(ctx, client, *tenantID, *autoRetry, *maxRetries) {
			return 1
		}
	case *serialNumber != "":
		if !getAPStatusWithRetry(ctx, client, *serialNumber, *tenantID, *autoRetry, *maxRetries) {
			return 1
		}
	default:
		fmt.Println("No command specified. Use --list or --serial")
		flag.Usage()
		return 1
	}

	return 0
}

// listAllAPsWithRetry lists all APs with rate limit handling
func listAllAPsWithRetry(ctx context.Context, client *arubacentral.Client, tenantID string, autoRetry bool, maxRetries int) bool {
	var allAPs []arubacentral.APStatus
	var err error
	retries := 0

	for {
		// We need a serial number for the API call, but we're only interested in the all APs response
		// Use a dummy serial or the first one we find
		dummySerial := "DUMMY"

		_, allAPs, err = client.GetAPStatusAndAll(ctx, dummySerial, tenantID)

		// Check if the error is rate limiting related
		if arubacentral.IsRateLimited(err) {
			retryAfter := arubacentral.GetRetryAfter(err)
			retries++

			if !autoRetry || retries > maxRetries {
				handleRateLimitError(err)
				return false
			}

			fmt.Printf("Rate limited. Waiting %d seconds before retry (%d/%d)...\n",
				retryAfter, retries, maxRetries)
			time.Sleep(time.Duration(retryAfter) * time.Second)
			continue
		}

		// If not rate limited but another error
		if err != nil {
			// If this is a not found error for our dummy serial, we can still process the list
			if arubacentral.IsNotFound(err) && len(allAPs) > 0 {
				fmt.Printf("Found %d access points\n", len(allAPs))
				printAPList(allAPs)
				return true
			}

			fmt.Printf("Error retrieving AP list: %v\n", err)
			return false
		}

		// Success
		break
	}

	fmt.Printf("Found %d access points\n", len(allAPs))
	printAPList(allAPs)
	return true
}

// getAPStatusWithRetry gets the status of a specific AP with rate limit handling
func getAPStatusWithRetry(ctx context.Context, client *arubacentral.Client, serial, tenantID string, autoRetry bool, maxRetries int) bool {
	var ap *arubacentral.APStatus
	var err error
	retries := 0

	for {
		ap, _, err = client.GetAPStatusAndAll(ctx, serial, tenantID)

		// Check if the error is rate limiting related
		if arubacentral.IsRateLimited(err) {
			retryAfter := arubacentral.GetRetryAfter(err)
			retries++

			if !autoRetry || retries > maxRetries {
				handleRateLimitError(err)
				return false
			}

			fmt.Printf("Rate limited. Waiting %d seconds before retry (%d/%d)...\n",
				retryAfter, retries, maxRetries)
			time.Sleep(time.Duration(retryAfter) * time.Second)
			continue
		}

		// If not a rate limit error but something else
		if err != nil {
			fmt.Printf("Error retrieving AP status: %v\n", err)
			return false
		}

		// Success
		break
	}

	fmt.Println("AP Details:")
	fmt.Printf("  Name:             %s\n", ap.Name)
	fmt.Printf("  Serial:           %s\n", ap.Serial)
	fmt.Printf("  MAC Address:      %s\n", ap.MacAddr)
	fmt.Printf("  Model:            %s\n", ap.Model)
	fmt.Printf("  Status:           %s\n", ap.Status)
	fmt.Printf("  IP Address:       %s\n", ap.IPAddress)
	fmt.Printf("  Public IP:        %s\n", ap.PublicIPAddress)
	fmt.Printf("  Firmware:         %s\n", ap.FirmwareVersion)
	fmt.Printf("  AP Group:         %s\n", ap.APGroup)
	fmt.Printf("  Site:             %s\n", ap.Site)
	fmt.Printf("  Swarm:            %s\n", ap.SwarmName)
	fmt.Printf("  Deployment Mode:  %s\n", ap.APDeploymentMode)

	fmt.Println("\nRadios:")
	for _, radio := range ap.Radios {
		fmt.Printf("  %s (Band: %d):\n", radio.RadioName, radio.Band)
		fmt.Printf("    Type:          %s\n", radio.RadioType)
		fmt.Printf("    MAC:           %s\n", radio.MacAddr)
		fmt.Printf("    Status:        %s\n", radio.Status)
		fmt.Printf("    Spatial Stream: %s\n", radio.SpatialStream)
	}
	return true
}

// handleRateLimitError prints detailed information about rate limit errors
func handleRateLimitError(err error) {
	fmt.Printf("Rate limit exceeded: %v\n", err)

	var apiErr *arubacentral.Error
	if errors.As(err, &apiErr) && apiErr.IsRateLimit {
		if apiErr.RateLimitType == "daily" && apiErr.RateLimitStats != nil {
			fmt.Printf("Daily limit: %d, Remaining: %d\n",
				apiErr.RateLimitStats.DailyLimit, apiErr.RateLimitStats.DailyRemaining)
		} else if apiErr.RateLimitType == "second" && apiErr.RateLimitStats != nil {
			fmt.Printf("Per-second limit: %d, Remaining: %d\n",
				apiErr.RateLimitStats.SecondLimit, apiErr.RateLimitStats.SecondRemaining)
		}
	}

	retryAfter := arubacentral.GetRetryAfter(err)
	if retryAfter > 0 {
		fmt.Printf("Please retry after %d seconds\n", retryAfter)
	}
}

// printAPList prints a list of APs in a table format
func printAPList(aps []arubacentral.APStatus) {
	// Define format string for table
	format := "%-20s %-18s %-15s %-10s %-15s\n"

	// Print table header
	fmt.Printf(format, "NAME", "SERIAL", "MODEL", "STATUS", "IP ADDRESS")
	fmt.Printf(format, "--------------------", "------------------", "---------------", "----------", "---------------")

	// Print each AP
	for i := range aps {
		ap := &aps[i]
		fmt.Printf(format,
			truncateString(ap.Name, 20),
			ap.Serial,
			truncateString(ap.Model, 15),
			ap.Status,
			ap.IPAddress)
	}
}

// truncateString truncates a string to the specified length if it's longer
func truncateString(s string, maxLen int) string {
	if len(s) > maxLen {
		return s[:maxLen-3] + "..."
	}
	return s
}
