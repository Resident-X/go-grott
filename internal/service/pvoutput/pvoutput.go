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

	// Check if this is smart meter data with consumption values
	if c.hasSmartMeterData(data) {
		// Send smart meter data with dual-post (v3/v4 parameters)
		return c.sendSmartMeterData(ctx, data, systemID)
	}

	// Send standard inverter data (v1/v2 parameters)
	return c.sendInverterData(ctx, data, systemID)
}

// sendInverterData sends standard inverter generation data to PVOutput (v1, v2, v5, v6).
func (c *Client) sendInverterData(ctx context.Context, data *domain.InverterData, systemID string) error {
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

	// Make the API request
	if err := c.makeRequest(ctx, params); err != nil {
		return err
	}

	// Update rate limit timestamp
	c.updateTimestamp(data.PVSerial)
	return nil
}

// sendSmartMeterData sends smart meter consumption data to PVOutput using dual-post method.
// First POST: v3 (lifetime consumption energy) with c1=3 flag
// Second POST: v4 (net power) with n=1 flag
func (c *Client) sendSmartMeterData(ctx context.Context, data *domain.InverterData, systemID string) error {
	now := time.Now()
	dateStr := now.Format("20060102")
	timeStr := now.Format("15:04")

	// Extract smart meter values from ExtendedData
	posActEnergy := c.getFloat64(data.ExtendedData, "pos_act_energy")
	posRevActPower := c.getFloat64(data.ExtendedData, "pos_rev_act_power")
	voltageL1 := c.getFloat64(data.ExtendedData, "voltage_l1")

	// First POST: lifetime consumption energy (v3)
	params1 := url.Values{}
	params1.Set("key", c.config.PVOutput.APIKey)
	params1.Set("sid", systemID)
	params1.Set("d", dateStr)
	params1.Set("t", timeStr)

	if posActEnergy > 0 {
		// v3: Consumption energy in Wh (multiply by 100 for lifetime value)
		params1.Set("v3", strconv.FormatFloat(posActEnergy*100, 'f', 0, 64))
		params1.Set("c1", "3") // Flag indicating v3 is lifetime cumulative
	}

	if voltageL1 > 0 {
		params1.Set("v6", strconv.FormatFloat(voltageL1, 'f', 1, 64))
	}

	// Make first request
	if err := c.makeRequest(ctx, params1); err != nil {
		return fmt.Errorf("smart meter first POST (v3) failed: %w", err)
	}

	// Second POST: net power (v4)
	params2 := url.Values{}
	params2.Set("key", c.config.PVOutput.APIKey)
	params2.Set("sid", systemID)
	params2.Set("d", dateStr)
	params2.Set("t", timeStr)

	if posRevActPower != 0 {
		// v4: Net power in W (positive = consuming, negative = exporting)
		params2.Set("v4", strconv.FormatFloat(posRevActPower, 'f', 0, 64))
		params2.Set("n", "1") // Flag indicating net data
	}

	if voltageL1 > 0 {
		params2.Set("v6", strconv.FormatFloat(voltageL1, 'f', 1, 64))
	}

	// Make second request
	if err := c.makeRequest(ctx, params2); err != nil {
		return fmt.Errorf("smart meter second POST (v4) failed: %w", err)
	}

	// Update rate limit timestamp
	c.updateTimestamp(data.PVSerial)
	return nil
}

// makeRequest makes an HTTP POST request to PVOutput API.
func (c *Client) makeRequest(ctx context.Context, params url.Values) error {
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

	return nil
}

// hasSmartMeterData checks if the inverter data contains smart meter consumption values.
func (c *Client) hasSmartMeterData(data *domain.InverterData) bool {
	if data.ExtendedData == nil {
		return false
	}

	// Check for presence of smart meter fields
	_, hasPosActEnergy := data.ExtendedData["pos_act_energy"]
	_, hasPosRevActPower := data.ExtendedData["pos_rev_act_power"]

	return hasPosActEnergy || hasPosRevActPower
}

// getFloat64 safely extracts a float64 value from ExtendedData map.
func (c *Client) getFloat64(extendedData map[string]interface{}, key string) float64 {
	if extendedData == nil {
		return 0
	}

	val, ok := extendedData[key]
	if !ok {
		return 0
	}

	switch v := val.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	default:
		return 0
	}
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
