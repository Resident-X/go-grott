// Package parser provides functionality for parsing Growatt inverter data.
package parser

import (
	"context"
	"embed"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/resident-x/go-grott/internal/config"
	"github.com/resident-x/go-grott/internal/domain"
	"github.com/resident-x/go-grott/internal/validation"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/sigurn/crc16"
)

//go:embed layouts/*.json
var embeddedLayouts embed.FS

const (
	// CRC16 algorithm constants.
	crcPolynomial = 0xA001 // Polynomial for CRC16 Modbus
	crcInitial    = 0xFFFF // Initial value for CRC calculation

	// Layout constants.
	genericLayoutKey = "T06NNNNXMIN" // Generic layout as fallback

	// Protocol constants.
	extendedDataThreshold = 750 // 375 bytes = 750 hex characters

	// Field type constants.
	fieldTypeText = "text" // Text field type
	fieldTypeNum  = "num"  // Unsigned numeric field type
	fieldTypeNumx = "numx" // Signed numeric field type

	// Common field names.
	datalogSerial = "datalogserial" // Common field for datalogger serial number
	pvserial      = "pvserial"      // Common field for PV serial number
	decrypt       = "decrypt"       // Field to indicate if decryption is needed

)

// RawFieldDefinition represents a field definition from the layout file.
type RawFieldDefinition struct {
	Value   interface{} `json:"value"`  // Position in the hex string (as string or number)
	Length  interface{} `json:"length"` // Length in bytes (as string or number)
	Type    string      `json:"type"`   // Type of field (text, num, numx, etc.)
	Divide  interface{} `json:"divide"` // Value to divide the raw value by
	Include string      `json:"incl"`   // Whether to include this field in output
}

// Layout represents a record layout definition.
type Layout struct {
	Fields map[string]RawFieldDefinition `json:"fields"`
}

// Parser implements domain.DataParser for parsing Growatt inverter data.
type Parser struct {
	config      *config.Config
	crcTable    *crc16.Table
	layouts     map[string]*Layout
	rawLayouts  map[string]map[string]interface{} // Keep original layouts for backward compatibility
	logger      zerolog.Logger                    // Logger for parser operations
	forceLayout string                            // Force using a specific layout (used for testing)
	validator   *validation.AdvancedValidator     // Data validation system
}

// NewParser creates a new Parser instance.
func NewParser(cfg *config.Config) (*Parser, error) {
	// Create CRC16 table for Modbus
	table := crc16.MakeTable(crc16.Params{
		Poly:   crcPolynomial,
		Init:   crcInitial,
		RefIn:  true,
		RefOut: true,
		XorOut: 0,
	})

	// Initialize parser with configuration and logger.
	logger := log.With().Str("component", "parser").Logger()

	// Initialize advanced validator with appropriate level based on log level
	validationLevel := validation.ValidationLevelStandard
	switch cfg.LogLevel {
	case "debug", "trace":
		validationLevel = validation.ValidationLevelStrict
	case "warn", "error":
		validationLevel = validation.ValidationLevelBasic
	}
	validator := validation.NewAdvancedValidator(validationLevel, logger)

	parser := &Parser{
		config:     cfg,
		crcTable:   table,
		layouts:    make(map[string]*Layout),
		rawLayouts: make(map[string]map[string]interface{}),
		logger:     logger,
		validator:  validator,
	}

	// Load layout files.
	if err := parser.loadLayouts(); err != nil {
		return nil, fmt.Errorf("failed to load layout files: %w", err)
	}

	return parser, nil
}

// loadLayouts loads all JSON layout files from embedded files.
func (p *Parser) loadLayouts() error {
	// Read embedded layout files
	layoutFiles, err := embeddedLayouts.ReadDir("layouts")
	if err != nil {
		return fmt.Errorf("failed to read embedded layouts: %w", err)
	}

	// Process each layout file
	for _, file := range layoutFiles {
		if err := p.processLayoutFile(file); err != nil {
			return err
		}
	}

	// Check if we loaded at least one layout file.
	if len(p.layouts) == 0 {
		return fmt.Errorf("no embedded layout files found")
	}

	return nil
}

// processLayoutFile processes a single layout file.
func (p *Parser) processLayoutFile(file fs.DirEntry) error {
	// Skip directories and non-JSON files
	if file.IsDir() || !strings.HasSuffix(strings.ToLower(file.Name()), ".json") {
		return nil
	}

	// Read JSON layout file from embedded FS
	fileData, err := embeddedLayouts.ReadFile("layouts/" + file.Name())
	if err != nil {
		return fmt.Errorf("failed to read embedded layout file %s: %w", file.Name(), err)
	}

	// Store raw layout first (for backward compatibility).
	var rawLayout map[string]interface{}
	if err := json.Unmarshal(fileData, &rawLayout); err != nil {
		return fmt.Errorf("failed to parse layout file %s: %w", file.Name(), err)
	}

	// Use filename (without extension) as key.
	layoutName := strings.ToLower(strings.TrimSuffix(file.Name(), filepath.Ext(file.Name())))
	p.rawLayouts[layoutName] = rawLayout

	// Parse into typed layout structure
	typedLayout := p.parseTypedLayout(rawLayout, fileData, layoutName)
	p.layouts[layoutName] = &typedLayout

	return nil
}

// parseTypedLayout creates a typed Layout from raw data.
func (p *Parser) parseTypedLayout(rawLayout map[string]interface{}, fileData []byte, layoutName string) Layout {
	var typedLayout Layout
	typedLayout.Fields = make(map[string]RawFieldDefinition)

	// Check if this is a nested format with a single top-level key.
	if len(rawLayout) == 1 {
		p.parseNestedLayout(rawLayout, &typedLayout)
	} else {
		// Try direct unmarshaling for flat structure.
		if err := json.Unmarshal(fileData, &typedLayout); err != nil {
			p.logf("Error parsing layout %s: %v", layoutName, err)
		}
	}

	return typedLayout
}

// parseNestedLayout handles nested layout format.
func (p *Parser) parseNestedLayout(rawLayout map[string]interface{}, typedLayout *Layout) {
	for _, value := range rawLayout {
		// Found the inner layout.
		if innerMap, ok := value.(map[string]interface{}); ok {
			// Parse each field.
			for fieldName, fieldDefRaw := range innerMap {
				fieldDefMap, ok := fieldDefRaw.(map[string]interface{})
				if !ok {
					continue
				}
				// Convert to JSON.
				fieldJSON, err := json.Marshal(fieldDefMap)
				if err != nil {
					p.logf("Failed to marshal field %s: %v", fieldName, err)
					continue
				}

				// Parse field definition.
				var fieldDef RawFieldDefinition
				if err := json.Unmarshal(fieldJSON, &fieldDef); err != nil {
					p.logf("Failed to parse field %s: %v", fieldName, err)
					continue
				}

				typedLayout.Fields[fieldName] = fieldDef
			}
			break
		}
	}
}

// Parse implements domain.DataParser.Parse.
func (p *Parser) Parse(ctx context.Context, data []byte) (*domain.InverterData, error) {
	if ctx.Err() != nil {
		return nil, fmt.Errorf("context error: %w", ctx.Err())
	}

	// Check minimum record length from config
	if len(data) < p.config.MinRecordLen {
		p.logf("Skipping data parsing: too short (%d bytes), minimum configured length is %d bytes", len(data), p.config.MinRecordLen)
		return nil, nil // Return nil without error to skip parsing but allow responses
	}

	p.logf("Data: %s", hex.EncodeToString(data))
	p.logf("Starting to parse data of length: %d bytes", len(data))

	// Process the raw data (validation or decryption).
	processedData, err := p.processRawData(data)
	if err != nil {
		return nil, fmt.Errorf("failed to process raw data: %w", err)
	}

	// Convert to hex string and detect layout.
	hexStr := hex.EncodeToString(processedData)
	p.logf("Hex string: %s", hexStr)
	layout, layoutKey, err := p.detectLayout(hexStr)
	if err != nil {
		return nil, fmt.Errorf("failed to detect layout: %w", err)
	}

	p.logf("Using layout %s with %d fields", layoutKey, len(layout.Fields))

	// Perform advanced validation on the raw packet
	protocolKey := strings.ToLower(layoutKey)
	metadata := map[string]interface{}{
		"layout":    layoutKey,
		"protocol":  protocolKey,
		"timestamp": time.Now().Unix(),
		"data_size": len(data),
	}

	validationResult := p.validator.ValidatePacket(data, protocolKey, metadata)
	if !validationResult.Valid {
		p.logf("Packet validation failed: %s", validationResult.Summary())
		// Log errors but continue parsing in standard mode
		for _, err := range validationResult.Errors {
			p.logf("Validation error: %s", err.Error())
		}
	}

	// Log warnings if any
	if validationResult.HasWarnings() {
		for _, warning := range validationResult.Warnings {
			p.logf("Validation warning: %s", warning.Error())
		}
	}

	// Extract data using the layout.
	inverterData := &domain.InverterData{
		Timestamp:    time.Now(),
		Buffered:     "no", // Indicates real-time data, not buffered
		Layout:       layoutKey,
		DeviceType:   p.getDeviceType(layoutKey),
		RawHex:       hexStr,
		ExtendedData: make(map[string]interface{}),
	}

	p.extractFields(hexStr, layout, inverterData)

	// Perform field-level validation
	fieldValidationResult := p.validator.ValidateFieldData(inverterData.ExtendedData, metadata)
	if !fieldValidationResult.Valid {
		p.logf("Field validation failed: %s", fieldValidationResult.Summary())
		// Log but don't fail the parse
		for _, err := range fieldValidationResult.Errors {
			p.logf("Field validation error: %s", err.Error())
		}
	}

	// Add validation metadata to extended data
	inverterData.ExtendedData["validation_confidence"] = validationResult.Confidence
	inverterData.ExtendedData["validation_errors"] = len(validationResult.Errors)
	inverterData.ExtendedData["validation_warnings"] = len(validationResult.Warnings)

	p.logf("Parsing complete. DatalogSerial=%s, PVSerial=%s, ValidationConfidence=%.2f",
		inverterData.DataloggerSerial, inverterData.PVSerial, validationResult.Confidence)

	return inverterData, nil
}

// processRawData handles validation for standard Modbus data or decryption for Growatt data.
func (p *Parser) processRawData(data []byte) ([]byte, error) {
	isStandardModbus := len(data) >= 4 && data[0] == 0x68 && data[3] == 0x68
	p.logf("Is standard Modbus RTU format: %v", isStandardModbus)

	if isStandardModbus {
		// Use legacy CRC validation for basic checks
		if err := p.Validate(data); err != nil {
			return nil, fmt.Errorf("basic validation failed: %w", err)
		}

		// Advanced validation is performed in Parse method after layout detection
		return data, nil
	}

	// For non-standard data, attempt decryption.
	p.logf("Non-standard format detected, attempting decryption")
	if len(data) >= 8 {
		return p.decryptGrowattData(data), nil
	}

	return data, nil
}

// detectLayout determines the appropriate layout based on the hex data.
func (p *Parser) detectLayout(hexStr string) (*Layout, string, error) {
	if p.forceLayout != "" {
		if layout, found := p.layouts[p.forceLayout]; found {
			return layout, p.forceLayout, nil
		}
		p.logf("Forced layout %s not found, falling back to detection", p.forceLayout)
	}

	layoutKey := p.buildLayoutKey(hexStr)
	layout, layoutKey := p.findLayout(layoutKey)

	if layout == nil {
		return nil, "", fmt.Errorf("no layout found for key: %s", layoutKey)
	}

	return layout, layoutKey, nil
}

// buildLayoutKey constructs the layout key from hex data.
func (p *Parser) buildLayoutKey(hexStr string) string {
	header := hexStr[:min(16, len(hexStr))]
	p.logf("Header: %s", header)

	if len(header) >= 16 {
		protocolByte := header[6:8]
		deviceType := header[12:16]

		p.logf("Protocol byte: %s, Device type: %s", protocolByte, deviceType)

		layoutKey := fmt.Sprintf("T%s%s", protocolByte, deviceType)

		// Add 'X' suffix for extended data.
		isSmartMeter := deviceType[2:4] == domain.SmartMeterDevicePattern1 || deviceType[2:4] == domain.SmartMeterDevicePattern2

		if !isSmartMeter {
			// Compare hex length directly against threshold (375 bytes = 750 hex chars)
			if len(hexStr) > extendedDataThreshold {
				layoutKey += "X"
				p.logf("Using extended layout for large data")
			}
			if p.config.InverterType != "default" {
				layoutKey += p.config.InverterType
				p.logf("Using inverter type from config: %s", p.config.InverterType)
			}
		}

		return layoutKey
	}

	// Fallback for short headers.
	return p.buildFallbackLayoutKey(hexStr)
}

// getDeviceType determines the device type based on the layout key.
func (p *Parser) getDeviceType(layoutKey string) string {
	// Check if this is a smart meter layout
	if strings.Contains(layoutKey, domain.SmartMeterLayoutPattern) || strings.Contains(layoutKey, domain.SmartMeterDevicePattern2) {
		return domain.DeviceTypeSmartMeter
	}

	// Default to inverter for all other layouts
	return domain.DeviceTypeInverter
}

// buildFallbackLayoutKey constructs layout key for short headers.
func (p *Parser) buildFallbackLayoutKey(hexStr string) string {
	var protocolID string
	if len(hexStr) >= 26 {
		protocolID = hexStr[24:26]
	} else {
		protocolID = "00"
	}

	if len(hexStr) >= 98 {
		deviceType := hexStr[90:98]
		return fmt.Sprintf("t%s%s", protocolID, deviceType[:min(4, len(deviceType))])
	}

	p.logf("Using generic layout key: %s", genericLayoutKey)
	return genericLayoutKey
}

// findLayout attempts to locate a layout, trying generic versions if needed.
func (p *Parser) findLayout(layoutKey string) (*Layout, string) {
	p.logf("Looking for layout with key: %s", layoutKey)

	// Try exact match first
	if layout, found := p.layouts[layoutKey]; found {
		return layout, layoutKey
	}

	// Try case insensitive match - check lowercase version
	lowerKey := strings.ToLower(layoutKey)
	if layout, found := p.layouts[lowerKey]; found {
		p.logf("Found layout with lowercase key: %s", lowerKey)
		return layout, lowerKey
	}

	// Try generic inverter version.
	if len(layoutKey) >= 6 {
		genericKey := layoutKey[:1] + layoutKey[1:3] + "NNNN" + layoutKey[7:]
		p.logf("Layout not found. Trying generic layout: %s", genericKey)
		if layout, found := p.layouts[genericKey]; found {
			return layout, genericKey
		}

		// Try lowercase generic version too
		lowerGenericKey := strings.ToLower(genericKey)
		if layout, found := p.layouts[lowerGenericKey]; found {
			p.logf("Found generic layout with lowercase key: %s", lowerGenericKey)
			return layout, lowerGenericKey
		}
	}

	// Try generic default version.
	if len(layoutKey) >= 6 {
		genericKey := layoutKey[:1] + layoutKey[1:3] + "NNNNX"
		p.logf("Layout not found. Trying generic layout: %s", genericKey)
		if layout, found := p.layouts[genericKey]; found {
			return layout, genericKey
		}

		// Try lowercase generic version too
		lowerGenericKey := strings.ToLower(genericKey)
		if layout, found := p.layouts[lowerGenericKey]; found {
			p.logf("Found generic layout with lowercase key: %s", lowerGenericKey)
			return layout, lowerGenericKey
		}
	}

	// Use default generic layout.
	p.logf("Using default generic layout: %s", genericLayoutKey)
	return p.layouts[genericLayoutKey], genericLayoutKey
}

// extractFields processes all fields from the layout and populates inverterData.
func (p *Parser) extractFields(hexStr string, layout *Layout, inverterData *domain.InverterData) {
	if len(layout.Fields) == 0 {
		p.logf("No fields in layout")
		return
	}

	// First pass: extract serial number fields.
	p.extractSerialFields(hexStr, layout.Fields, inverterData)

	// Second pass: extract all other fields.
	p.extractDataFields(hexStr, layout.Fields, inverterData)
}

// extractSerialFields extracts datalogserial and pvserial fields.
func (p *Parser) extractSerialFields(hexStr string, fields map[string]RawFieldDefinition, inverterData *domain.InverterData) {
	for fieldName, fieldDef := range fields {
		if fieldName != datalogSerial && fieldName != pvserial {
			continue
		}

		// Create copy to avoid G601 gosec warning about memory aliasing
		fieldDefCopy := fieldDef
		pos, length, ok := p.parseFieldPosition(&fieldDefCopy)
		if !ok || !p.isValidPosition(hexStr, pos, length) {
			p.logf("Field %s position out of bounds", fieldName)
			continue
		}

		textValue, err := p.extractTextValue(hexStr, pos, length)
		if err != nil {
			p.logf("Error extracting %s: %v", fieldName, err)
			continue
		}

		if fieldName == datalogSerial {
			inverterData.DataloggerSerial = textValue
		} else {
			inverterData.PVSerial = textValue
		}

		p.logf("Extracted %s: %s", fieldName, textValue)
	}
}

// extractDataFields extracts all non-serial fields.
func (p *Parser) extractDataFields(hexStr string, fields map[string]RawFieldDefinition, inverterData *domain.InverterData) {
	for fieldName, fieldDef := range fields {
		// Skip already processed fields.
		if fieldName == datalogSerial || fieldName == pvserial || fieldName == decrypt {
			continue
		}

		// Skip excluded fields.
		if fieldDef.Include == "no" {
			continue
		}

		// Create copy to avoid G601 gosec warning about memory aliasing
		fieldDefCopy := fieldDef
		pos, length, ok := p.parseFieldPosition(&fieldDefCopy)
		if !ok {
			p.logf("Invalid position for field %s", fieldName)
			continue
		}

		// Handle text fields.
		if fieldDef.Type == fieldTypeText {
			if p.isValidPosition(hexStr, pos, length) {
				if textValue, err := p.extractTextValue(hexStr, pos, length); err == nil {
					inverterData.ExtendedData[fieldName] = textValue
					p.logf("Field %s text value: %s", fieldName, textValue)
				}
			}
			continue
		}

		// Handle numeric fields (both signed and unsigned).
		divider := p.parseDivider(&fieldDefCopy)
		var value float64
		var err error

		if fieldDef.Type == fieldTypeNumx {
			// Handle signed integers (numx type)
			value, err = p.extractSignedNumericValue(hexStr, pos, length, divider)
		} else {
			// Handle unsigned integers (num type or default)
			value, err = p.extractNumericValue(hexStr, pos, length, divider)
		}

		if err != nil {
			p.logf("Error extracting field %s: %v", fieldName, err)
			continue // Skip fields that can't be extracted
		}

		// Apply field-specific transformations.
		value = p.applyFieldTransformations(fieldName, value)

		p.logf("Field %s value: %.2f", fieldName, value)
		p.setFieldValue(inverterData, fieldName, value)
	}
}

// parseFieldPosition extracts position and length from field definition.
func (p *Parser) parseFieldPosition(fieldDef *RawFieldDefinition) (pos, length int, ok bool) {
	// Get position value.
	switch v := fieldDef.Value.(type) {
	case float64:
		pos = int(v)
	case int:
		pos = v
	case string:
		if posInt, err := strconv.Atoi(v); err == nil {
			pos = posInt
		} else {
			return 0, 0, false
		}
	default:
		return 0, 0, false
	}

	// Get length value.
	if fieldDef.Length != nil {
		switch v := fieldDef.Length.(type) {
		case float64:
			length = int(v)
		case int:
			length = v
		case string:
			if lenInt, err := strconv.Atoi(v); err == nil {
				length = lenInt
			} else {
				return 0, 0, false
			}
		default:
			return 0, 0, false
		}
	} else {
		length = 2 // Default length
	}

	return pos, length, true
}

// isValidPosition checks if the position and length are within bounds.
func (p *Parser) isValidPosition(hexStr string, pos, length int) bool {
	return pos >= 0 && length > 0 && pos+length*2 <= len(hexStr)
}

// extractTextValue extracts a text value from hex string.
func (p *Parser) extractTextValue(hexStr string, pos, length int) (string, error) {
	fieldHex := hexStr[pos : pos+length*2]

	bytes, err := hex.DecodeString(fieldHex)
	if err != nil {
		return "", fmt.Errorf("failed to decode hex: %w", err)
	}

	return cleanASCIIString(string(bytes)), nil
}

// parseDivider extracts the divider value from field definition.
func (p *Parser) parseDivider(fieldDef *RawFieldDefinition) float64 {
	if fieldDef.Divide == nil {
		return 1.0
	}

	switch v := fieldDef.Divide.(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case string:
		if divFloat, err := strconv.ParseFloat(v, 64); err == nil {
			return divFloat
		}
	}

	return 1.0
}

// extractNumericValue extracts a numeric value from hex string.
func (p *Parser) extractNumericValue(hexStr string, pos, length int, divider float64) (float64, error) {
	if !p.isValidPosition(hexStr, pos, length) {
		return 0, fmt.Errorf("position out of bounds")
	}

	hexValue := hexStr[pos : pos+length*2]
	intValue, err := strconv.ParseUint(hexValue, 16, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse hex value: %w", err)
	}

	value := float64(intValue)
	if divider > 0 {
		value /= divider
	}

	return value, nil
}

// extractSignedNumericValue extracts a signed numeric value from hex string.
// This implements the Python equivalent of numx type processing:
// keybytes = bytes.fromhex(result_string[...])
// definedkey[keyword] = int.from_bytes(keybytes, byteorder='big', signed=True)
func (p *Parser) extractSignedNumericValue(hexStr string, pos, length int, divider float64) (float64, error) {
	if !p.isValidPosition(hexStr, pos, length) {
		return 0, fmt.Errorf("position out of bounds")
	}

	hexValue := hexStr[pos : pos+length*2]

	// Decode hex string to bytes
	bytes, err := hex.DecodeString(hexValue)
	if err != nil {
		return 0, fmt.Errorf("failed to decode hex value: %w", err)
	}

	// Convert bytes to signed integer using big-endian byte order
	var intValue int64
	switch length {
	case 1:
		intValue = int64(int8(bytes[0]))
	case 2:
		intValue = int64(int16(binary.BigEndian.Uint16(bytes)))
	case 4:
		intValue = int64(int32(binary.BigEndian.Uint32(bytes)))
	case 8:
		intValue = int64(binary.BigEndian.Uint64(bytes))
	default:
		return 0, fmt.Errorf("unsupported length for signed integer: %d bytes", length)
	}

	value := float64(intValue)
	if divider > 0 {
		value /= divider
	}

	return value, nil
}

// applyFieldTransformations applies field-specific transformations.
func (p *Parser) applyFieldTransformations(fieldName string, value float64) float64 {
	// Apply Python-like transformations
	if fieldName == "pvfrequency" && value == 0 {
		return 50.0 // Python forces frequency to 50.0 if it's 0
	}
	return value
}

// fieldSetterFunc represents a function that sets a field value in InverterData.
type fieldSetterFunc func(*domain.InverterData, float64)

// fieldSetters maps field names to their setter functions.
var fieldSetters = map[string]fieldSetterFunc{
	"pvstatus":      func(d *domain.InverterData, v float64) { d.PVStatus = int(v) },
	"pvpowerin":     func(d *domain.InverterData, v float64) { d.PVPowerIn = v },
	"pvpowerout":    func(d *domain.InverterData, v float64) { d.PVPowerOut = v },
	"eactoday":      func(d *domain.InverterData, v float64) { d.EACToday = v },
	"eactotal":      func(d *domain.InverterData, v float64) { d.EACTotal = v },
	"pvenergytoday": func(d *domain.InverterData, v float64) { d.EACToday = v }, // Alias for eactoday
	"pvenergytotal": func(d *domain.InverterData, v float64) { d.EACTotal = v }, // Alias for eactotal
	"pv1voltage":    func(d *domain.InverterData, v float64) { d.PV1Voltage = v },
	"pv1current":    func(d *domain.InverterData, v float64) { d.PV1Current = v },
	"pv1watt":       func(d *domain.InverterData, v float64) { d.PV1Watt = v },
	"pv2voltage":    func(d *domain.InverterData, v float64) { d.PV2Voltage = v },
	"pv2current":    func(d *domain.InverterData, v float64) { d.PV2Current = v },
	"pv2watt":       func(d *domain.InverterData, v float64) { d.PV2Watt = v },
	"pvfrequency":   func(d *domain.InverterData, v float64) { d.PVFrequency = v },
	"pvgridvoltage": func(d *domain.InverterData, v float64) { d.PVGridVoltage = v },
	"pvgridcurrent": func(d *domain.InverterData, v float64) { d.PVGridCurrent = v },
	"pvgridpower":   func(d *domain.InverterData, v float64) { d.PVGridPower = v },
	"pvtemperature": func(d *domain.InverterData, v float64) { d.PVTemperature = v },
}

// setFieldValue sets a value in the appropriate field of InverterData.
func (p *Parser) setFieldValue(data *domain.InverterData, fieldName string, value float64) {
	fieldNameLower := strings.ToLower(fieldName)

	// Always store in ExtendedData for flattened MQTT output
	data.ExtendedData[fieldName] = value

	// Also populate typed fields for Go code type safety
	if setter, ok := fieldSetters[fieldNameLower]; ok {
		setter(data, value)
	}
}

// SetCustomLogger allows updating the logger (useful for tests).
func (p *Parser) SetCustomLogger(logger *zerolog.Logger) {
	p.logger = logger.With().Str("component", "parser").Logger()
}

// SetForceLayout forces the parser to use a specific layout (mainly for testing).
func (p *Parser) SetForceLayout(layoutName string) {
	p.forceLayout = layoutName
	p.logf("Forcing layout to: %s", layoutName)
}

// GetValidationStatistics returns validation statistics from the advanced validator.
func (p *Parser) GetValidationStatistics() map[string]interface{} {
	if p.validator == nil {
		return map[string]interface{}{}
	}
	return p.validator.GetStatistics()
}

// SetValidationLevel updates the validation level dynamically.
func (p *Parser) SetValidationLevel(level validation.ValidationLevel) {
	if p.validator != nil {
		p.validator.SetValidationLevel(level)
		p.logf("Validation level updated to: %s", level.String())
	}
}

// logf logs a message at debug level.
func (p *Parser) logf(format string, args ...interface{}) {
	p.logger.Debug().Msgf(format, args...)
}

// Validate implements domain.DataParser.Validate.
// Note: Python reference only checks minimum length - removed strict header/footer validation for compatibility
func (p *Parser) Validate(data []byte) error {
	// Python equivalent: if ndata < 12
	if len(data) < 12 {
		return fmt.Errorf("data too short (%d bytes), minimum is 12 bytes", len(data))
	}

	// Python reference doesn't validate SOH/ETX markers or CRC in basic validation
	// Advanced validation can be performed separately if needed
	return nil
}

// decryptGrowattData decrypts the Growatt data using XOR with the "Growatt" mask.
func (p *Parser) decryptGrowattData(data []byte) []byte {
	mask := []byte("Growatt")
	maskLen := len(mask)

	result := make([]byte, len(data))
	copy(result[:8], data[:8]) // Copy header unchanged

	// XOR the remaining bytes with the mask.
	for i := 8; i < len(data); i++ {
		maskIdx := (i - 8) % maskLen
		result[i] = data[i] ^ mask[maskIdx]
	}

	return result
}

// cleanASCIIString cleans an ASCII string by removing non-printable characters.
func cleanASCIIString(s string) string {
	var cleaned strings.Builder
	for _, r := range s {
		if r >= 32 && r <= 126 { // ASCII printable range
			cleaned.WriteRune(r)
		}
	}

	// Trim null bytes and spaces from the right.
	return strings.TrimRight(cleaned.String(), "\x00 ")
}
