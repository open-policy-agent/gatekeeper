package testdriver

import (
	"context"
	"fmt"
)

const (
	Name    = "testdriver"
	ErrName = "testdriver-error"
)

var FakeConn = &Connection{
	openConnections: make(map[string]FakeConnection),
}

// Connection represents driver to use testdriver.
type Connection struct {
	openConnections map[string]FakeConnection
}

type FakeConnection struct {
	name string
}

func (r *Connection) Publish(_ context.Context, _ string, _ interface{}, _ string) error {
	return nil
}

func (r *Connection) CloseConnection(connectionName string) error {
	delete(r.openConnections, connectionName)
	return nil
}

func (r *Connection) UpdateConnection(_ context.Context, connectionName string, config interface{}) error {
	name, ok := config.(string)
	if !ok {
		return fmt.Errorf("invalid type assertion, config is not in expected format")
	}
	r.openConnections[connectionName] = FakeConnection{name: name}
	return nil
}

func (r *Connection) CreateConnection(_ context.Context, connectionName string, config interface{}) error {
	name, ok := config.(string)
	if !ok {
		return fmt.Errorf("invalid type assertion, config is not in expected format")
	}
	r.openConnections[connectionName] = FakeConnection{name: name}
	return nil
}

var FakeErrConn = &ErrConnection{
	openErrConnections: make(map[string]FakeErrConnection),
}

// ErrConnection represents driver to use testdriver.
type ErrConnection struct {
	openErrConnections map[string]FakeErrConnection
}

type FakeErrConnection struct {
	name string
}

func (r *ErrConnection) Publish(_ context.Context, _ string, _ interface{}, _ string) error {
	return fmt.Errorf("error publishing message to testdriver")
}

func (r *ErrConnection) CloseConnection(connectionName string) error {
	delete(r.openErrConnections, connectionName)
	return fmt.Errorf("error closing connection")
}

func (r *ErrConnection) UpdateConnection(_ context.Context, connectionName string, config interface{}) error {
	name, ok := config.(string)
	if !ok {
		return fmt.Errorf("invalid type assertion, config is not in expected format")
	}
	r.openErrConnections[connectionName] = FakeErrConnection{name: name}
	return fmt.Errorf("error updating connection")
}

func (r *ErrConnection) CreateConnection(_ context.Context, connectionName string, config interface{}) error {
	name, ok := config.(string)
	if !ok {
		return fmt.Errorf("invalid type assertion, config is not in expected format")
	}
	r.openErrConnections[connectionName] = FakeErrConnection{name: name}
	return fmt.Errorf("error creating connection")
}
