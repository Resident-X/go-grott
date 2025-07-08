// Package session provides session management for connected inverters and dataloggers.
package session

import (
	"net"
	"sync"
	"time"
)

// SessionState represents the current state of a device session.
type SessionState int

const (
	SessionStateConnected SessionState = iota
	SessionStateAuthenticated
	SessionStateActive
	SessionStateDisconnected
)

// String returns the string representation of the session state.
func (s SessionState) String() string {
	switch s {
	case SessionStateConnected:
		return "connected"
	case SessionStateAuthenticated:
		return "authenticated"
	case SessionStateActive:
		return "active"
	case SessionStateDisconnected:
		return "disconnected"
	default:
		return "unknown"
	}
}

// DeviceType represents the type of connected device.
type DeviceType int

const (
	DeviceTypeUnknown DeviceType = iota
	DeviceTypeDatalogger
	DeviceTypeInverter
	DeviceTypeSmartMeter
)

// String returns the string representation of the device type.
func (d DeviceType) String() string {
	switch d {
	case DeviceTypeDatalogger:
		return "datalogger"
	case DeviceTypeInverter:
		return "inverter"
	case DeviceTypeSmartMeter:
		return "smart_meter"
	default:
		return "unknown"
	}
}

// Session represents a connection session with a device.
type Session struct {
	ID              string
	DeviceType      DeviceType
	SerialNumber    string
	RemoteAddr      string
	LocalAddr       string
	State           SessionState
	ConnectedAt     time.Time
	LastActivity    time.Time
	LastCommand     time.Time
	BytesReceived   int64
	BytesSent       int64
	PacketsReceived int64
	PacketsSent     int64
	ErrorCount      int64
	Connection      net.Conn
	Protocol        string
	Version         string
	mutex           sync.RWMutex
}

// NewSession creates a new session for a device connection.
func NewSession(conn net.Conn) *Session {
	now := time.Now()
	return &Session{
		ID:           generateSessionID(conn.RemoteAddr().String(), now),
		DeviceType:   DeviceTypeUnknown,
		RemoteAddr:   conn.RemoteAddr().String(),
		LocalAddr:    conn.LocalAddr().String(),
		State:        SessionStateConnected,
		ConnectedAt:  now,
		LastActivity: now,
		Connection:   conn,
	}
}

// UpdateActivity updates the last activity time for the session.
func (s *Session) UpdateActivity() {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.LastActivity = time.Now()
}

// UpdateLastCommand updates the last command time for the session.
func (s *Session) UpdateLastCommand() {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.LastCommand = time.Now()
}

// SetState safely updates the session state.
func (s *Session) SetState(state SessionState) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.State = state
}

// GetState safely retrieves the session state.
func (s *Session) GetState() SessionState {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.State
}

// SetDeviceInfo updates device information for the session.
func (s *Session) SetDeviceInfo(deviceType DeviceType, serialNumber, protocol, version string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.DeviceType = deviceType
	s.SerialNumber = serialNumber
	s.Protocol = protocol
	s.Version = version
}

// AddBytesReceived safely adds to the bytes received counter.
func (s *Session) AddBytesReceived(bytes int64) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.BytesReceived += bytes
	s.PacketsReceived++
}

// AddBytesSent safely adds to the bytes sent counter.
func (s *Session) AddBytesSent(bytes int64) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.BytesSent += bytes
	s.PacketsSent++
}

// IncrementErrorCount safely increments the error counter.
func (s *Session) IncrementErrorCount() {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.ErrorCount++
}

// GetStats returns a copy of the session statistics.
func (s *Session) GetStats() SessionStats {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	
	return SessionStats{
		ID:              s.ID,
		DeviceType:      s.DeviceType,
		SerialNumber:    s.SerialNumber,
		RemoteAddr:      s.RemoteAddr,
		State:           s.State,
		ConnectedAt:     s.ConnectedAt,
		LastActivity:    s.LastActivity,
		LastCommand:     s.LastCommand,
		BytesReceived:   s.BytesReceived,
		BytesSent:       s.BytesSent,
		PacketsReceived: s.PacketsReceived,
		PacketsSent:     s.PacketsSent,
		ErrorCount:      s.ErrorCount,
		Protocol:        s.Protocol,
		Version:         s.Version,
	}
}

// IsExpired checks if the session has expired based on inactivity.
func (s *Session) IsExpired(timeout time.Duration) bool {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return time.Since(s.LastActivity) > timeout
}

// Close closes the session and its underlying connection.
func (s *Session) Close() error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	
	s.State = SessionStateDisconnected
	if s.Connection != nil {
		return s.Connection.Close()
	}
	return nil
}

// SessionStats represents session statistics for external consumption.
type SessionStats struct {
	ID              string        `json:"id"`
	DeviceType      DeviceType    `json:"device_type"`
	SerialNumber    string        `json:"serial_number"`
	RemoteAddr      string        `json:"remote_addr"`
	State           SessionState  `json:"state"`
	ConnectedAt     time.Time     `json:"connected_at"`
	LastActivity    time.Time     `json:"last_activity"`
	LastCommand     time.Time     `json:"last_command"`
	BytesReceived   int64         `json:"bytes_received"`
	BytesSent       int64         `json:"bytes_sent"`
	PacketsReceived int64         `json:"packets_received"`
	PacketsSent     int64         `json:"packets_sent"`
	ErrorCount      int64         `json:"error_count"`
	Protocol        string        `json:"protocol"`
	Version         string        `json:"version"`
	Duration        time.Duration `json:"duration"`
}

// SessionManager manages multiple device sessions.
type SessionManager struct {
	sessions       map[string]*Session
	sessionsByAddr map[string]*Session
	mutex          sync.RWMutex
	cleanupTicker  *time.Ticker
	stopCleanup    chan struct{}
	sessionTimeout time.Duration
}

// NewSessionManager creates a new session manager.
func NewSessionManager(sessionTimeout time.Duration) *SessionManager {
	sm := &SessionManager{
		sessions:       make(map[string]*Session),
		sessionsByAddr: make(map[string]*Session),
		sessionTimeout: sessionTimeout,
		stopCleanup:    make(chan struct{}),
	}
	
	// Start cleanup routine
	sm.startCleanupRoutine()
	
	return sm
}

// CreateSession creates a new session for a connection.
func (sm *SessionManager) CreateSession(conn net.Conn) *Session {
	session := NewSession(conn)
	
	sm.mutex.Lock()
	defer sm.mutex.Unlock()
	
	// Clean up any existing session for this address
	if existingSession, exists := sm.sessionsByAddr[session.RemoteAddr]; exists {
		delete(sm.sessions, existingSession.ID)
		existingSession.Close()
	}
	
	sm.sessions[session.ID] = session
	sm.sessionsByAddr[session.RemoteAddr] = session
	
	return session
}

// GetSession retrieves a session by ID.
func (sm *SessionManager) GetSession(id string) (*Session, bool) {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()
	
	session, exists := sm.sessions[id]
	return session, exists
}

// GetSessionByAddr retrieves a session by remote address.
func (sm *SessionManager) GetSessionByAddr(addr string) (*Session, bool) {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()
	
	session, exists := sm.sessionsByAddr[addr]
	return session, exists
}

// GetAllSessions returns statistics for all active sessions.
func (sm *SessionManager) GetAllSessions() []SessionStats {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()
	
	stats := make([]SessionStats, 0, len(sm.sessions))
	now := time.Now()
	
	for _, session := range sm.sessions {
		sessionStats := session.GetStats()
		sessionStats.Duration = now.Sub(sessionStats.ConnectedAt)
		stats = append(stats, sessionStats)
	}
	
	return stats
}

// RemoveSession removes a session by ID.
func (sm *SessionManager) RemoveSession(id string) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()
	
	if session, exists := sm.sessions[id]; exists {
		delete(sm.sessionsByAddr, session.RemoteAddr)
		delete(sm.sessions, id)
		session.Close()
	}
}

// CleanupExpiredSessions removes expired sessions.
func (sm *SessionManager) CleanupExpiredSessions() int {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()
	
	var expiredSessions []string
	
	for id, session := range sm.sessions {
		if session.IsExpired(sm.sessionTimeout) {
			expiredSessions = append(expiredSessions, id)
		}
	}
	
	for _, id := range expiredSessions {
		if session, exists := sm.sessions[id]; exists {
			delete(sm.sessionsByAddr, session.RemoteAddr)
			delete(sm.sessions, id)
			session.Close()
		}
	}
	
	return len(expiredSessions)
}

// GetSessionCount returns the number of active sessions.
func (sm *SessionManager) GetSessionCount() int {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()
	return len(sm.sessions)
}

// Close shuts down the session manager and closes all sessions.
func (sm *SessionManager) Close() {
	// Stop cleanup routine
	close(sm.stopCleanup)
	if sm.cleanupTicker != nil {
		sm.cleanupTicker.Stop()
	}
	
	// Close all sessions
	sm.mutex.Lock()
	defer sm.mutex.Unlock()
	
	for _, session := range sm.sessions {
		session.Close()
	}
	
	sm.sessions = make(map[string]*Session)
	sm.sessionsByAddr = make(map[string]*Session)
}

// startCleanupRoutine starts a goroutine to periodically clean up expired sessions.
func (sm *SessionManager) startCleanupRoutine() {
	sm.cleanupTicker = time.NewTicker(time.Minute) // Run cleanup every minute
	
	go func() {
		for {
			select {
			case <-sm.cleanupTicker.C:
				cleaned := sm.CleanupExpiredSessions()
				if cleaned > 0 {
					// Log cleanup activity if needed
					_ = cleaned
				}
			case <-sm.stopCleanup:
				return
			}
		}
	}()
}

// generateSessionID generates a unique session ID.
func generateSessionID(addr string, timestamp time.Time) string {
	return addr + "_" + timestamp.Format("20060102_150405.000000")
}
