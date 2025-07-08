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

// Mock connection for testing
type mockConn struct {
	remoteAddr   net.Addr
	writeData    []byte
	writeError   error
	writeCalled  bool
}

func (m *mockConn) Read(b []byte) (n int, err error) {
	return 0, nil
}

func (m *mockConn) Write(b []byte) (n int, err error) {
	m.writeCalled = true
	m.writeData = b
	if m.writeError != nil {
		return 0, m.writeError
	}
	return len(b), nil
}

func (m *mockConn) Close() error {
	return nil
}

func (m *mockConn) LocalAddr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 5279}
}

func (m *mockConn) RemoteAddr() net.Addr {
	return m.remoteAddr
}

func (m *mockConn) SetDeadline(t time.Time) error {
	return nil
}

func (m *mockConn) SetReadDeadline(t time.Time) error {
	return nil
}

func (m *mockConn) SetWriteDeadline(t time.Time) error {
	return nil
}

func TestCommandPriorityString(t *testing.T) {
	tests := []struct {
		priority CommandPriority
		expected string
	}{
		{PriorityLow, "low"},
		{PriorityNormal, "normal"},
		{PriorityHigh, "high"},
		{PriorityUrgent, "urgent"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.priority.String())
		})
	}
}

func TestCommandTypeString(t *testing.T) {
	tests := []struct {
		cmdType  CommandType
		expected string
	}{
		{CommandTypeTimeSync, "time_sync"},
		{CommandTypePing, "ping"},
		{CommandTypeRegisterRead, "register_read"},
		{CommandTypeRegisterWrite, "register_write"},
		{CommandTypeDeviceInfo, "device_info"},
		{CommandTypeHealthCheck, "health_check"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.cmdType.String())
		})
	}
}

func TestScheduledCommand(t *testing.T) {
	now := time.Now()
	
	tests := []struct {
		name     string
		cmd      *ScheduledCommand
		isExpired bool
		shouldExecute bool
		canRetry bool
	}{
		{
			name: "not expired, ready to execute",
			cmd: &ScheduledCommand{
				ScheduledAt: now.Add(-time.Minute),
				ExpiresAt:   now.Add(time.Hour),
				Retries:     0,
				MaxRetries:  3,
			},
			isExpired:     false,
			shouldExecute: true,
			canRetry:      true,
		},
		{
			name: "expired command",
			cmd: &ScheduledCommand{
				ScheduledAt: now.Add(-time.Hour),
				ExpiresAt:   now.Add(-time.Minute),
				Retries:     0,
				MaxRetries:  3,
			},
			isExpired:     true,
			shouldExecute: false,
			canRetry:      true,
		},
		{
			name: "future command",
			cmd: &ScheduledCommand{
				ScheduledAt: now.Add(time.Hour),
				ExpiresAt:   now.Add(2 * time.Hour),
				Retries:     0,
				MaxRetries:  3,
			},
			isExpired:     false,
			shouldExecute: false,
			canRetry:      true,
		},
		{
			name: "max retries reached",
			cmd: &ScheduledCommand{
				ScheduledAt: now.Add(-time.Minute),
				ExpiresAt:   now.Add(time.Hour),
				Retries:     3,
				MaxRetries:  3,
			},
			isExpired:     false,
			shouldExecute: true,
			canRetry:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.isExpired, tt.cmd.IsExpired())
			assert.Equal(t, tt.shouldExecute, tt.cmd.ShouldExecute())
			assert.Equal(t, tt.canRetry, tt.cmd.CanRetry())
		})
	}
}

func TestCommandQueue(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	queue := NewCommandQueue(logger)

	// Test empty queue
	assert.Equal(t, 0, queue.GetQueueLength())
	assert.Nil(t, queue.Dequeue())

	// Add commands with different priorities
	now := time.Now()
	
	lowCmd := &ScheduledCommand{
		ID:          "low1",
		Type:        CommandTypePing,
		Priority:    PriorityLow,
		ScheduledAt: now.Add(-time.Minute),
		ExpiresAt:   now.Add(time.Hour),
	}
	
	highCmd := &ScheduledCommand{
		ID:          "high1",
		Type:        CommandTypeTimeSync,
		Priority:    PriorityHigh,
		ScheduledAt: now.Add(-time.Minute),
		ExpiresAt:   now.Add(time.Hour),
	}
	
	urgentCmd := &ScheduledCommand{
		ID:          "urgent1",
		Type:        CommandTypeHealthCheck,
		Priority:    PriorityUrgent,
		ScheduledAt: now.Add(-time.Minute),
		ExpiresAt:   now.Add(time.Hour),
	}

	// Enqueue in random order
	queue.Enqueue(lowCmd)
	queue.Enqueue(highCmd)
	queue.Enqueue(urgentCmd)

	assert.Equal(t, 3, queue.GetQueueLength())

	// Should dequeue in priority order
	dequeued := queue.Dequeue()
	assert.Equal(t, "urgent1", dequeued.ID)

	dequeued = queue.Dequeue()
	assert.Equal(t, "high1", dequeued.ID)

	dequeued = queue.Dequeue()
	assert.Equal(t, "low1", dequeued.ID)

	assert.Equal(t, 0, queue.GetQueueLength())
}

func TestCommandQueueCleanupExpired(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	queue := NewCommandQueue(logger)

	now := time.Now()
	
	// Add expired command
	expiredCmd := &ScheduledCommand{
		ID:          "expired1",
		Type:        CommandTypePing,
		Priority:    PriorityNormal,
		ScheduledAt: now.Add(-2 * time.Hour),
		ExpiresAt:   now.Add(-time.Hour),
	}
	
	// Add active command
	activeCmd := &ScheduledCommand{
		ID:          "active1",
		Type:        CommandTypeTimeSync,
		Priority:    PriorityNormal,
		ScheduledAt: now.Add(-time.Minute),
		ExpiresAt:   now.Add(time.Hour),
	}

	queue.Enqueue(expiredCmd)
	queue.Enqueue(activeCmd)

	assert.Equal(t, 2, queue.GetQueueLength())

	// Cleanup expired
	cleaned := queue.CleanupExpired()
	assert.Equal(t, 1, cleaned)
	assert.Equal(t, 1, queue.GetQueueLength())

	// Should only have active command
	dequeued := queue.Dequeue()
	assert.Equal(t, "active1", dequeued.ID)
}

func TestDefaultSchedulerConfig(t *testing.T) {
	config := DefaultSchedulerConfig()
	
	assert.Equal(t, time.Second, config.TickInterval)
	assert.Equal(t, 10, config.MaxConcurrent)
	assert.Equal(t, 30*time.Second, config.DefaultTimeout)
	assert.Equal(t, 3, config.DefaultRetries)
	assert.Equal(t, time.Hour, config.TimeSyncInterval)
	assert.Equal(t, 5*time.Minute, config.HealthCheckInterval)
}

func TestCommandScheduler(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	
	// Create dependencies
	sessionManager := session.NewSessionManager(time.Minute)
	commandBuilder := protocol.NewCommandBuilder()
	responseManager := protocol.NewResponseManager()
	
	// Create scheduler with fast tick for testing
	config := &SchedulerConfig{
		TickInterval:        10 * time.Millisecond,
		MaxConcurrent:       5,
		DefaultTimeout:      time.Second,
		DefaultRetries:      2,
		TimeSyncInterval:    100 * time.Millisecond,
		HealthCheckInterval: 50 * time.Millisecond,
	}
	
	scheduler := NewCommandScheduler(sessionManager, commandBuilder, responseManager, config, logger)
	
	// Test initial state
	assert.False(t, scheduler.isRunning)
	metrics := scheduler.GetMetrics()
	assert.False(t, metrics["is_running"].(bool))
	assert.Equal(t, int64(0), metrics["commands_executed"].(int64))
	
	// Start scheduler
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	
	err := scheduler.Start(ctx)
	assert.NoError(t, err)
	assert.True(t, scheduler.isRunning)
	
	// Try to start again (should fail)
	err = scheduler.Start(ctx)
	assert.Error(t, err)
	
	// Create a session
	addr := &net.TCPAddr{IP: net.ParseIP("192.168.1.100"), Port: 5279}
	conn := &mockConn{remoteAddr: addr}
	sess := sessionManager.CreateSession(conn)
	sess.SetDeviceInfo(session.DeviceTypeInverter, "TEST123456", "06", "1.0")
	
	// Schedule a command
	cmd := &ScheduledCommand{
		Type:        CommandTypeTimeSync,
		Priority:    PriorityNormal,
		SessionID:   sess.ID,
		ScheduledAt: time.Now(),
		Parameters:  make(map[string]interface{}),
	}
	
	err = scheduler.ScheduleCommand(cmd)
	assert.NoError(t, err)
	
	// Wait for command to be processed
	time.Sleep(50 * time.Millisecond)
	
	// Check metrics
	metrics = scheduler.GetMetrics()
	assert.True(t, metrics["is_running"].(bool))
	
	// Stop scheduler
	err = scheduler.Stop()
	assert.NoError(t, err)
	assert.False(t, scheduler.isRunning)
	
	// Try to stop again (should fail)
	err = scheduler.Stop()
	assert.Error(t, err)
}

func TestScheduleTimeSync(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	
	sessionManager := session.NewSessionManager(time.Minute)
	commandBuilder := protocol.NewCommandBuilder()
	responseManager := protocol.NewResponseManager()
	
	scheduler := NewCommandScheduler(sessionManager, commandBuilder, responseManager, nil, logger)
	
	// Create a session
	addr := &net.TCPAddr{IP: net.ParseIP("192.168.1.100"), Port: 5279}
	conn := &mockConn{remoteAddr: addr}
	sess := sessionManager.CreateSession(conn)
	
	// Schedule time sync
	err := scheduler.ScheduleTimeSync(sess.ID, PriorityHigh)
	assert.NoError(t, err)
	
	// Check queue
	assert.Equal(t, 1, scheduler.queue.GetQueueLength())
	
	// Dequeue and verify
	cmd := scheduler.queue.Dequeue()
	require.NotNil(t, cmd)
	assert.Equal(t, CommandTypeTimeSync, cmd.Type)
	assert.Equal(t, PriorityHigh, cmd.Priority)
	assert.Equal(t, sess.ID, cmd.SessionID)
}

func TestScheduleHealthCheck(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	
	sessionManager := session.NewSessionManager(time.Minute)
	commandBuilder := protocol.NewCommandBuilder()
	responseManager := protocol.NewResponseManager()
	
	scheduler := NewCommandScheduler(sessionManager, commandBuilder, responseManager, nil, logger)
	
	// Create a session
	addr := &net.TCPAddr{IP: net.ParseIP("192.168.1.100"), Port: 5279}
	conn := &mockConn{remoteAddr: addr}
	sess := sessionManager.CreateSession(conn)
	
	// Schedule health check
	err := scheduler.ScheduleHealthCheck(sess.ID, PriorityLow)
	assert.NoError(t, err)
	
	// Check queue
	assert.Equal(t, 1, scheduler.queue.GetQueueLength())
	
	// Dequeue and verify
	cmd := scheduler.queue.Dequeue()
	require.NotNil(t, cmd)
	assert.Equal(t, CommandTypeHealthCheck, cmd.Type)
	assert.Equal(t, PriorityLow, cmd.Priority)
	assert.Equal(t, sess.ID, cmd.SessionID)
}

func TestExecuteTimeSyncCommand(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	
	sessionManager := session.NewSessionManager(time.Minute)
	commandBuilder := protocol.NewCommandBuilder()
	responseManager := protocol.NewResponseManager()
	
	scheduler := NewCommandScheduler(sessionManager, commandBuilder, responseManager, nil, logger)
	
	// Create a session with mock connection
	addr := &net.TCPAddr{IP: net.ParseIP("192.168.1.100"), Port: 5279}
	mockConn := &mockConn{remoteAddr: addr}
	sess := sessionManager.CreateSession(mockConn)
	sess.SetDeviceInfo(session.DeviceTypeInverter, "TEST123456", "06", "1.0")
	
	// Create command
	cmd := &ScheduledCommand{
		ID:        "test1",
		Type:      CommandTypeTimeSync,
		SessionID: sess.ID,
		Parameters: make(map[string]interface{}),
	}
	
	// Execute command
	err := scheduler.executeTimeSyncCommand(context.Background(), cmd, sess)
	assert.NoError(t, err)
	
	// Verify mock connection was called
	assert.True(t, mockConn.writeCalled)
	assert.Greater(t, len(mockConn.writeData), 0)
	
	// Verify command result
	require.NotNil(t, cmd.Result)
	result := cmd.Result.(map[string]interface{})
	assert.Contains(t, result, "command")
	assert.Contains(t, result, "bytes_sent")
}

func TestExecuteHealthCheckCommand(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	
	sessionManager := session.NewSessionManager(time.Minute)
	commandBuilder := protocol.NewCommandBuilder()
	responseManager := protocol.NewResponseManager()
	
	scheduler := NewCommandScheduler(sessionManager, commandBuilder, responseManager, nil, logger)
	
	// Create a session with mock connection
	addr := &net.TCPAddr{IP: net.ParseIP("192.168.1.100"), Port: 5279}
	mockConn := &mockConn{remoteAddr: addr}
	sess := sessionManager.CreateSession(mockConn)
	sess.SetDeviceInfo(session.DeviceTypeInverter, "TEST123456", "06", "1.0")
	
	// Create command
	cmd := &ScheduledCommand{
		ID:        "test1",
		Type:      CommandTypeHealthCheck,
		SessionID: sess.ID,
		Parameters: make(map[string]interface{}),
	}
	
	// Execute command
	err := scheduler.executeHealthCheckCommand(context.Background(), cmd, sess)
	assert.NoError(t, err)
	
	// Verify mock connection was called
	assert.True(t, mockConn.writeCalled)
	assert.Greater(t, len(mockConn.writeData), 0)
	
	// Verify command result
	require.NotNil(t, cmd.Result)
	result := cmd.Result.(map[string]interface{})
	assert.Contains(t, result, "command")
	assert.Contains(t, result, "bytes_sent")
}

func TestCommandRetry(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	
	sessionManager := session.NewSessionManager(time.Minute)
	commandBuilder := protocol.NewCommandBuilder()
	responseManager := protocol.NewResponseManager()
	
	scheduler := NewCommandScheduler(sessionManager, commandBuilder, responseManager, nil, logger)
	
	// Create command that can be retried
	cmd := &ScheduledCommand{
		ID:         "test1",
		Type:       CommandTypeTimeSync,
		SessionID:  "non-existent",
		Retries:    0,
		MaxRetries: 3,
		Parameters: make(map[string]interface{}),
	}
	
	// Retry command
	scheduler.retryCommand(cmd)
	
	// Verify retry state
	assert.Equal(t, 1, cmd.Retries)
	assert.Nil(t, cmd.ExecutedAt)
	assert.True(t, cmd.ScheduledAt.After(time.Now().Add(-time.Second)))
	assert.Equal(t, int64(1), scheduler.commandsRetried)
}

func TestGenerateCommandID(t *testing.T) {
	id1 := generateCommandID()
	id2 := generateCommandID()
	
	assert.NotEmpty(t, id1)
	assert.NotEmpty(t, id2)
	assert.NotEqual(t, id1, id2)
	assert.Contains(t, id1, "cmd_")
	assert.Contains(t, id2, "cmd_")
}

// Benchmark tests
func BenchmarkCommandQueueEnqueue(b *testing.B) {
	logger := zerolog.New(zerolog.NewTestWriter(b))
	queue := NewCommandQueue(logger)
	
	cmd := &ScheduledCommand{
		ID:          "benchmark",
		Type:        CommandTypeTimeSync,
		Priority:    PriorityNormal,
		ScheduledAt: time.Now(),
		ExpiresAt:   time.Now().Add(time.Hour),
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		queue.Enqueue(cmd)
	}
}

func BenchmarkCommandQueueDequeue(b *testing.B) {
	logger := zerolog.New(zerolog.NewTestWriter(b))
	queue := NewCommandQueue(logger)
	
	// Pre-populate queue
	for i := 0; i < b.N; i++ {
		cmd := &ScheduledCommand{
			ID:          generateCommandID(),
			Type:        CommandTypeTimeSync,
			Priority:    PriorityNormal,
			ScheduledAt: time.Now(),
			ExpiresAt:   time.Now().Add(time.Hour),
		}
		queue.Enqueue(cmd)
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		queue.Dequeue()
	}
}

func BenchmarkGenerateCommandID(b *testing.B) {
	for i := 0; i < b.N; i++ {
		generateCommandID()
	}
}
