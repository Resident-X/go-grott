// Package api provides format conversion utilities for register operations.
package api

import (
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// FormatType represents different data format types supported by the API.
type FormatType string

const (
	FormatDec  FormatType = "dec"
	FormatHex  FormatType = "hex"
	FormatText FormatType = "text"
)

// FormatConverter provides utilities for converting between different data formats.
type FormatConverter struct{}

// NewFormatConverter creates a new format converter instance.
func NewFormatConverter() *FormatConverter {
	return &FormatConverter{}
}

// ConvertToHex converts a value from the specified input format to hex string.
func (fc *FormatConverter) ConvertToHex(value string, inputFormat FormatType) (string, error) {
	switch inputFormat {
	case FormatDec:
		return fc.decToHex(value)
	case FormatHex:
		return fc.normalizeHex(value)
	case FormatText:
		return fc.textToHex(value)
	default:
		return "", fmt.Errorf("unsupported input format: %s", inputFormat)
	}
}

// ConvertFromHex converts a hex string to the specified output format.
func (fc *FormatConverter) ConvertFromHex(hexValue string, outputFormat FormatType) (interface{}, error) {
	switch outputFormat {
	case FormatDec:
		return fc.hexToDec(hexValue)
	case FormatHex:
		return hexValue, nil
	case FormatText:
		return fc.hexToText(hexValue)
	default:
		return nil, fmt.Errorf("unsupported output format: %s", outputFormat)
	}
}

// ConvertValue converts a value from input format to output format.
func (fc *FormatConverter) ConvertValue(value string, inputFormat, outputFormat FormatType) (interface{}, error) {
	// First convert to hex as intermediate format
	hexValue, err := fc.ConvertToHex(value, inputFormat)
	if err != nil {
		return nil, fmt.Errorf("failed to convert to hex: %w", err)
	}
	
	// Then convert hex to output format
	return fc.ConvertFromHex(hexValue, outputFormat)
}

// ValidateFormat checks if the given format is supported.
func (fc *FormatConverter) ValidateFormat(format string) error {
	switch FormatType(strings.ToLower(format)) {
	case FormatDec, FormatHex, FormatText:
		return nil
	default:
		return fmt.Errorf("invalid format specified: %s (supported: dec, hex, text)", format)
	}
}

// IsValidFormat checks if the given format is supported, returning true/false.
func (fc *FormatConverter) IsValidFormat(format string) bool {
	switch FormatType(strings.ToLower(format)) {
	case FormatDec, FormatHex, FormatText:
		return true
	default:
		return false
	}
}

// ValidateValue validates that a value is appropriate for the given format.
func (fc *FormatConverter) ValidateValue(value string, format FormatType) error {
	switch format {
	case FormatDec:
		return fc.validateDecValue(value)
	case FormatHex:
		return fc.validateHexValue(value)
	case FormatText:
		return fc.validateTextValue(value)
	default:
		return fmt.Errorf("unsupported format for validation: %s", format)
	}
}

// decToHex converts a decimal string to hex string.
func (fc *FormatConverter) decToHex(decValue string) (string, error) {
	val, err := strconv.ParseInt(decValue, 10, 64)
	if err != nil {
		return "", fmt.Errorf("invalid decimal value: %s", decValue)
	}
	
	if val < 0 || val > 65535 {
		return "", fmt.Errorf("decimal value out of range (0-65535): %d", val)
	}
	
	return fmt.Sprintf("%04x", val), nil
}

// hexToDec converts a hex string to decimal integer.
func (fc *FormatConverter) hexToDec(hexValue string) (int, error) {
	// Remove any 0x prefix
	hexValue = strings.TrimPrefix(strings.ToLower(hexValue), "0x")
	
	val, err := strconv.ParseInt(hexValue, 16, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid hex value: %s", hexValue)
	}
	
	return int(val), nil
}

// textToHex converts a text string to hex string.
func (fc *FormatConverter) textToHex(textValue string) (string, error) {
	if len(textValue) == 0 {
		return "", fmt.Errorf("text value cannot be empty")
	}
	
	// Convert text to UTF-8 bytes then to hex
	textBytes := []byte(textValue)
	
	// For register operations, we typically work with 16-bit values
	// If text is longer than 2 bytes, we'll use the first 2 bytes
	if len(textBytes) > 2 {
		textBytes = textBytes[:2]
	}
	
	// Pad to 2 bytes if shorter
	if len(textBytes) < 2 {
		padded := make([]byte, 2)
		copy(padded, textBytes)
		textBytes = padded
	}
	
	// Convert to little-endian hex (as expected by Growatt protocol)
	return fmt.Sprintf("%02x%02x", textBytes[1], textBytes[0]), nil
}

// hexToText converts a hex string to text string.
func (fc *FormatConverter) hexToText(hexValue string) (string, error) {
	// Remove any 0x prefix
	hexValue = strings.TrimPrefix(strings.ToLower(hexValue), "0x")
	
	// Ensure even length
	if len(hexValue)%2 != 0 {
		hexValue = "0" + hexValue
	}
	
	bytes, err := hex.DecodeString(hexValue)
	if err != nil {
		return "", fmt.Errorf("invalid hex value: %s", hexValue)
	}
	
	// For register values, convert from little-endian
	if len(bytes) >= 2 {
		// Swap bytes for little-endian conversion
		bytes[0], bytes[1] = bytes[1], bytes[0]
	}
	
	// Remove null bytes and convert to string
	text := strings.TrimRight(string(bytes), "\x00")
	
	return text, nil
}

// normalizeHex ensures hex string is in proper format.
func (fc *FormatConverter) normalizeHex(hexValue string) (string, error) {
	// Remove any 0x prefix
	hexValue = strings.TrimPrefix(strings.ToLower(hexValue), "0x")
	
	// Validate hex characters
	if _, err := hex.DecodeString(hexValue); err != nil {
		return "", fmt.Errorf("invalid hex value: %s", hexValue)
	}
	
	// Ensure proper length (pad to 4 chars for 16-bit values)
	if len(hexValue) < 4 {
		hexValue = strings.Repeat("0", 4-len(hexValue)) + hexValue
	}
	
	return hexValue, nil
}

// validateDecValue validates a decimal value string.
func (fc *FormatConverter) validateDecValue(value string) error {
	val, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid decimal value: %s", value)
	}
	
	if val < 0 || val > 65535 {
		return fmt.Errorf("decimal value out of range (0-65535): %d", val)
	}
	
	return nil
}

// validateHexValue validates a hex value string.
func (fc *FormatConverter) validateHexValue(value string) error {
	// Remove any 0x prefix
	value = strings.TrimPrefix(strings.ToLower(value), "0x")
	
	if _, err := hex.DecodeString(value); err != nil {
		return fmt.Errorf("invalid hex value: %s", value)
	}
	
	return nil
}

// validateTextValue validates a text value string.
func (fc *FormatConverter) validateTextValue(value string) error {
	if len(value) == 0 {
		return fmt.Errorf("text value cannot be empty")
	}
	
	// Check for valid UTF-8
	if !utf8.ValidString(value) {
		return fmt.Errorf("text value contains invalid UTF-8 characters")
	}
	
	return nil
}

// GetDefaultFormat returns the default format for a given operation type.
func (fc *FormatConverter) GetDefaultFormat() FormatType {
	return FormatDec // Default to decimal as in Python implementation
}

// FormatResponseValue formats a response value according to the requested format.
func (fc *FormatConverter) FormatResponseValue(hexValue string, format FormatType) (interface{}, error) {
	switch format {
	case FormatDec:
		return fc.hexToDec(hexValue)
	case FormatHex:
		return hexValue, nil
	case FormatText:
		return fc.hexToText(hexValue)
	default:
		// Default to hex if format is unrecognized
		return hexValue, nil
	}
}

// CreateValueForCommand prepares a value for use in a device command.
func (fc *FormatConverter) CreateValueForCommand(value string, inputFormat FormatType) (string, error) {
	// Always convert to hex for device commands
	hexValue, err := fc.ConvertToHex(value, inputFormat)
	if err != nil {
		return "", err
	}
	
	// Ensure 4-character hex format for 16-bit registers
	if len(hexValue) < 4 {
		hexValue = strings.Repeat("0", 4-len(hexValue)) + hexValue
	}
	
	return hexValue, nil
}
