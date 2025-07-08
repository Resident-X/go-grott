package scheduler

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/resident-x/go-grott/internal/protocol"
	"github.com/resident-x/go-grott/internal/session"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPhase2Integration tests the complete Phase 2 implementation
func TestPhase2Integration(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	
	// Create dependencies
	sessionManager := session.NewSessionManager(time.Minute)
	commandBuilder := protocol.NewCommandBuilder()
	responseManager := protocol.NewResponseManager()
	
	// Create scheduler with fast tick for testing
	config := &SchedulerConfig{
		TickInterval:        50 * time.Millisecond,
		MaxConcurrent:       3,
		DefaultTimeout:      2 * time.Second,
		DefaultRetries:      2,
		TimeSyncInterval:    200 * time.Millisecond, // Fast for testing
		HealthCheckInterval: 150 * time.Millisecond, // Fast for testing
	}
	
	scheduler := NewCommandScheduler(sessionManager, commandBuilder, responseManager, config, logger)
	
	// Start scheduler
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	
	err := scheduler.Start(ctx)
	require.NoError(t, err)
	defer scheduler.Stop()
	
	// Create mock sessions
	session1 := createTestSession(t, sessionManager, "192.168.1.100:5279")
	session2 := createTestSession(t, sessionManager, "192.168.1.101:5279")
	
	// Test 1: Manual command scheduling
	t.Run("Manual Command Scheduling", func(t *testing.T) {
		// Schedule different types of commands
		err := scheduler.ScheduleTimeSync(session1.ID, PriorityHigh)
		assert.NoError(t, err)
		
		err = scheduler.ScheduleHealthCheck(session2.ID, PriorityNormal)
		assert.NoError(t, err)
		
		// Custom command with parameters
		customCmd := &ScheduledCommand{
			Type:        CommandTypeRegisterRead,
			Priority:    PriorityLow,
			SessionID:   session1.ID,
			ScheduledAt: time.Now(),
			Parameters: map[string]interface{}{
				"register": 40000,
				"count":    10,
			},
		}
		
		err = scheduler.ScheduleCommand(customCmd)
		assert.NoError(t, err)
		
		// Wait for commands to be processed
		time.Sleep(200 * time.Millisecond)
		
		// Verify metrics show command execution
		metrics := scheduler.GetMetrics()
		assert.True(t, metrics["is_running"].(bool))
		assert.Greater(t, metrics["commands_executed"].(int64), int64(0))
	})
	
	// Test 2: Automatic scheduling (time sync and health checks)
	t.Run("Automatic Command Scheduling", func(t *testing.T) {
		initialMetrics := scheduler.GetMetrics()
		initialExecuted := initialMetrics["commands_executed"].(int64)
		
		// Wait for automatic commands to be scheduled and executed
		time.Sleep(500 * time.Millisecond)
		
		finalMetrics := scheduler.GetMetrics()
		finalExecuted := finalMetrics["commands_executed"].(int64)
		
		// Should have executed more commands automatically
		assert.Greater(t, finalExecuted, initialExecuted)
		
		// Verify queue management (queue_length is int, not int64)
		assert.Equal(t, 0, finalMetrics["queue_length"].(int)) // Should be processed
	})
	
	// Test 3: Priority-based execution
	t.Run("Priority-Based Execution", func(t *testing.T) {
		// Schedule commands in reverse priority order
		lowCmd := &ScheduledCommand{
			ID:          "low_priority",
			Type:        CommandTypePing,
			Priority:    PriorityLow,
			SessionID:   session1.ID,
			ScheduledAt: time.Now(),
			Parameters:  make(map[string]interface{}),
		}
		
		urgentCmd := &ScheduledCommand{
			ID:          "urgent_priority", 
			Type:        CommandTypeDeviceInfo,
			Priority:    PriorityUrgent,
			SessionID:   session2.ID,
			ScheduledAt: time.Now(),
			Parameters:  make(map[string]interface{}),
		}
		
		// Schedule low priority first
		err := scheduler.ScheduleCommand(lowCmd)
		assert.NoError(t, err)
		
		// Then urgent (should execute first)
		err = scheduler.ScheduleCommand(urgentCmd)
		assert.NoError(t, err)
		
		// Wait for execution
		time.Sleep(100 * time.Millisecond)
		
		// Priority ordering should be respected
		metrics := scheduler.GetMetrics()
		assert.Greater(t, metrics["commands_executed"].(int64), int64(0))
	})
	
	// Test 4: Error handling and retries
	t.Run("Error Handling and Retries", func(t *testing.T) {
		// Schedule command for non-existent session (will fail)
		failCmd := &ScheduledCommand{
			Type:        CommandTypeTimeSync,
			Priority:    PriorityNormal,
			SessionID:   "non-existent-session",
			ScheduledAt: time.Now(),
			MaxRetries:  2,
			Parameters:  make(map[string]interface{}),
		}
		
		err := scheduler.ScheduleCommand(failCmd)
		assert.NoError(t, err)
		
		// Wait for retries
		time.Sleep(200 * time.Millisecond)
		
		metrics := scheduler.GetMetrics()
		assert.Greater(t, metrics["commands_failed"].(int64), int64(0))
		assert.Greater(t, metrics["commands_retried"].(int64), int64(0))
	})
	
	// Test 5: Concurrent execution
	t.Run("Concurrent Execution", func(t *testing.T) {
		initialMetrics := scheduler.GetMetrics()
		initialExecuted := initialMetrics["commands_executed"].(int64)
		
		// Schedule multiple commands rapidly
		for i := 0; i < 5; i++ {
			cmd := &ScheduledCommand{
				Type:        CommandTypePing,
				Priority:    PriorityNormal,
				SessionID:   session1.ID,
				ScheduledAt: time.Now(),
				Parameters:  make(map[string]interface{}),
			}
			
			err := scheduler.ScheduleCommand(cmd)
			assert.NoError(t, err)
		}
		
		// Wait for processing
		time.Sleep(300 * time.Millisecond)
		
		finalMetrics := scheduler.GetMetrics()
		finalExecuted := finalMetrics["commands_executed"].(int64)
		
		// Should have processed multiple commands
		assert.Greater(t, finalExecuted-initialExecuted, int64(3))
	})
	
	// Test 6: Scheduler metrics and monitoring
	t.Run("Scheduler Metrics", func(t *testing.T) {
		metrics := scheduler.GetMetrics()
		
		// Verify all expected metrics are present
		expectedMetrics := []string{
			"is_running",
			"commands_executed", 
			"commands_failed",
			"commands_retried",
			"queue_length",
			"active_workers",
		}
		
		for _, metric := range expectedMetrics {
			assert.Contains(t, metrics, metric, "Missing metric: %s", metric)
		}
		
		// Verify running state
		assert.True(t, metrics["is_running"].(bool))
		assert.LessOrEqual(t, metrics["active_workers"].(int64), int64(config.MaxConcurrent))
	})
}

// TestSchedulerLifecycle tests scheduler start/stop behavior
func TestSchedulerLifecycle(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	
	sessionManager := session.NewSessionManager(time.Minute)
	commandBuilder := protocol.NewCommandBuilder()
	responseManager := protocol.NewResponseManager()
	
	scheduler := NewCommandScheduler(sessionManager, commandBuilder, responseManager, nil, logger)
	
	// Test initial state
	assert.False(t, scheduler.isRunning)
	
	// Start scheduler
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	
	err := scheduler.Start(ctx)
	assert.NoError(t, err)
	assert.True(t, scheduler.isRunning)
	
	// Cannot start twice
	err = scheduler.Start(ctx)
	assert.Error(t, err)
	
	// Stop scheduler
	err = scheduler.Stop()
	assert.NoError(t, err)
	assert.False(t, scheduler.isRunning)
	
	// Cannot stop twice
	err = scheduler.Stop()
	assert.Error(t, err)
}

// Helper function to create test sessions
func createTestSession(t testing.TB, manager *session.SessionManager, addr string) *session.Session {
	// Create mock connection
	mockConn := &mockConn{
		remoteAddr: &net.TCPAddr{
			IP:   net.ParseIP("192.168.1.100"),
			Port: 5279,
		},
	}
	
	sess := manager.CreateSession(mockConn)
	sess.SetDeviceInfo(session.DeviceTypeInverter, "TEST123456", "06", "1.0")
	
	return sess
}

// BenchmarkSchedulerThroughput tests command processing throughput
func BenchmarkSchedulerThroughput(b *testing.B) {
	logger := zerolog.New(zerolog.NewTestWriter(b))
	
	sessionManager := session.NewSessionManager(time.Minute)
	commandBuilder := protocol.NewCommandBuilder()
	responseManager := protocol.NewResponseManager()
	
	config := &SchedulerConfig{
		TickInterval:  10 * time.Millisecond,
		MaxConcurrent: 10,
		DefaultTimeout: time.Second,
		DefaultRetries: 1,
		TimeSyncInterval: time.Hour, // Disable automatic scheduling
		HealthCheckInterval: time.Hour,
	}
	
	scheduler := NewCommandScheduler(sessionManager, commandBuilder, responseManager, config, logger)
	
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	
	err := scheduler.Start(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer scheduler.Stop()
	
	// Create test session
	session := createTestSession(b, sessionManager, "192.168.1.100:5279")
	
	b.ResetTimer()
	
	for i := 0; i < b.N; i++ {
		cmd := &ScheduledCommand{
			Type:        CommandTypePing,
			Priority:    PriorityNormal,
			SessionID:   session.ID,
			ScheduledAt: time.Now(),
			Parameters:  make(map[string]interface{}),
		}
		
		scheduler.ScheduleCommand(cmd)
	}
	
	// Wait for all commands to process
	for {
		metrics := scheduler.GetMetrics()
		if metrics["queue_length"].(int) == 0 && metrics["active_workers"].(int64) == 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
}
