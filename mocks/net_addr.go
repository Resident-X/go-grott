// Code generated by mockery v2.53.4. DO NOT EDIT.

package mocks

import mock "github.com/stretchr/testify/mock"

// MockAddr is an autogenerated mock type for the Addr type
type MockAddr struct {
	mock.Mock
}

type MockAddr_Expecter struct {
	mock *mock.Mock
}

func (_m *MockAddr) EXPECT() *MockAddr_Expecter {
	return &MockAddr_Expecter{mock: &_m.Mock}
}

// Network provides a mock function with no fields
func (_m *MockAddr) Network() string {
	ret := _m.Called()

	if len(ret) == 0 {
		panic("no return value specified for Network")
	}

	var r0 string
	if rf, ok := ret.Get(0).(func() string); ok {
		r0 = rf()
	} else {
		r0 = ret.Get(0).(string)
	}

	return r0
}

// MockAddr_Network_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'Network'
type MockAddr_Network_Call struct {
	*mock.Call
}

// Network is a helper method to define mock.On call
func (_e *MockAddr_Expecter) Network() *MockAddr_Network_Call {
	return &MockAddr_Network_Call{Call: _e.mock.On("Network")}
}

func (_c *MockAddr_Network_Call) Run(run func()) *MockAddr_Network_Call {
	_c.Call.Run(func(args mock.Arguments) {
		run()
	})
	return _c
}

func (_c *MockAddr_Network_Call) Return(_a0 string) *MockAddr_Network_Call {
	_c.Call.Return(_a0)
	return _c
}

func (_c *MockAddr_Network_Call) RunAndReturn(run func() string) *MockAddr_Network_Call {
	_c.Call.Return(run)
	return _c
}

// String provides a mock function with no fields
func (_m *MockAddr) String() string {
	ret := _m.Called()

	if len(ret) == 0 {
		panic("no return value specified for String")
	}

	var r0 string
	if rf, ok := ret.Get(0).(func() string); ok {
		r0 = rf()
	} else {
		r0 = ret.Get(0).(string)
	}

	return r0
}

// MockAddr_String_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'String'
type MockAddr_String_Call struct {
	*mock.Call
}

// String is a helper method to define mock.On call
func (_e *MockAddr_Expecter) String() *MockAddr_String_Call {
	return &MockAddr_String_Call{Call: _e.mock.On("String")}
}

func (_c *MockAddr_String_Call) Run(run func()) *MockAddr_String_Call {
	_c.Call.Run(func(args mock.Arguments) {
		run()
	})
	return _c
}

func (_c *MockAddr_String_Call) Return(_a0 string) *MockAddr_String_Call {
	_c.Call.Return(_a0)
	return _c
}

func (_c *MockAddr_String_Call) RunAndReturn(run func() string) *MockAddr_String_Call {
	_c.Call.Return(run)
	return _c
}

// NewMockAddr creates a new instance of MockAddr. It also registers a testing interface on the mock and a cleanup function to assert the mocks expectations.
// The first argument is typically a *testing.T value.
func NewMockAddr(t interface {
	mock.TestingT
	Cleanup(func())
}) *MockAddr {
	mock := &MockAddr{}
	mock.Mock.Test(t)

	t.Cleanup(func() { mock.AssertExpectations(t) })

	return mock
}
