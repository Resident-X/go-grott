// Package scheduler provides command scheduling and queue management for device communication.
package scheduler

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/resident-x/go-grott/internal/protocol"
	"github.com/resident-x/go-grott/internal/session"
	"github.com/rs/zerolog"
)

// Global counter for unique command IDs
var commandIDCounter uint64

// CommandPriority defines the priority level for commands.
type CommandPriority int

const (
	PriorityLow CommandPriority = iota
	PriorityNormal
	PriorityHigh
	PriorityUrgent
)

// String returns the string representation of the command priority.
func (p CommandPriority) String() string {
	switch p {
	case PriorityLow:
		return "low"
	case PriorityNormal:
		return "normal"
	case PriorityHigh:
		return "high"
	case PriorityUrgent:
		return "urgent"
	default:
		return "unknown"
	}
}

// CommandType defines the type of command to execute.
type CommandType int

const (
	CommandTypeTimeSync CommandType = iota
	CommandTypePing
	CommandTypeRegisterRead
	CommandTypeRegisterWrite
	CommandTypeDeviceInfo
	CommandTypeHealthCheck
)

// String returns the string representation of the command type.
func (ct CommandType) String() string {
	switch ct {
	case CommandTypeTimeSync:
		return "time_sync"
	case CommandTypePing:
		return "ping"
	case CommandTypeRegisterRead:
		return "register_read"
	case CommandTypeRegisterWrite:
		return "register_write"
	case CommandTypeDeviceInfo:
		return "device_info"
	case CommandTypeHealthCheck:
		return "health_check"
	default:
		return "unknown"
	}
}

// ScheduledCommand represents a command to be executed at a specific time.
type ScheduledCommand struct {
	ID          string
	Type        CommandType
	Priority    CommandPriority
	SessionID   string
	Protocol    string
	Parameters  map[string]interface{}
	ScheduledAt time.Time
	ExpiresAt   time.Time
	Retries     int
	MaxRetries  int
	CreatedAt   time.Time
	ExecutedAt  *time.Time
	CompletedAt *time.Time
	Error       error
	Result      interface{}
}

// IsExpired returns true if the command has expired.
func (sc *ScheduledCommand) IsExpired() bool {
	return time.Now().After(sc.ExpiresAt)
}

// ShouldExecute returns true if the command should be executed now.
func (sc *ScheduledCommand) ShouldExecute() bool {
	return !sc.IsExpired() && time.Now().After(sc.ScheduledAt) && sc.ExecutedAt == nil
}

// CanRetry returns true if the command can be retried.
func (sc *ScheduledCommand) CanRetry() bool {
	return sc.Retries < sc.MaxRetries
}

// CommandQueue manages a priority queue of scheduled commands.
type CommandQueue struct {
	commands map[CommandPriority][]*ScheduledCommand
	mutex    sync.RWMutex
	logger   zerolog.Logger
}

// NewCommandQueue creates a new command queue.
func NewCommandQueue(logger zerolog.Logger) *CommandQueue {
	return &CommandQueue{
		commands: make(map[CommandPriority][]*ScheduledCommand),
		logger:   logger.With().Str("component", "command_queue").Logger(),
	}
}

// Enqueue adds a command to the queue.
func (cq *CommandQueue) Enqueue(cmd *ScheduledCommand) {
	cq.mutex.Lock()
	defer cq.mutex.Unlock()

	cq.commands[cmd.Priority] = append(cq.commands[cmd.Priority], cmd)
	cq.logger.Debug().
		Str("command_id", cmd.ID).
		Str("type", cmd.Type.String()).
		Str("priority", cmd.Priority.String()).
		Msg("Command enqueued")
}

// Dequeue removes and returns the highest priority command that's ready to execute.
func (cq *CommandQueue) Dequeue() *ScheduledCommand {
	cq.mutex.Lock()
	defer cq.mutex.Unlock()

	// Check priorities from highest to lowest
	priorities := []CommandPriority{PriorityUrgent, PriorityHigh, PriorityNormal, PriorityLow}

	for _, priority := range priorities {
		commands := cq.commands[priority]
		for i, cmd := range commands {
			if cmd.ShouldExecute() {
				// Remove from queue
				cq.commands[priority] = append(commands[:i], commands[i+1:]...)
				cq.logger.Debug().
					Str("command_id", cmd.ID).
					Str("type", cmd.Type.String()).
					Msg("Command dequeued")
				return cmd
			}
		}
	}

	return nil
}

// CleanupExpired removes expired commands from the queue.
func (cq *CommandQueue) CleanupExpired() int {
	cq.mutex.Lock()
	defer cq.mutex.Unlock()

	cleaned := 0
	for priority := range cq.commands {
		var active []*ScheduledCommand
		for _, cmd := range cq.commands[priority] {
			if !cmd.IsExpired() {
				active = append(active, cmd)
			} else {
				cleaned++
				cq.logger.Debug().
					Str("command_id", cmd.ID).
					Str("type", cmd.Type.String()).
					Msg("Expired command removed")
			}
		}
		cq.commands[priority] = active
	}

	if cleaned > 0 {
		cq.logger.Info().Int("count", cleaned).Msg("Cleaned up expired commands")
	}

	return cleaned
}

// GetQueueLength returns the total number of commands in the queue.
func (cq *CommandQueue) GetQueueLength() int {
	cq.mutex.RLock()
	defer cq.mutex.RUnlock()

	total := 0
	for _, commands := range cq.commands {
		total += len(commands)
	}
	return total
}

// GetQueueLengthByPriority returns the number of commands for each priority.
func (cq *CommandQueue) GetQueueLengthByPriority() map[CommandPriority]int {
	cq.mutex.RLock()
	defer cq.mutex.RUnlock()

	result := make(map[CommandPriority]int)
	for priority, commands := range cq.commands {
		result[priority] = len(commands)
	}
	return result
}

// CommandScheduler manages the execution of scheduled commands.
type CommandScheduler struct {
	queue           *CommandQueue
	sessionManager  *session.SessionManager
	commandBuilder  *protocol.CommandBuilder
	responseManager *protocol.ResponseManager
	logger          zerolog.Logger
	ticker          *time.Ticker
	stopChan        chan struct{}
	wg              sync.WaitGroup
	isRunning       bool
	mutex           sync.RWMutex

	// Configuration
	tickInterval        time.Duration
	maxConcurrent       int
	defaultTimeout      time.Duration
	defaultRetries      int
	timeSyncInterval    time.Duration
	healthCheckInterval time.Duration

	// Metrics
	commandsExecuted int64
	commandsFailed   int64
	commandsRetried  int64
	activeWorkers    int64
}

// SchedulerConfig holds configuration for the command scheduler.
type SchedulerConfig struct {
	TickInterval        time.Duration
	MaxConcurrent       int
	DefaultTimeout      time.Duration
	DefaultRetries      int
	TimeSyncInterval    time.Duration
	HealthCheckInterval time.Duration
}

// DefaultSchedulerConfig returns a default scheduler configuration.
func DefaultSchedulerConfig() *SchedulerConfig {
	return &SchedulerConfig{
		TickInterval:        time.Second,
		MaxConcurrent:       10,
		DefaultTimeout:      30 * time.Second,
		DefaultRetries:      3,
		TimeSyncInterval:    time.Hour,
		HealthCheckInterval: 5 * time.Minute,
	}
}

// NewCommandScheduler creates a new command scheduler.
func NewCommandScheduler(
	sessionManager *session.SessionManager,
	commandBuilder *protocol.CommandBuilder,
	responseManager *protocol.ResponseManager,
	config *SchedulerConfig,
	logger zerolog.Logger,
) *CommandScheduler {
	if config == nil {
		config = DefaultSchedulerConfig()
	}

	return &CommandScheduler{
		queue:               NewCommandQueue(logger),
		sessionManager:      sessionManager,
		commandBuilder:      commandBuilder,
		responseManager:     responseManager,
		logger:              logger.With().Str("component", "command_scheduler").Logger(),
		stopChan:            make(chan struct{}),
		tickInterval:        config.TickInterval,
		maxConcurrent:       config.MaxConcurrent,
		defaultTimeout:      config.DefaultTimeout,
		defaultRetries:      config.DefaultRetries,
		timeSyncInterval:    config.TimeSyncInterval,
		healthCheckInterval: config.HealthCheckInterval,
	}
}

// Start begins the command scheduler.
func (cs *CommandScheduler) Start(ctx context.Context) error {
	cs.mutex.Lock()
	defer cs.mutex.Unlock()

	if cs.isRunning {
		return fmt.Errorf("scheduler is already running")
	}

	cs.ticker = time.NewTicker(cs.tickInterval)
	cs.isRunning = true

	cs.wg.Add(2)
	go cs.executionLoop(ctx)
	go cs.maintenanceLoop(ctx)

	cs.logger.Info().
		Dur("tick_interval", cs.tickInterval).
		Int("max_concurrent", cs.maxConcurrent).
		Msg("Command scheduler started")

	return nil
}

// Stop shuts down the command scheduler.
func (cs *CommandScheduler) Stop() error {
	cs.mutex.Lock()
	defer cs.mutex.Unlock()

	if !cs.isRunning {
		return fmt.Errorf("scheduler is not running")
	}

	close(cs.stopChan)
	if cs.ticker != nil {
		cs.ticker.Stop()
	}

	cs.wg.Wait()
	cs.isRunning = false

	cs.logger.Info().Msg("Command scheduler stopped")
	return nil
}

// ScheduleCommand adds a command to the execution queue.
func (cs *CommandScheduler) ScheduleCommand(cmd *ScheduledCommand) error {
	if cmd.ID == "" {
		cmd.ID = generateCommandID()
	}
	if cmd.CreatedAt.IsZero() {
		cmd.CreatedAt = time.Now()
	}
	if cmd.ExpiresAt.IsZero() {
		cmd.ExpiresAt = time.Now().Add(cs.defaultTimeout)
	}
	if cmd.MaxRetries == 0 {
		cmd.MaxRetries = cs.defaultRetries
	}

	cs.queue.Enqueue(cmd)
	return nil
}

// ScheduleTimeSync schedules a time synchronization command for a session.
func (cs *CommandScheduler) ScheduleTimeSync(sessionID string, priority CommandPriority) error {
	cmd := &ScheduledCommand{
		Type:        CommandTypeTimeSync,
		Priority:    priority,
		SessionID:   sessionID,
		ScheduledAt: time.Now(),
		Parameters:  make(map[string]interface{}),
	}

	return cs.ScheduleCommand(cmd)
}

// ScheduleHealthCheck schedules a health check (ping) command for a session.
func (cs *CommandScheduler) ScheduleHealthCheck(sessionID string, priority CommandPriority) error {
	cmd := &ScheduledCommand{
		Type:        CommandTypeHealthCheck,
		Priority:    priority,
		SessionID:   sessionID,
		ScheduledAt: time.Now(),
		Parameters:  make(map[string]interface{}),
	}

	return cs.ScheduleCommand(cmd)
}

// SchedulePeriodicTimeSync schedules automatic time sync commands for all active sessions.
func (cs *CommandScheduler) SchedulePeriodicTimeSync() {
	sessions := cs.sessionManager.GetAllSessions()
	for _, sessionStats := range sessions {
		if err := cs.ScheduleTimeSync(sessionStats.ID, PriorityNormal); err != nil {
			cs.logger.Error().
				Err(err).
				Str("session_id", sessionStats.ID).
				Msg("Failed to schedule periodic time sync")
		}
	}
}

// SchedulePeriodicHealthChecks schedules health check commands for all active sessions.
func (cs *CommandScheduler) SchedulePeriodicHealthChecks() {
	sessions := cs.sessionManager.GetAllSessions()
	for _, sessionStats := range sessions {
		if err := cs.ScheduleHealthCheck(sessionStats.ID, PriorityLow); err != nil {
			cs.logger.Error().
				Err(err).
				Str("session_id", sessionStats.ID).
				Msg("Failed to schedule health check")
		}
	}
}

// GetMetrics returns current scheduler metrics.
func (cs *CommandScheduler) GetMetrics() map[string]interface{} {
	cs.mutex.RLock()
	defer cs.mutex.RUnlock()

	return map[string]interface{}{
		"is_running":        cs.isRunning,
		"queue_length":      cs.queue.GetQueueLength(),
		"queue_by_priority": cs.queue.GetQueueLengthByPriority(),
		"commands_executed": atomic.LoadInt64(&cs.commandsExecuted),
		"commands_failed":   atomic.LoadInt64(&cs.commandsFailed),
		"commands_retried":  atomic.LoadInt64(&cs.commandsRetried),
		"active_sessions":   cs.sessionManager.GetSessionCount(),
		"active_workers":    atomic.LoadInt64(&cs.activeWorkers), // Add active workers count
	}
}

// executionLoop handles command execution.
func (cs *CommandScheduler) executionLoop(ctx context.Context) {
	defer cs.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case <-cs.stopChan:
			return
		case <-cs.ticker.C:
			cs.processCommands(ctx)
		}
	}
}

// maintenanceLoop handles periodic maintenance tasks.
func (cs *CommandScheduler) maintenanceLoop(ctx context.Context) {
	defer cs.wg.Done()

	timeSyncTicker := time.NewTicker(cs.timeSyncInterval)
	healthCheckTicker := time.NewTicker(cs.healthCheckInterval)
	cleanupTicker := time.NewTicker(5 * time.Minute)

	defer func() {
		timeSyncTicker.Stop()
		healthCheckTicker.Stop()
		cleanupTicker.Stop()
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case <-cs.stopChan:
			return
		case <-timeSyncTicker.C:
			cs.SchedulePeriodicTimeSync()
		case <-healthCheckTicker.C:
			cs.SchedulePeriodicHealthChecks()
		case <-cleanupTicker.C:
			cs.queue.CleanupExpired()
		}
	}
}

// processCommands processes commands from the queue.
func (cs *CommandScheduler) processCommands(ctx context.Context) {
	// Process up to maxConcurrent commands
	for i := 0; i < cs.maxConcurrent; i++ {
		cmd := cs.queue.Dequeue()
		if cmd == nil {
			break
		}

		go cs.executeCommand(ctx, cmd)
	}
}

// executeCommand executes a single command.
func (cs *CommandScheduler) executeCommand(ctx context.Context, cmd *ScheduledCommand) {
	// Track active worker
	atomic.AddInt64(&cs.activeWorkers, 1)
	defer atomic.AddInt64(&cs.activeWorkers, -1)

	now := time.Now()
	cmd.ExecutedAt = &now

	cs.logger.Debug().
		Str("command_id", cmd.ID).
		Str("type", cmd.Type.String()).
		Str("session_id", cmd.SessionID).
		Msg("Executing command")

	// Get session
	sess, exists := cs.sessionManager.GetSession(cmd.SessionID)
	if !exists {
		cmd.Error = fmt.Errorf("session not found: %s", cmd.SessionID)
		cs.recordCommandFailure(cmd)
		return
	}

	// Execute command based on type
	var err error
	switch cmd.Type {
	case CommandTypeTimeSync:
		err = cs.executeTimeSyncCommand(ctx, cmd, sess)
	case CommandTypeHealthCheck:
		err = cs.executeHealthCheckCommand(ctx, cmd, sess)
	case CommandTypePing:
		err = cs.executePingCommand(ctx, cmd, sess)
	default:
		err = fmt.Errorf("unknown command type: %s", cmd.Type.String())
	}

	if err != nil {
		cmd.Error = err
		if cmd.CanRetry() {
			cs.retryCommand(cmd)
		} else {
			cs.recordCommandFailure(cmd)
		}
		return
	}

	// Mark as completed
	completed := time.Now()
	cmd.CompletedAt = &completed
	atomic.AddInt64(&cs.commandsExecuted, 1)

	cs.logger.Debug().
		Str("command_id", cmd.ID).
		Str("type", cmd.Type.String()).
		Dur("duration", completed.Sub(*cmd.ExecutedAt)).
		Msg("Command completed successfully")
}

// executeTimeSyncCommand executes a time synchronization command.
func (cs *CommandScheduler) executeTimeSyncCommand(ctx context.Context, cmd *ScheduledCommand, sess *session.Session) error {
	// Get protocol from session or parameters
	ptcl := sess.Protocol
	if ptcl == "" {
		if p, ok := cmd.Parameters["protocol"].(string); ok {
			ptcl = p
		} else {
			ptcl = protocol.ProtocolInverterWrite // Default to ProtocolMultiRegister
		}
	}

	// Create time sync command
	timeUnix := time.Now().Unix()
	//nolint:gosec // Safe conversion, modulo ensures value fits in uint16
	timeValue := uint16(timeUnix % 65536)
	timeSyncCmd, data, err := cs.commandBuilder.CreateTimeSyncCommand(ptcl, sess.SerialNumber, timeValue)
	if err != nil {
		return fmt.Errorf("failed to create time sync command: %w", err)
	}

	// Send command to device
	_, err = sess.Connection.Write(data)
	if err != nil {
		return fmt.Errorf("failed to send time sync command: %w", err)
	}

	cmd.Result = map[string]interface{}{
		"command":    timeSyncCmd,
		"bytes_sent": len(data),
	}

	sess.UpdateLastCommand()
	return nil
}

// executeHealthCheckCommand executes a health check command.
func (cs *CommandScheduler) executeHealthCheckCommand(ctx context.Context, cmd *ScheduledCommand, sess *session.Session) error {
	// Get protocol from session
	ptcl := sess.Protocol
	if ptcl == "" {
		ptcl = protocol.ProtocolInverterWrite // Default to ProtocolMultiRegister
	}

	// Create ping command
	timeUnix := time.Now().Unix()
	//nolint:gosec // Safe conversion, modulo ensures value fits in uint16
	timeValue := uint16(timeUnix % 65536)
	pingCmd, data, err := cs.commandBuilder.CreatePingCommand(ptcl, sess.SerialNumber, timeValue)
	if err != nil {
		return fmt.Errorf("failed to create ping command: %w", err)
	}

	// Send command to device
	_, err = sess.Connection.Write(data)
	if err != nil {
		return fmt.Errorf("failed to send ping command: %w", err)
	}

	cmd.Result = map[string]interface{}{
		"command":    pingCmd,
		"bytes_sent": len(data),
	}

	sess.UpdateLastCommand()
	return nil
}

// executePingCommand executes a ping command (alias for health check).
func (cs *CommandScheduler) executePingCommand(ctx context.Context, cmd *ScheduledCommand, sess *session.Session) error {
	return cs.executeHealthCheckCommand(ctx, cmd, sess)
}

// retryCommand reschedules a failed command for retry.
func (cs *CommandScheduler) retryCommand(cmd *ScheduledCommand) {
	cmd.Retries++
	cmd.ExecutedAt = nil
	cmd.ScheduledAt = time.Now().Add(time.Duration(cmd.Retries) * time.Second) // Exponential backoff

	cs.queue.Enqueue(cmd)
	atomic.AddInt64(&cs.commandsRetried, 1)

	cs.logger.Warn().
		Str("command_id", cmd.ID).
		Str("type", cmd.Type.String()).
		Int("retry", cmd.Retries).
		Int("max_retries", cmd.MaxRetries).
		Err(cmd.Error).
		Msg("Retrying command")
}

// recordCommandFailure records a failed command.
func (cs *CommandScheduler) recordCommandFailure(cmd *ScheduledCommand) {
	atomic.AddInt64(&cs.commandsFailed, 1)

	cs.logger.Error().
		Str("command_id", cmd.ID).
		Str("type", cmd.Type.String()).
		Str("session_id", cmd.SessionID).
		Int("retries", cmd.Retries).
		Err(cmd.Error).
		Msg("Command failed after retries")
}

// generateCommandID generates a unique command ID.
func generateCommandID() string {
	// Use atomic counter to ensure uniqueness even with concurrent calls
	counter := atomic.AddUint64(&commandIDCounter, 1)
	return fmt.Sprintf("cmd_%d_%d", time.Now().UnixNano(), counter)
}
