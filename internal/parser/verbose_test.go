package parser

import (
	"bytes"
	"context"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/resident-x/go-grott/internal/config"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

// TestVerboseLogging tests the parser with verbose logging to see what happens to the input.
func TestVerboseLogging(t *testing.T) {
	// Sample data from grotttest.py with intentional space character
	sampleHexData := "00320006010101040d222c4559454c74412d7761747447726f7761747447726f7761747447723e3a23464c75415d4150747447726f7761747447726f7761747447726f7774767d4b7c65756174746b726e776161064e776f6061746135726f7761747447726f7772cf67ce7b2a7774747454c96f7761747447726f7761747447726f7761747452726fd0d97796a77e6e7a61747447726f7761747447726f77617474477ce977617474475f6f2e2f547447726f776174624772df4161747447726f77617474f7446f7761747447726f7761747447726f7761747447786f776b287d457a9777607eb407066ef46ff37652 7ce87762747747656f7761747447726f6d03"

	// Remove any spaces in the hex string (part of the test to ensure parser can handle spaces)
	sampleHexData = strings.ReplaceAll(sampleHexData, " ", "")

	// Create a buffer to capture log output
	var logOutput bytes.Buffer

	// Configure zerolog to write to our buffer
	logger := zerolog.New(&logOutput).Level(zerolog.TraceLevel)

	// Create a test config with test layouts directory
	testConfig := &config.Config{}

	// Create parser
	parser, err := NewParser(testConfig)
	require.NoError(t, err, "Failed to create parser")

	// Set custom logger that writes to our buffer
	parser.SetCustomLogger(&logger)

	// Convert hex string to bytes
	data, err := hex.DecodeString(sampleHexData)
	require.NoError(t, err, "Failed to decode hex data")

	// Parse the data - we expect an error but want to see logs
	result, parseErr := parser.Parse(context.Background(), data)

	// Log the results
	if parseErr != nil {
		t.Logf("Parse error: %v", parseErr)
	} else {
		t.Logf("Parse successful: %+v", *result)
	}

	// Print the logs to the test output
	t.Logf("Parser logs for sample data:\n%s", logOutput.String())
}
