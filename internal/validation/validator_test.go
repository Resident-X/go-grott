package validation

import (
	"strings"
	"testing"
	"time"

	"github.com/resident-x/go-grott/internal/protocol"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidationLevel_String(t *testing.T) {
	tests := []struct {
		level    ValidationLevel
		expected string
	}{
		{ValidationLevelBasic, "basic"},
		{ValidationLevelStandard, "standard"},
		{ValidationLevelStrict, "strict"},
		{ValidationLevelParanoid, "paranoid"},
		{ValidationLevel(999), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.level.String())
		})
	}
}

func TestValidationError(t *testing.T) {
	err := &ValidationError{
		Type:     "protocol",
		Severity: "critical",
		Message:  "test error",
		Field:    "test_field",
		Value:    "test_value",
		Context:  map[string]interface{}{"key": "value"},
	}

	assert.Equal(t, "critical validation error in test_field: test error", err.Error())
}

func TestValidationResult(t *testing.T) {
	t.Run("HasCriticalErrors", func(t *testing.T) {
		result := &ValidationResult{
			Errors: []*ValidationError{
				{Severity: "warning"},
				{Severity: "critical"},
			},
		}
		assert.True(t, result.HasCriticalErrors())

		result.Errors = []*ValidationError{{Severity: "warning"}}
		assert.False(t, result.HasCriticalErrors())
	})

	t.Run("HasWarnings", func(t *testing.T) {
		result := &ValidationResult{
			Warnings: []*ValidationError{{Severity: "warning"}},
		}
		assert.True(t, result.HasWarnings())

		result.Warnings = nil
		assert.False(t, result.HasWarnings())
	})

	t.Run("Summary", func(t *testing.T) {
		// Valid with no warnings
		result := &ValidationResult{
			Valid:      true,
			Confidence: 0.95,
		}
		assert.Equal(t, "Valid (confidence: 0.95)", result.Summary())

		// With errors and warnings
		result = &ValidationResult{
			Valid:      false,
			Errors:     []*ValidationError{{}, {}},
			Warnings:   []*ValidationError{{}},
			Confidence: 0.5,
		}
		assert.Equal(t, "2 errors, 1 warnings (confidence: 0.50)", result.Summary())
	})
}

func TestAdvancedValidator_Creation(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	validator := NewAdvancedValidator(ValidationLevelStandard, logger)

	assert.NotNil(t, validator)
	assert.Equal(t, ValidationLevelStandard, validator.level)
	assert.NotEmpty(t, validator.protocolRules)
	assert.NotEmpty(t, validator.dataRules)
}

func TestAdvancedValidator_ValidatePacket(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	validator := NewAdvancedValidator(ValidationLevelStandard, logger)

	t.Run("Valid Packet", func(t *testing.T) {
		// Create a valid packet structure (Python only checks minimum length)
		data := []byte{
			0x68, 0x20, 0x00, 0x68, // Header: SOH, length, SOH
			0x01, 0x02, 0x03, 0x04, // Data
			0x05, 0x06, 0x07, 0x08,
			0x09, 0x0A, 0x0B, 0x0C,
			0x0D, 0x0E, 0x0F, 0x10,
			0x11, 0x12, 0x13, 0x14,
			0x15, 0x16, 0x17, 0x18,
			0x19, 0x1A, 0x1B, 0x1C,
			0x1D, 0x1E, 0x1F, 0x20,
			0x00, 0x00, 0x16, 0x00, // CRC, ETX, checksum
		}

		result := validator.ValidatePacket(data, "generic", make(map[string]interface{}))
		assert.True(t, result.Valid)
		assert.Empty(t, result.Errors)
	})

	t.Run("Non-Standard Header Format", func(t *testing.T) {
		// Python doesn't validate header format, so this should still be valid
		data := []byte{
			0x69, 0x20, 0x00, 0x68, // Non-standard first byte
			0x01, 0x02, 0x03, 0x04,
			0x05, 0x06, 0x07, 0x08,
			0x00, 0x00, 0x16, 0x00,
		}

		result := validator.ValidatePacket(data, "generic", make(map[string]interface{}))
		assert.True(t, result.Valid) // Should be valid now (Python compatibility)
		assert.Empty(t, result.Errors)
	})

	t.Run("Too Short Packet", func(t *testing.T) {
		data := []byte{0x68, 0x01, 0x02}

		result := validator.ValidatePacket(data, "generic", make(map[string]interface{}))
		assert.False(t, result.Valid)
		assert.NotEmpty(t, result.Errors)
		assert.Contains(t, result.Errors[0].Message, "packet too short")
	})

	t.Run("Large Packet No Warning", func(t *testing.T) {
		// Python doesn't check maximum packet size, so no warning should be generated
		data := make([]byte, 2100)
		data[0] = 0x68
		data[3] = 0x68
		data[len(data)-2] = 0x16

		result := validator.ValidatePacket(data, "generic", make(map[string]interface{}))
		assert.True(t, result.Valid) // Should be valid
		assert.Empty(t, result.Warnings) // No size warnings in Python compatibility mode
	})
}

func TestAdvancedValidator_ValidateFieldData(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	validator := NewAdvancedValidator(ValidationLevelStandard, logger)

	t.Run("Valid Serial Number", func(t *testing.T) {
		fields := map[string]interface{}{
			"datalogserial": "TESTSERIAL123",
		}

		result := validator.ValidateFieldData(fields, make(map[string]interface{}))
		assert.True(t, result.Valid)
		assert.Empty(t, result.Errors)
	})

	t.Run("Invalid Serial Number Type", func(t *testing.T) {
		fields := map[string]interface{}{
			"datalogserial": 12345, // Should be string
		}

		result := validator.ValidateFieldData(fields, make(map[string]interface{}))
		assert.False(t, result.Valid)
		assert.NotEmpty(t, result.Errors)
		assert.Contains(t, result.Errors[0].Message, "not a string")
	})

	t.Run("Valid Timestamp", func(t *testing.T) {
		fields := map[string]interface{}{
			"time": time.Now().Format("2006-01-02 15:04:05"),
		}

		result := validator.ValidateFieldData(fields, make(map[string]interface{}))
		assert.True(t, result.Valid)
		assert.Empty(t, result.Errors)
	})

	t.Run("Future Timestamp", func(t *testing.T) {
		futureTime := time.Now().Add(48 * time.Hour)
		fields := map[string]interface{}{
			"time": futureTime.Format("2006-01-02 15:04:05"),
		}

		result := validator.ValidateFieldData(fields, make(map[string]interface{}))
		assert.False(t, result.Valid)
		assert.NotEmpty(t, result.Errors)
		assert.Contains(t, result.Errors[0].Message, "too far in the future")
	})

	t.Run("Valid Power Value", func(t *testing.T) {
		fields := map[string]interface{}{
			"pac": 5000.0, // 5kW - reasonable
		}

		result := validator.ValidateFieldData(fields, make(map[string]interface{}))
		assert.True(t, result.Valid)
		assert.Empty(t, result.Errors)
		assert.Empty(t, result.Warnings)
	})

	t.Run("Negative Power Warning", func(t *testing.T) {
		fields := map[string]interface{}{
			"pac": -1000.0,
		}

		result := validator.ValidateFieldData(fields, make(map[string]interface{}))
		assert.True(t, result.Valid) // Warning, not error
		assert.Empty(t, result.Errors)
		assert.NotEmpty(t, result.Warnings)
		assert.Contains(t, result.Warnings[0].Message, "negative power")
	})

	t.Run("High Power Warning", func(t *testing.T) {
		fields := map[string]interface{}{
			"pac": 150000.0, // 150kW - unusually high for residential
		}

		result := validator.ValidateFieldData(fields, make(map[string]interface{}))
		assert.True(t, result.Valid) // Warning, not error
		assert.Empty(t, result.Errors)
		assert.NotEmpty(t, result.Warnings)
		assert.Contains(t, result.Warnings[0].Message, "unusually high power")
	})
}

func TestAdvancedValidator_ProtocolV6Rules(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	validator := NewAdvancedValidator(ValidationLevelStandard, logger)

	t.Run("V6 Length Consistency", func(t *testing.T) {
		// Create packet with correct length field
		data := []byte{
			0x68, 0x10, 0x00, 0x68, // Header: SOH, length=16, SOH
			0x01, 0x02, 0x03, 0x04, // 16 bytes of data
			0x05, 0x06, 0x07, 0x08,
			0x09, 0x0A, 0x0B, 0x0C,
			0x0D, 0x0E, 0x0F, 0x10,
			0x00, 0x00, 0x16, 0x00, // CRC, ETX, checksum
		}

		result := validator.ValidatePacket(data, protocol.ProtocolInverterWrite, make(map[string]interface{}))
		// This should pass basic validation but may have warnings
		assert.NotEmpty(t, result)

		// Now test with incorrect length field
		data[1] = 0x08 // Declare length as 8 instead of 16

		result = validator.ValidatePacket(data, protocol.ProtocolInverterWrite, make(map[string]interface{}))
		assert.False(t, result.Valid)
		assert.NotEmpty(t, result.Errors)
		assert.Contains(t, result.Errors[0].Message, "length mismatch")
	})
}

func TestAdvancedValidator_PatternDetection(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	validator := NewAdvancedValidator(ValidationLevelStrict, logger)

	t.Run("Repeated Pattern Detection", func(t *testing.T) {
		// Create data with repeated pattern
		pattern := []byte{0xAA, 0xBB, 0xCC, 0xDD}
		data := []byte{0x68, 0x20, 0x00, 0x68} // Header

		// Repeat pattern multiple times
		for i := 0; i < 8; i++ {
			data = append(data, pattern...)
		}
		data = append(data, []byte{0x00, 0x00, 0x16, 0x00}...) // Footer

		result := validator.ValidatePacket(data, protocol.ProtocolInverterWrite, make(map[string]interface{}))
		// We expect warnings due to the pattern
		assert.NotEmpty(t, result.Warnings)
		assert.Contains(t, result.Warnings[0].Message, "repeated byte pattern")
	})

	t.Run("Uniform Pattern Detection", func(t *testing.T) {
		// Create data with uniform bytes
		data := []byte{0x68, 0x20, 0x00, 0x68} // Header

		// Add 32 bytes of same value
		for i := 0; i < 32; i++ {
			data = append(data, 0xFF)
		}
		data = append(data, []byte{0x00, 0x00, 0x16, 0x00}...) // Footer

		result := validator.ValidatePacket(data, protocol.ProtocolInverterWrite, make(map[string]interface{}))
		// We expect warnings due to the uniform pattern
		assert.NotEmpty(t, result.Warnings)
		// Should contain either "uniform" or "repeated" pattern warning
		warningMsg := result.Warnings[0].Message
		assert.True(t, strings.Contains(warningMsg, "uniform byte pattern") ||
			strings.Contains(warningMsg, "repeated byte pattern"))
	})
}

func TestAdvancedValidator_CustomRules(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	validator := NewAdvancedValidator(ValidationLevelStandard, logger)

	t.Run("Add Custom Protocol Rule", func(t *testing.T) {
		customRule := &ProtocolRule{
			Name:        "custom_test_rule",
			Description: "Test custom protocol rule",
			Protocol:    "test",
			Level:       ValidationLevelBasic,
			Check: func(data []byte, metadata map[string]interface{}) *ValidationError {
				if len(data) > 100 {
					return &ValidationError{
						Type:     "custom",
						Severity: "warning",
						Message:  "custom rule triggered",
						Field:    "custom_field",
					}
				}
				return nil
			},
		}

		validator.AddProtocolRule("test", customRule)

		// Test the custom rule
		data := make([]byte, 150)
		result := validator.ValidatePacket(data, "test", make(map[string]interface{}))

		require.NotEmpty(t, result.Warnings)
		assert.Contains(t, result.Warnings[0].Message, "custom rule triggered")
	})

	t.Run("Add Custom Data Rule", func(t *testing.T) {
		customRule := &DataIntegrityRule{
			Name:        "custom_data_rule",
			Description: "Test custom data rule",
			Field:       "custom_field",
			Level:       ValidationLevelBasic,
			Check: func(value interface{}, context map[string]interface{}) *ValidationError {
				if str, ok := value.(string); ok && str == "invalid" {
					return &ValidationError{
						Type:     "custom",
						Severity: "error",
						Message:  "custom data rule triggered",
						Field:    "custom_field",
					}
				}
				return nil
			},
		}

		validator.AddDataRule(customRule)

		// Test the custom rule
		fields := map[string]interface{}{
			"custom_field": "invalid",
		}

		result := validator.ValidateFieldData(fields, make(map[string]interface{}))
		assert.False(t, result.Valid)
		require.NotEmpty(t, result.Errors)
		assert.Contains(t, result.Errors[0].Message, "custom data rule triggered")
	})
}

func TestAdvancedValidator_Statistics(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	validator := NewAdvancedValidator(ValidationLevelStandard, logger)

	// Perform some validations
	data := []byte{0x68, 0x01, 0x02} // Too short - will cause error
	validator.ValidatePacket(data, "generic", make(map[string]interface{}))

	fields := map[string]interface{}{
		"pac": -100.0, // Negative power - will cause warning
	}
	validator.ValidateFieldData(fields, make(map[string]interface{}))

	stats := validator.GetStatistics()

	assert.Equal(t, int64(1), stats["validations_performed"])
	assert.Equal(t, int64(1), stats["errors_found"])
	assert.Equal(t, int64(1), stats["warnings_found"])
	assert.Equal(t, "standard", stats["validation_level"])
}

func TestAdvancedValidator_ValidationLevelChange(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	validator := NewAdvancedValidator(ValidationLevelBasic, logger)

	assert.Equal(t, ValidationLevelBasic, validator.level)

	validator.SetValidationLevel(ValidationLevelStrict)
	assert.Equal(t, ValidationLevelStrict, validator.level)
}

func TestHasRepeatedPattern(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	validator := NewAdvancedValidator(ValidationLevelBasic, logger)

	t.Run("With Repeated Pattern", func(t *testing.T) {
		// Create data with repeated 4-byte pattern
		data := []byte{0xAA, 0xBB, 0xCC, 0xDD, 0xAA, 0xBB, 0xCC, 0xDD, 0xAA, 0xBB, 0xCC, 0xDD, 0xAA, 0xBB, 0xCC, 0xDD}
		assert.True(t, validator.hasRepeatedPattern(data, 16))
	})

	t.Run("Without Repeated Pattern", func(t *testing.T) {
		data := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10}
		assert.False(t, validator.hasRepeatedPattern(data, 16))
	})

	t.Run("Data Too Short", func(t *testing.T) {
		data := []byte{0x01, 0x02}
		assert.False(t, validator.hasRepeatedPattern(data, 16))
	})
}

func TestHasUniformPattern(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	validator := NewAdvancedValidator(ValidationLevelBasic, logger)

	t.Run("With Uniform Pattern", func(t *testing.T) {
		data := make([]byte, 20)
		for i := range data {
			data[i] = 0xFF
		}
		assert.True(t, validator.hasUniformPattern(data))
	})

	t.Run("Without Uniform Pattern", func(t *testing.T) {
		data := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10}
		assert.False(t, validator.hasUniformPattern(data))
	})

	t.Run("Data Too Short", func(t *testing.T) {
		data := []byte{0xFF, 0xFF, 0xFF}
		assert.False(t, validator.hasUniformPattern(data))
	})
}

// Benchmark tests
func BenchmarkValidatePacket(b *testing.B) {
	logger := zerolog.New(zerolog.NewTestWriter(b))
	validator := NewAdvancedValidator(ValidationLevelStandard, logger)

	data := []byte{
		0x68, 0x20, 0x00, 0x68,
		0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
		0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10,
		0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18,
		0x19, 0x1A, 0x1B, 0x1C, 0x1D, 0x1E, 0x1F, 0x20,
		0x00, 0x00, 0x16, 0x00,
	}

	metadata := make(map[string]interface{})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		validator.ValidatePacket(data, protocol.ProtocolInverterWrite, metadata)
	}
}

func BenchmarkValidateFieldData(b *testing.B) {
	logger := zerolog.New(zerolog.NewTestWriter(b))
	validator := NewAdvancedValidator(ValidationLevelStandard, logger)

	fields := map[string]interface{}{
		"datalogserial": "TESTSERIAL123",
		"time":          time.Now().Format("2006-01-02 15:04:05"),
		"pac":           5000.0,
	}
	metadata := make(map[string]interface{})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		validator.ValidateFieldData(fields, metadata)
	}
}
