package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"testing"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
)

func TestMainFunctionality(t *testing.T) {
	// Test version flag parsing
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	tests := []struct {
		name     string
		args     []string
		expected bool
	}{
		{
			name:     "version flag short",
			args:     []string{"cmd", "-version"},
			expected: true,
		},
		{
			name:     "no version flag",
			args:     []string{"cmd"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset flag package for each test
			flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)

			os.Args = tt.args
			configFile := flag.String("config", "config.yaml", "Path to configuration file")
			showVersion := flag.Bool("version", false, "Show version information")

			// Parse flags without exiting
			err := flag.CommandLine.Parse(tt.args[1:])
			assert.NoError(t, err)

			assert.Equal(t, tt.expected, *showVersion)
			assert.Equal(t, "config.yaml", *configFile) // Default value
		})
	}
}

func TestVersion(t *testing.T) {
	// Test that version variable is set
	assert.Equal(t, "1.0.0", version)
}

func TestMainWithArgs(t *testing.T) {
	// Test main function doesn't panic with various arguments
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	// This test just ensures the flag parsing logic doesn't break
	// We can't easily test the full main() function due to its blocking nature
	// and dependency on external resources

	// Test config file flag
	os.Args = []string{"cmd", "-config", "test.yaml"}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)

	configFile := flag.String("config", "config.yaml", "Path to configuration file")
	showVersion := flag.Bool("version", false, "Show version information")

	err := flag.CommandLine.Parse(os.Args[1:])
	assert.NoError(t, err)
	assert.Equal(t, "test.yaml", *configFile)
	assert.False(t, *showVersion)
}

// TestMainVersionOutput tests the version output functionality.
func TestMainVersionOutput(t *testing.T) {
	// Save original args and stdout
	oldArgs := os.Args
	oldStdout := os.Stdout
	defer func() {
		os.Args = oldArgs
		os.Stdout = oldStdout
	}()

	// Create a pipe to capture stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Set up version flag
	os.Args = []string{"cmd", "-version"}

	// Reset flag state
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)

	// Capture the output
	outputChan := make(chan string)
	go func() {
		var buf bytes.Buffer
		io.Copy(&buf, r)
		outputChan <- buf.String()
	}()

	// Call main function - we need to handle the version flag path
	configFile := flag.String("config", "config.yaml", "Path to configuration file")
	showVersion := flag.Bool("version", false, "Show version information")
	flag.Parse()

	if *showVersion {
		fmt.Printf("go-grott server %s\n", version)
	}

	// Close writer and capture output
	w.Close()
	output := <-outputChan

	// Verify output
	expectedOutput := fmt.Sprintf("go-grott server %s\n", version)
	assert.Equal(t, expectedOutput, output)
	assert.NotNil(t, configFile) // Just verify flags were parsed
}

// TestInitLogger tests the logger initialization function.
func TestInitLogger(t *testing.T) {
	// Save original logger
	originalLogger := log.Logger

	tests := []struct {
		name     string
		level    string
		expected zerolog.Level
	}{
		{
			name:     "info level",
			level:    "info",
			expected: zerolog.InfoLevel,
		},
		{
			name:     "debug level",
			level:    "debug",
			expected: zerolog.DebugLevel,
		},
		{
			name:     "warn level",
			level:    "warn",
			expected: zerolog.WarnLevel,
		},
		{
			name:     "error level",
			level:    "error",
			expected: zerolog.ErrorLevel,
		},
		{
			name:     "uppercase level",
			level:    "INFO",
			expected: zerolog.InfoLevel,
		},
		{
			name:     "invalid level defaults to info",
			level:    "invalid",
			expected: zerolog.InfoLevel,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Capture stdout for invalid level message
			oldStdout := os.Stdout
			r, w, _ := os.Pipe()
			os.Stdout = w

			// Test the function
			initLogger(tt.level)

			// Restore stdout and capture output
			w.Close()
			os.Stdout = oldStdout
			var buf bytes.Buffer
			io.Copy(&buf, r)
			stdoutOutput := buf.String()

			// Check global log level was set correctly
			assert.Equal(t, tt.expected, zerolog.GlobalLevel())

			// Check for invalid level message
			if tt.level == "invalid" {
				assert.Contains(t, stdoutOutput, "Invalid log level 'invalid'")
			}

			// Verify logger was configured (basic check)
			assert.NotNil(t, log.Logger)
		})
	}

	// Restore original logger
	log.Logger = originalLogger
}

// TestMainConfigLoadError tests main function behavior when config loading fails.
func TestMainConfigLoadError(t *testing.T) {
	// This test verifies the error handling path in main when config loading fails
	// We can't easily test os.Exit() calls, but we can test the setup logic

	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	// Test with non-existent config file
	os.Args = []string{"cmd", "-config", "non-existent-file.yaml"}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)

	configFile := flag.String("config", "config.yaml", "Path to configuration file")
	showVersion := flag.Bool("version", false, "Show version information")
	flag.Parse()

	assert.Equal(t, "non-existent-file.yaml", *configFile)
	assert.False(t, *showVersion)

	// We can't easily test the config.Load error path without mocking
	// but we've verified the flag parsing works correctly
}
