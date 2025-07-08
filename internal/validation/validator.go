// Package validation provides advanced data validation and integrity checks for Growatt device communication.
package validation

import (
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

// ValidationLevel defines the strictness of validation rules.
type ValidationLevel int

const (
	ValidationLevelBasic ValidationLevel = iota
	ValidationLevelStandard
	ValidationLevelStrict
	ValidationLevelParanoid
)

// String returns the string representation of the validation level.
func (vl ValidationLevel) String() string {
	switch vl {
	case ValidationLevelBasic:
		return "basic"
	case ValidationLevelStandard:
		return "standard"
	case ValidationLevelStrict:
		return "strict"
	case ValidationLevelParanoid:
		return "paranoid"
	default:
		return "unknown"
	}
}

// ValidationError represents a validation error with severity and context.
type ValidationError struct {
	Type     string
	Severity string
	Message  string
	Field    string
	Value    interface{}
	Context  map[string]interface{}
}

// Error implements the error interface.
func (ve *ValidationError) Error() string {
	return fmt.Sprintf("%s validation error in %s: %s", ve.Severity, ve.Field, ve.Message)
}

// ValidationResult contains the result of a validation check.
type ValidationResult struct {
	Valid      bool
	Errors     []*ValidationError
	Warnings   []*ValidationError
	Fixes      []string
	Confidence float64 // 0.0-1.0 confidence in data integrity
}

// HasCriticalErrors returns true if there are any critical validation errors.
func (vr *ValidationResult) HasCriticalErrors() bool {
	for _, err := range vr.Errors {
		if err.Severity == "critical" || err.Severity == "error" {
			return true
		}
	}
	return false
}

// HasWarnings returns true if there are any validation warnings.
func (vr *ValidationResult) HasWarnings() bool {
	return len(vr.Warnings) > 0
}

// Summary returns a summary of the validation result.
func (vr *ValidationResult) Summary() string {
	if vr.Valid && !vr.HasWarnings() {
		return fmt.Sprintf("Valid (confidence: %.2f)", vr.Confidence)
	}

	var parts []string
	if !vr.Valid {
		parts = append(parts, fmt.Sprintf("%d errors", len(vr.Errors)))
	}
	if vr.HasWarnings() {
		parts = append(parts, fmt.Sprintf("%d warnings", len(vr.Warnings)))
	}

	return fmt.Sprintf("%s (confidence: %.2f)", strings.Join(parts, ", "), vr.Confidence)
}

// ProtocolRule defines a validation rule for protocol-specific checks.
type ProtocolRule struct {
	Name        string
	Description string
	Protocol    string
	Level       ValidationLevel
	Check       func(data []byte, metadata map[string]interface{}) *ValidationError
}

// DataIntegrityRule defines a rule for data integrity validation.
type DataIntegrityRule struct {
	Name        string
	Description string
	Field       string
	Level       ValidationLevel
	Check       func(value interface{}, context map[string]interface{}) *ValidationError
}

// AdvancedValidator provides comprehensive validation capabilities.
type AdvancedValidator struct {
	level         ValidationLevel
	protocolRules map[string][]*ProtocolRule
	dataRules     []*DataIntegrityRule
	logger        zerolog.Logger

	// Statistics
	validationsPerformed int64
	errorsFound          int64
	warningsFound        int64
	corruptionsDetected  int64
}

// NewAdvancedValidator creates a new advanced validator.
func NewAdvancedValidator(level ValidationLevel, logger zerolog.Logger) *AdvancedValidator {
	validator := &AdvancedValidator{
		level:         level,
		protocolRules: make(map[string][]*ProtocolRule),
		dataRules:     make([]*DataIntegrityRule, 0),
		logger:        logger.With().Str("component", "validator").Logger(),
	}

	// Register default rules
	validator.registerDefaultProtocolRules()
	validator.registerDefaultDataRules()

	return validator
}

// ValidatePacket performs comprehensive validation of a data packet.
func (av *AdvancedValidator) ValidatePacket(data []byte, protocol string, metadata map[string]interface{}) *ValidationResult {
	av.validationsPerformed++

	result := &ValidationResult{
		Valid:      true,
		Errors:     make([]*ValidationError, 0),
		Warnings:   make([]*ValidationError, 0),
		Fixes:      make([]string, 0),
		Confidence: 1.0,
	}

	// Apply protocol-specific rules
	if rules, exists := av.protocolRules[protocol]; exists {
		for _, rule := range rules {
			if rule.Level <= av.level {
				if err := rule.Check(data, metadata); err != nil {
					av.addValidationError(result, err)
				}
			}
		}
	}

	// Apply generic protocol rules if specific ones don't exist
	if _, exists := av.protocolRules[protocol]; !exists {
		if genericRules, exists := av.protocolRules["generic"]; exists {
			for _, rule := range genericRules {
				if rule.Level <= av.level {
					if err := rule.Check(data, metadata); err != nil {
						av.addValidationError(result, err)
					}
				}
			}
		}
	}

	av.logger.Debug().
		Str("protocol", protocol).
		Int("data_length", len(data)).
		Int("errors", len(result.Errors)).
		Int("warnings", len(result.Warnings)).
		Float64("confidence", result.Confidence).
		Msg("Packet validation completed")

	return result
}

// ValidateFieldData performs validation on parsed field data.
func (av *AdvancedValidator) ValidateFieldData(fields map[string]interface{}, metadata map[string]interface{}) *ValidationResult {
	result := &ValidationResult{
		Valid:      true,
		Errors:     make([]*ValidationError, 0),
		Warnings:   make([]*ValidationError, 0),
		Fixes:      make([]string, 0),
		Confidence: 1.0,
	}

	// Apply data integrity rules
	for _, rule := range av.dataRules {
		if rule.Level <= av.level {
			if value, exists := fields[rule.Field]; exists {
				if err := rule.Check(value, metadata); err != nil {
					av.addValidationError(result, err)
				}
			}
		}
	}

	return result
}

// addValidationError adds a validation error to the result and updates metrics.
func (av *AdvancedValidator) addValidationError(result *ValidationResult, err *ValidationError) {
	if err.Severity == "warning" {
		result.Warnings = append(result.Warnings, err)
		av.warningsFound++
		result.Confidence *= 0.95 // Slight confidence reduction for warnings
	} else {
		result.Errors = append(result.Errors, err)
		av.errorsFound++
		result.Valid = false

		// Reduce confidence based on error severity
		switch err.Severity {
		case "critical":
			result.Confidence *= 0.1
			av.corruptionsDetected++
		case "error":
			result.Confidence *= 0.5
		case "minor":
			result.Confidence *= 0.8
		}
	}
}

// registerDefaultProtocolRules registers default protocol validation rules.
func (av *AdvancedValidator) registerDefaultProtocolRules() {
	// Generic protocol rules
	genericRules := []*ProtocolRule{
		{
			Name:        "packet_size_check",
			Description: "Validates packet size is within reasonable bounds",
			Protocol:    "generic",
			Level:       ValidationLevelBasic,
			Check: func(data []byte, metadata map[string]interface{}) *ValidationError {
				if len(data) < 12 {
					return &ValidationError{
						Type:     "protocol",
						Severity: "critical",
						Message:  fmt.Sprintf("packet too short: %d bytes, minimum 12", len(data)),
						Field:    "packet_size",
						Value:    len(data),
						Context:  metadata,
					}
				}
				if len(data) > 2048 {
					return &ValidationError{
						Type:     "protocol",
						Severity: "warning",
						Message:  fmt.Sprintf("unusually large packet: %d bytes", len(data)),
						Field:    "packet_size",
						Value:    len(data),
						Context:  metadata,
					}
				}
				return nil
			},
		},
		{
			Name:        "protocol_structure_check",
			Description: "Validates basic protocol structure markers",
			Protocol:    "generic",
			Level:       ValidationLevelBasic,
			Check: func(data []byte, metadata map[string]interface{}) *ValidationError {
				if len(data) < 12 {
					return nil // Handled by packet_size_check
				}

				// Check SOH markers
				if data[0] != 0x68 || data[3] != 0x68 {
					return &ValidationError{
						Type:     "protocol",
						Severity: "critical",
						Message:  "invalid SOH markers",
						Field:    "protocol_header",
						Value:    fmt.Sprintf("0x%02x, 0x%02x", data[0], data[3]),
						Context:  metadata,
					}
				}

				// Check ETX marker
				if data[len(data)-2] != 0x16 {
					return &ValidationError{
						Type:     "protocol",
						Severity: "critical",
						Message:  "invalid ETX marker",
						Field:    "protocol_footer",
						Value:    fmt.Sprintf("0x%02x", data[len(data)-2]),
						Context:  metadata,
					}
				}

				return nil
			},
		},
	}

	av.protocolRules["generic"] = genericRules

	// Protocol V6 specific rules
	v6Rules := []*ProtocolRule{
		{
			Name:        "v6_length_consistency",
			Description: "Validates protocol V6 length field consistency",
			Protocol:    "06",
			Level:       ValidationLevelStandard,
			Check: func(data []byte, metadata map[string]interface{}) *ValidationError {
				if len(data) < 6 {
					return nil
				}

				// Extract length field (bytes 1-2, little endian)
				declaredLength := int(data[1]) | (int(data[2]) << 8)
				actualLength := len(data) - 4 // Excluding header and footer

				if declaredLength != actualLength {
					return &ValidationError{
						Type:     "protocol",
						Severity: "error",
						Message:  fmt.Sprintf("length mismatch: declared %d, actual %d", declaredLength, actualLength),
						Field:    "length_field",
						Value:    map[string]int{"declared": declaredLength, "actual": actualLength},
						Context:  metadata,
					}
				}

				return nil
			},
		},
		{
			Name:        "v6_data_pattern_check",
			Description: "Detects suspicious data patterns in V6 protocol",
			Protocol:    "06",
			Level:       ValidationLevelStrict,
			Check: func(data []byte, metadata map[string]interface{}) *ValidationError {
				if len(data) < 20 {
					return nil
				}

				// Check for repeated byte patterns (potential corruption)
				dataSection := data[8 : len(data)-4] // Skip header and footer
				if av.hasRepeatedPattern(dataSection, 16) {
					return &ValidationError{
						Type:     "data_integrity",
						Severity: "warning",
						Message:  "detected repeated byte pattern (possible corruption)",
						Field:    "data_pattern",
						Value:    "repeated_pattern",
						Context:  metadata,
					}
				}

				// Check for all-zero or all-FF patterns
				if av.hasUniformPattern(dataSection) {
					return &ValidationError{
						Type:     "data_integrity",
						Severity: "warning",
						Message:  "detected uniform byte pattern (possible corruption)",
						Field:    "data_pattern",
						Value:    "uniform_pattern",
						Context:  metadata,
					}
				}

				return nil
			},
		},
	}

	av.protocolRules["06"] = append(genericRules, v6Rules...)
}

// registerDefaultDataRules registers default data integrity rules.
func (av *AdvancedValidator) registerDefaultDataRules() {
	av.dataRules = []*DataIntegrityRule{
		{
			Name:        "serial_number_format",
			Description: "Validates serial number format and consistency",
			Field:       "datalogserial",
			Level:       ValidationLevelStandard,
			Check: func(value interface{}, context map[string]interface{}) *ValidationError {
				serial, ok := value.(string)
				if !ok {
					return &ValidationError{
						Type:     "data_type",
						Severity: "error",
						Message:  "serial number is not a string",
						Field:    "datalogserial",
						Value:    value,
						Context:  context,
					}
				}

				if len(serial) < 8 || len(serial) > 20 {
					return &ValidationError{
						Type:     "data_format",
						Severity: "warning",
						Message:  fmt.Sprintf("unusual serial number length: %d characters", len(serial)),
						Field:    "datalogserial",
						Value:    serial,
						Context:  context,
					}
				}

				// Check for invalid characters
				for _, r := range serial {
					if !((r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z')) {
						return &ValidationError{
							Type:     "data_format",
							Severity: "warning",
							Message:  "serial number contains invalid characters",
							Field:    "datalogserial",
							Value:    serial,
							Context:  context,
						}
					}
				}

				return nil
			},
		},
		{
			Name:        "timestamp_reasonableness",
			Description: "Validates timestamp values are reasonable",
			Field:       "time",
			Level:       ValidationLevelStandard,
			Check: func(value interface{}, context map[string]interface{}) *ValidationError {
				timeStr, ok := value.(string)
				if !ok {
					return nil // Not a string timestamp
				}

				// Parse timestamp
				parsedTime, err := time.Parse("2006-01-02 15:04:05", timeStr)
				if err != nil {
					return &ValidationError{
						Type:     "data_format",
						Severity: "error",
						Message:  "invalid timestamp format",
						Field:    "time",
						Value:    timeStr,
						Context:  context,
					}
				}

				now := time.Now()

				// Check if timestamp is too far in the future
				if parsedTime.After(now.Add(24 * time.Hour)) {
					return &ValidationError{
						Type:     "data_integrity",
						Severity: "error",
						Message:  "timestamp is too far in the future",
						Field:    "time",
						Value:    timeStr,
						Context:  context,
					}
				}

				// Check if timestamp is too far in the past
				if parsedTime.Before(now.Add(-365 * 24 * time.Hour)) {
					return &ValidationError{
						Type:     "data_integrity",
						Severity: "warning",
						Message:  "timestamp is more than a year old",
						Field:    "time",
						Value:    timeStr,
						Context:  context,
					}
				}

				return nil
			},
		},
		{
			Name:        "power_value_reasonableness",
			Description: "Validates power values are within reasonable ranges",
			Field:       "pac",
			Level:       ValidationLevelStandard,
			Check: func(value interface{}, context map[string]interface{}) *ValidationError {
				var power float64

				switch v := value.(type) {
				case float64:
					power = v
				case float32:
					power = float64(v)
				case int:
					power = float64(v)
				case int64:
					power = float64(v)
				default:
					return nil // Not a numeric power value
				}

				// Check for negative power (unusual for inverters)
				if power < 0 {
					return &ValidationError{
						Type:     "data_integrity",
						Severity: "warning",
						Message:  "negative power value detected",
						Field:    "pac",
						Value:    power,
						Context:  context,
					}
				}

				// Check for unreasonably high power (>100kW for typical residential)
				if power > 100000 {
					return &ValidationError{
						Type:     "data_integrity",
						Severity: "warning",
						Message:  "unusually high power value",
						Field:    "pac",
						Value:    power,
						Context:  context,
					}
				}

				return nil
			},
		},
	}
}

// hasRepeatedPattern checks if data contains repeated byte patterns.
func (av *AdvancedValidator) hasRepeatedPattern(data []byte, minLength int) bool {
	if len(data) < minLength {
		return false
	}

	// Check for repeated sequences
	for patternLen := 4; patternLen <= minLength && patternLen <= len(data)/2; patternLen++ {
		pattern := data[:patternLen]
		matches := 0

		for i := patternLen; i+patternLen <= len(data); i += patternLen {
			if string(data[i:i+patternLen]) == string(pattern) {
				matches++
			} else {
				break
			}
		}

		if matches >= 3 { // Pattern repeats at least 4 times total
			return true
		}
	}

	return false
}

// hasUniformPattern checks if data contains uniform byte patterns (all same value).
func (av *AdvancedValidator) hasUniformPattern(data []byte) bool {
	if len(data) < 16 {
		return false
	}

	// Check for sequences of same byte
	consecutiveCount := 1
	for i := 1; i < len(data); i++ {
		if data[i] == data[i-1] {
			consecutiveCount++
			if consecutiveCount >= 16 {
				return true
			}
		} else {
			consecutiveCount = 1
		}
	}

	return false
}

// GetStatistics returns validation statistics.
func (av *AdvancedValidator) GetStatistics() map[string]interface{} {
	return map[string]interface{}{
		"validations_performed": av.validationsPerformed,
		"errors_found":          av.errorsFound,
		"warnings_found":        av.warningsFound,
		"corruptions_detected":  av.corruptionsDetected,
		"validation_level":      av.level.String(),
		"protocol_rules":        len(av.protocolRules),
		"data_rules":            len(av.dataRules),
	}
}

// SetValidationLevel changes the validation level.
func (av *AdvancedValidator) SetValidationLevel(level ValidationLevel) {
	av.level = level
	av.logger.Info().
		Str("old_level", av.level.String()).
		Str("new_level", level.String()).
		Msg("Validation level changed")
}

// AddProtocolRule adds a custom protocol validation rule.
func (av *AdvancedValidator) AddProtocolRule(protocol string, rule *ProtocolRule) {
	if av.protocolRules[protocol] == nil {
		av.protocolRules[protocol] = make([]*ProtocolRule, 0)
	}
	av.protocolRules[protocol] = append(av.protocolRules[protocol], rule)

	av.logger.Debug().
		Str("protocol", protocol).
		Str("rule", rule.Name).
		Msg("Added custom protocol rule")
}

// AddDataRule adds a custom data validation rule.
func (av *AdvancedValidator) AddDataRule(rule *DataIntegrityRule) {
	av.dataRules = append(av.dataRules, rule)

	av.logger.Debug().
		Str("field", rule.Field).
		Str("rule", rule.Name).
		Msg("Added custom data rule")
}
