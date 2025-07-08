package session

import (
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Mock connection for testing
type mockConn struct {
	remoteAddr net.Addr
	closed     bool
}

func (m *mockConn) Read(b []byte) (n int, err error) {
	return 0, nil
}

func (m *mockConn) Write(b []byte) (n int, err error) {
	return len(b), nil
}

func (m *mockConn) Close() error {
	m.closed = true
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

func TestNewSessionManager(t *testing.T) {
	manager := NewSessionManager(time.Minute)
	require.NotNil(t, manager)
	require.NotNil(t, manager.sessions)
	require.NotNil(t, manager.sessionsByAddr)
}

func TestSessionManagerCreateSession(t *testing.T) {
	manager := NewSessionManager(time.Minute)
	
	addr := &net.TCPAddr{IP: net.ParseIP("192.168.1.100"), Port: 5279}
	conn := &mockConn{remoteAddr: addr}
	
	session := manager.CreateSession(conn)
	
	// Verify session was created
	assert.NotNil(t, session)
	assert.NotEmpty(t, session.ID)
	
	// Verify session exists
	retrievedSession, exists := manager.GetSession(session.ID)
	require.True(t, exists)
	require.NotNil(t, retrievedSession)
	
	// Verify session properties
	assert.Equal(t, session.ID, retrievedSession.ID)
	assert.Equal(t, addr.String(), retrievedSession.RemoteAddr)
	assert.Equal(t, SessionStateConnected, retrievedSession.State)
	assert.Equal(t, DeviceTypeUnknown, retrievedSession.DeviceType)
	assert.WithinDuration(t, time.Now(), retrievedSession.ConnectedAt, time.Second)
	assert.WithinDuration(t, time.Now(), retrievedSession.LastActivity, time.Second)
}

func TestSessionManagerRemoveSession(t *testing.T) {
	manager := NewSessionManager(time.Minute)
	
	addr := &net.TCPAddr{IP: net.ParseIP("192.168.1.100"), Port: 5279}
	conn := &mockConn{remoteAddr: addr}
	
	session := manager.CreateSession(conn)
	
	// Verify session exists
	_, exists := manager.GetSession(session.ID)
	assert.True(t, exists)
	
	// Remove session
	manager.RemoveSession(session.ID)
	
	// Verify session was removed
	_, exists = manager.GetSession(session.ID)
	assert.False(t, exists)
}

func TestSessionManagerGetSessionByAddr(t *testing.T) {
	manager := NewSessionManager(time.Minute)
	
	addr := &net.TCPAddr{IP: net.ParseIP("192.168.1.100"), Port: 5279}
	conn := &mockConn{remoteAddr: addr}
	
	session := manager.CreateSession(conn)
	
	// Retrieve session by address
	retrievedSession, exists := manager.GetSessionByAddr(addr.String())
	assert.True(t, exists)
	assert.Equal(t, session.ID, retrievedSession.ID)
}

func TestSessionManagerGetAllSessions(t *testing.T) {
	manager := NewSessionManager(time.Minute)
	
	// Create multiple sessions
	addr1 := &net.TCPAddr{IP: net.ParseIP("192.168.1.100"), Port: 5279}
	addr2 := &net.TCPAddr{IP: net.ParseIP("192.168.1.101"), Port: 5279}
	
	conn1 := &mockConn{remoteAddr: addr1}
	conn2 := &mockConn{remoteAddr: addr2}
	
	session1 := manager.CreateSession(conn1)
	session2 := manager.CreateSession(conn2)
	
	// Get all sessions
	sessionStats := manager.GetAllSessions()
	
	// Verify we have 2 sessions
	assert.Len(t, sessionStats, 2)
	
	// Verify session IDs are correct
	sessionIDs := make([]string, len(sessionStats))
	for i, stats := range sessionStats {
		sessionIDs[i] = stats.ID
	}
	assert.Contains(t, sessionIDs, session1.ID)
	assert.Contains(t, sessionIDs, session2.ID)
}

func TestSessionManagerGetSessionCount(t *testing.T) {
	manager := NewSessionManager(time.Minute)
	
	// Initially no sessions
	assert.Equal(t, 0, manager.GetSessionCount())
	
	// Create a session
	addr := &net.TCPAddr{IP: net.ParseIP("192.168.1.100"), Port: 5279}
	conn := &mockConn{remoteAddr: addr}
	
	manager.CreateSession(conn)
	
	// Verify count
	assert.Equal(t, 1, manager.GetSessionCount())
}

func TestSessionManagerCleanupExpiredSessions(t *testing.T) {
	manager := NewSessionManager(50 * time.Millisecond)
	
	addr := &net.TCPAddr{IP: net.ParseIP("192.168.1.100"), Port: 5279}
	conn := &mockConn{remoteAddr: addr}
	
	session := manager.CreateSession(conn)
	
	// Verify session exists
	_, exists := manager.GetSession(session.ID)
	assert.True(t, exists)
	
	// Wait for timeout
	time.Sleep(60 * time.Millisecond)
	
	// Cleanup expired sessions
	cleaned := manager.CleanupExpiredSessions()
	
	// Verify session was cleaned up
	assert.Equal(t, 1, cleaned)
	_, exists = manager.GetSession(session.ID)
	assert.False(t, exists)
}

func TestSession(t *testing.T) {
	addr := &net.TCPAddr{IP: net.ParseIP("192.168.1.100"), Port: 5279}
	conn := &mockConn{remoteAddr: addr}
	
	session := NewSession(conn)
	
	// Test basic properties
	assert.NotEmpty(t, session.ID)
	assert.Equal(t, DeviceTypeUnknown, session.DeviceType)
	assert.Equal(t, addr.String(), session.RemoteAddr)
	assert.Equal(t, SessionStateConnected, session.State)
	
	// Test state changes
	session.SetState(SessionStateAuthenticated)
	assert.Equal(t, SessionStateAuthenticated, session.GetState())
	
	// Test activity updates
	oldActivity := session.LastActivity
	time.Sleep(10 * time.Millisecond)
	session.UpdateActivity()
	assert.True(t, session.LastActivity.After(oldActivity))
	
	// Test device info
	session.SetDeviceInfo(DeviceTypeInverter, "TEST123456", "06", "1.0")
	assert.Equal(t, DeviceTypeInverter, session.DeviceType)
	assert.Equal(t, "TEST123456", session.SerialNumber)
	assert.Equal(t, "06", session.Protocol)
	assert.Equal(t, "1.0", session.Version)
	
	// Test byte counters
	session.AddBytesReceived(100)
	session.AddBytesSent(50)
	assert.Equal(t, int64(100), session.BytesReceived)
	assert.Equal(t, int64(50), session.BytesSent)
	assert.Equal(t, int64(1), session.PacketsReceived)
	assert.Equal(t, int64(1), session.PacketsSent)
}

func TestSessionStateString(t *testing.T) {
	tests := []struct {
		state    SessionState
		expected string
	}{
		{SessionStateConnected, "connected"},
		{SessionStateAuthenticated, "authenticated"},
		{SessionStateActive, "active"},
		{SessionStateDisconnected, "disconnected"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.state.String())
		})
	}
}

func TestDeviceTypeString(t *testing.T) {
	tests := []struct {
		deviceType DeviceType
		expected   string
	}{
		{DeviceTypeUnknown, "unknown"},
		{DeviceTypeDatalogger, "datalogger"},
		{DeviceTypeInverter, "inverter"},
		{DeviceTypeSmartMeter, "smart_meter"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.deviceType.String())
		})
	}
}

// Benchmark tests
func BenchmarkCreateSession(b *testing.B) {
	manager := NewSessionManager(time.Minute)
	addr := &net.TCPAddr{IP: net.ParseIP("192.168.1.100"), Port: 5279}
	
	for i := 0; i < b.N; i++ {
		conn := &mockConn{remoteAddr: addr}
		session := manager.CreateSession(conn)
		manager.RemoveSession(session.ID)
	}
}

func BenchmarkGetSession(b *testing.B) {
	manager := NewSessionManager(time.Minute)
	addr := &net.TCPAddr{IP: net.ParseIP("192.168.1.100"), Port: 5279}
	conn := &mockConn{remoteAddr: addr}
	
	session := manager.CreateSession(conn)
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = manager.GetSession(session.ID)
	}
}

func BenchmarkSessionUpdateActivity(b *testing.B) {
	addr := &net.TCPAddr{IP: net.ParseIP("192.168.1.100"), Port: 5279}
	conn := &mockConn{remoteAddr: addr}
	
	session := NewSession(conn)
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		session.UpdateActivity()
	}
}
