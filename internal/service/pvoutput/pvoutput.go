// Package pvoutput provides the PVOutput.org monitoring service implementation.
package pvoutput

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/resident-x/go-grott/internal/config"
	"github.com/resident-x/go-grott/internal/domain"
)

// NoopClient is a no-operation implementation of the MonitoringService interface.
type NoopClient struct{}

// NewNoopClient creates a new no-operation PVOutput client.
func NewNoopClient() *NoopClient {
	return &NoopClient{}
}

// Send is a no-op for the NoopClient.
func (c *NoopClient) Send(_ context.Context, _ *domain.InverterData) error {
	return nil
}

// Connect is a no-op for the NoopClient.
func (c *NoopClient) Connect() error {
	return nil
}

// Close is a no-op for the NoopClient.
func (c *NoopClient) Close() error {
	return nil
}

// Client implements the MonitoringService interface for PVOutput.org.
type Client struct {
	config        *config.Config
	httpClient    *http.Client
	lastUpdateMap map[string]time.Time
	mutex         sync.Mutex
}

// NewClient creates a new PVOutput client.
func NewClient(cfg *config.Config) *Client {
	return &Client{
		config:        cfg,
		httpClient:    &http.Client{Timeout: 10 * time.Second},
		lastUpdateMap: make(map[string]time.Time),
		mutex:         sync.Mutex{},
	}
}

// Connect establishes a connection to the service.
// For PVOutput, this is a no-op as each request is independent.
func (c *Client) Connect() error {
	// No connection needed for PVOutput
	return nil
}

// Send publishes inverter data to the PVOutput service.
func (c *Client) Send(ctx context.Context, data *domain.InverterData) error {
	// If PVOutput is disabled, do nothing
	if !c.config.PVOutput.Enabled {
		return nil
	}

	// Check required configuration
	if c.config.PVOutput.APIKey == "" || c.config.PVOutput.SystemID == "" {
		return fmt.Errorf("PVOutput API key and/or System ID not configured")
	}

	// Apply rate limiting based on inverter serial
	if !c.canUpdate(data.PVSerial) {
		return nil // Skip update due to rate limiting
	}

	// Get system ID for this inverter
	systemID := c.getSystemID(data.PVSerial)
	if systemID == "" {
		return fmt.Errorf("no system ID configured for inverter %s", data.PVSerial)
	}

	// Build the PVOutput update parameters
	params := url.Values{}
	params.Set("key", c.config.PVOutput.APIKey)
	params.Set("sid", systemID)

	// Format date and time
	now := time.Now()
	params.Set("d", now.Format("20060102"))
	params.Set("t", now.Format("15:04"))

	// Set energy production values
	if !c.config.PVOutput.DisableEnergyToday && data.PVEnergyToday > 0 {
		// Convert to watt hours
		energyWh := data.PVEnergyToday * 1000
		params.Set("v1", strconv.FormatFloat(energyWh, 'f', 0, 64))
	}

	// Set power output
	if data.PVPowerOut > 0 {
		// Convert to watts
		powerW := data.PVPowerOut
		params.Set("v2", strconv.FormatFloat(powerW, 'f', 0, 64))
	}

	// Set temperature if configured to use inverter temperature
	if c.config.PVOutput.UseInverterTemp && data.PVTemperature > 0 {
		params.Set("v5", strconv.FormatFloat(data.PVTemperature, 'f', 1, 64))
	}

	// Set voltage
	if data.PVGridVoltage > 0 {
		params.Set("v6", strconv.FormatFloat(data.PVGridVoltage, 'f', 1, 64))
	}

	// Make the API request with context
	req, err := http.NewRequestWithContext(
		ctx,
		"POST",
		"https://pvoutput.org/service/r2/addstatus.jsp",
		strings.NewReader(params.Encode()),
	)
	if err != nil {
		return fmt.Errorf("failed to create PVOutput request: %w", err)
	}

	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Add("X-Rate-Limit", "1")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("PVOutput request failed: %w", err)
	}
	defer func() {
		_ = resp.Body.Close() //nolint:errcheck // Closing response body in defer, error not critical
	}()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("PVOutput returned status code %d", resp.StatusCode)
	}

	// Update rate limit timestamp
	c.updateTimestamp(data.PVSerial)
	return nil
}

// Close terminates the connection to the service.
func (c *Client) Close() error {
	// No resources to clean up for HTTP client
	return nil
}

// canUpdate checks if an update is allowed based on rate limiting.
func (c *Client) canUpdate(inverterSerial string) bool {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	lastUpdate, exists := c.lastUpdateMap[inverterSerial]
	if !exists {
		return true
	}

	// Check if enough time has passed since the last update
	updateInterval := time.Duration(c.config.PVOutput.UpdateLimitMinutes) * time.Minute
	return time.Since(lastUpdate) >= updateInterval
}

// updateTimestamp records when an update was made.
func (c *Client) updateTimestamp(inverterSerial string) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.lastUpdateMap[inverterSerial] = time.Now()
}

// getSystemID returns the PVOutput system ID for an inverter.
func (c *Client) getSystemID(inverterSerial string) string {
	// If multiple inverters are not configured, use the default system ID
	if !c.config.PVOutput.MultipleInverters {
		return c.config.PVOutput.SystemID
	}

	// Look up the system ID in the inverter mappings
	for _, mapping := range c.config.PVOutput.InverterMappings {
		if mapping.InverterSerial == inverterSerial {
			return mapping.SystemID
		}
	}

	// If no mapping found but we have a default, use that
	if c.config.PVOutput.SystemID != "" {
		return c.config.PVOutput.SystemID
	}

	return ""
}
