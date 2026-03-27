package export

import (
	"context"
	"os"
	"sync"
	"testing"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/export/dapr"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/export/driver"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/export/testdriver"
	"github.com/stretchr/testify/assert"
)

var testSystem *System

func TestMain(m *testing.M) {
	ctx := context.Background()
	SupportedDrivers = map[string]driver.Driver{
		dapr.Name: dapr.FakeConn,
	}
	testSystem = NewSystem()
	cfg := map[string]interface{}{
		dapr.Name: map[string]interface{}{
			"component": "pubsub",
		},
	}
	for name, fakeConn := range SupportedDrivers {
		testSystem.connectionToDriver[name] = name
		_ = fakeConn.CreateConnection(ctx, name, cfg[name])
	}
	r := m.Run()
	for name, fakeConn := range testSystem.connectionToDriver {
		_ = SupportedDrivers[fakeConn].CloseConnection(name)
	}

	if r != 0 {
		os.Exit(r)
	}
}

func TestNewSystem(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  *System
	}{
		{
			name: "requesting system",
			want: &System{
				connectionToDriver: map[string]string{},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ret := NewSystem()
			assert.Equal(t, ret, tc.want)
		})
	}
}

func TestSystem_UpsertConnection(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name           string
		config         interface{}
		connectionName string
		newDriver      string
		setup          func(*System) error
		wantErr        bool
	}{
		{
			name:           "new connection with supported driver",
			config:         map[string]interface{}{"component": "pubsub"},
			connectionName: "conn1",
			newDriver:      dapr.Name,
			setup: func(s *System) error {
				s.connectionToDriver = map[string]string{}
				SupportedDrivers[dapr.Name] = dapr.FakeConn
				return nil
			},
			wantErr: false,
		},
		{
			name:           "update existing connection with same driver",
			config:         map[string]interface{}{"component": "pubsub1"},
			connectionName: "conn1",
			newDriver:      dapr.Name,
			setup: func(s *System) error {
				s.connectionToDriver["conn1"] = dapr.Name
				SupportedDrivers[dapr.Name] = dapr.FakeConn
				return SupportedDrivers[dapr.Name].CreateConnection(ctx, "conn1", map[string]interface{}{"component": "pubsub"})
			},
			wantErr: false,
		},
		{
			name:           "new connection with unsupported driver",
			config:         map[string]interface{}{"component": "pubsub"},
			connectionName: "conn3",
			newDriver:      "unsupportedDriver",
			setup:          func(_ *System) error { return nil },
			wantErr:        true,
		},
		{
			name:           "update existing connection with different driver",
			config:         map[string]interface{}{"component": "pubsub"},
			connectionName: "conn4",
			newDriver:      dapr.Name,
			setup: func(s *System) error {
				s.connectionToDriver["conn4"] = testdriver.Name
				SupportedDrivers[dapr.Name] = dapr.FakeConn
				SupportedDrivers[testdriver.Name] = testdriver.FakeConn
				return SupportedDrivers[testdriver.Name].CreateConnection(ctx, "conn4", "config4")
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			system := NewSystem()
			if err := tt.setup(system); err != nil {
				t.Fatalf("failed to setup test: %v", err)
			}

			err := system.UpsertConnection(ctx, tt.config, tt.connectionName, tt.newDriver)
			if (err != nil) != tt.wantErr {
				t.Errorf("UpsertConnection() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr {
				if driver, ok := system.connectionToDriver[tt.connectionName]; !ok || driver != tt.newDriver {
					t.Errorf("connection %s not found or driver mismatch: got %v, want %v", tt.connectionName, driver, tt.newDriver)
				}
			}
		})
	}
}

func TestSystem_CloseConnection(t *testing.T) {
	tests := []struct {
		name           string
		setup          func(*System)
		connectionName string
		wantErr        bool
	}{
		{
			name: "close existing connection",
			setup: func(s *System) {
				s.connectionToDriver["test-connection"] = dapr.Name
				SupportedDrivers[dapr.Name] = dapr.FakeConn
				_ = dapr.FakeConn.CreateConnection(context.TODO(), "test-connection", map[string]interface{}{"component": "pubsub"})
			},
			connectionName: "test-connection",
			wantErr:        false,
		},
		{
			name: "close non-existing connection",
			setup: func(s *System) {
				// No setup needed for non-existing connection
				s.connectionToDriver = map[string]string{}
			},
			connectionName: "non-existing-connection",
			wantErr:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewSystem()
			if tt.setup != nil {
				tt.setup(s)
			}

			err := s.CloseConnection(tt.connectionName)
			if (err != nil) != tt.wantErr {
				t.Errorf("CloseConnection() error = %v, wantErr %v", err, tt.wantErr)
			}

			if _, exists := s.connectionToDriver[tt.connectionName]; exists && !tt.wantErr {
				t.Errorf("connection %s still exists after CloseConnection", tt.connectionName)
			}
		})
	}
}

func TestSystem_Publish(t *testing.T) {
	type fields struct {
		connections map[string]string
	}
	type args struct {
		ctx        context.Context
		connection string
		topic      string
		msg        interface{}
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr bool
	}{
		{
			name: "There are no connections established",
			fields: fields{
				connections: nil,
			},
			args:    args{ctx: context.Background(), connection: "audit", topic: "test", msg: nil},
			wantErr: true,
		},
		{
			name: "Exporting to a connection that does not exist",
			fields: fields{
				connections: map[string]string{"audit": dapr.Name},
			},
			args:    args{ctx: context.Background(), connection: "test", topic: "test", msg: nil},
			wantErr: true,
		},
		{
			name: "Exporting to a connection that does exist",
			fields: fields{
				connections: testSystem.connectionToDriver,
			},
			args:    args{ctx: context.Background(), connection: "dapr", topic: "test", msg: nil},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &System{
				mux:                sync.RWMutex{},
				connectionToDriver: tt.fields.connections,
			}
			if err := s.Publish(tt.args.ctx, tt.args.connection, tt.args.topic, tt.args.msg); (err != nil) != tt.wantErr {
				t.Errorf("System.Publish() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSystem_closeConnection(t *testing.T) {
	ctx := context.Background()

	type args struct {
		connectionName string
	}
	tests := []struct {
		name                string
		setup               func(*System)
		args                args
		wantErr             bool
		expectConnectionDel bool
	}{
		{
			name: "close existing connection with supported driver",
			setup: func(s *System) {
				s.connectionToDriver["conn1"] = dapr.Name
				SupportedDrivers[dapr.Name] = dapr.FakeConn
				_ = dapr.FakeConn.CreateConnection(ctx, "conn1", map[string]interface{}{"component": "pubsub"})
			},
			args:                args{connectionName: "conn1"},
			wantErr:             false,
			expectConnectionDel: true,
		},
		{
			name: "close connection with unsupported driver",
			setup: func(s *System) {
				s.connectionToDriver["conn2"] = "unsupported"
				// Do not add to SupportedDrivers
			},
			args:                args{connectionName: "conn2"},
			wantErr:             false,
			expectConnectionDel: true,
		},
		{
			name: "close connection returns error from driver",
			setup: func(s *System) {
				s.connectionToDriver["conn3"] = testdriver.ErrName
				SupportedDrivers[testdriver.ErrName] = testdriver.FakeErrConn
				_ = SupportedDrivers[testdriver.ErrName].CreateConnection(ctx, "conn3", "config3")
			},
			args:                args{connectionName: "conn3"},
			wantErr:             true,
			expectConnectionDel: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewSystem()
			if tt.setup != nil {
				tt.setup(s)
			}
			err := s.closeConnection(tt.args.connectionName)
			if (err != nil) != tt.wantErr {
				t.Errorf("closeConnection() error = %v, wantErr %v", err, tt.wantErr)
			}
			_, exists := s.connectionToDriver[tt.args.connectionName]
			if tt.expectConnectionDel && exists {
				t.Errorf("connection %s should have been deleted from map", tt.args.connectionName)
			}
		})
	}
}

// concurrentTestDriver is a minimal thread-safe driver implementation used to
// verify that the export system behaves correctly when accessed from multiple
// goroutines. It tracks how many times Publish is called so the test can
// assert that publishers actually reached the driver.
type concurrentTestDriver struct {
	mu           sync.Mutex
	publishCalls int
}

func (d *concurrentTestDriver) Publish(_ context.Context, _ string, _ interface{}, _ string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.publishCalls++
	return nil
}

func (d *concurrentTestDriver) CloseConnection(_ string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return nil
}

func (d *concurrentTestDriver) UpdateConnection(_ context.Context, _ string, _ interface{}) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return nil
}

func (d *concurrentTestDriver) CreateConnection(_ context.Context, _ string, _ interface{}) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return nil
}

func TestSystem_ConcurrentAccess(t *testing.T) {
	ctx := context.Background()

	// Preserve the original drivers so this test does not affect others.
	origDrivers := SupportedDrivers
	t.Cleanup(func() {
		SupportedDrivers = origDrivers
	})

	const driverName = "concurrent-test-driver"
	testDrv := &concurrentTestDriver{}
	SupportedDrivers = map[string]driver.Driver{
		driverName: testDrv,
	}

	sys := NewSystem()

	const (
		connectionName = "conn-concurrent"
		publishers     = 16
		upserters      = 16
		closers        = 8
	)

	var wg sync.WaitGroup

	// Run multiple goroutines that concurrently upsert, publish to, and close
	// the same logical connection. This exercises the internal locking on the
	// System type and ensures there are no data races when used from a
	// multi-threaded environment.

	for i := 0; i < upserters; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			cfg := map[string]interface{}{
				"component": i,
			}
			if err := sys.UpsertConnection(ctx, cfg, connectionName, driverName); err != nil {
				t.Errorf("UpsertConnection failed: %v", err)
			}
		}(i)
	}

	for i := 0; i < publishers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Publish may legitimately fail if the connection has not yet
			// been created or has been closed; we only assert that it does
			// not panic and that the system remains usable.
			_ = sys.Publish(ctx, connectionName, "subject", nil)
		}()
	}

	for i := 0; i < closers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = sys.CloseConnection(connectionName)
		}()
	}

	wg.Wait()

	// Ensure at least one publish reached the driver so we know publishers
	// were able to observe a usable connection during concurrent access.
	testDrv.mu.Lock()
	publishCount := testDrv.publishCalls
	testDrv.mu.Unlock()
	if publishCount == 0 {
		t.Fatalf("expected at least one successful Publish call, got %d", publishCount)
	}

	// Ensure internal mapping is in a valid state (either the connection is
	// present and mapped to the expected driver, or it has been fully removed).
	sys.mux.RLock()
	defer sys.mux.RUnlock()

	if len(sys.connectionToDriver) > 1 {
		t.Fatalf("expected at most one connection, got %d", len(sys.connectionToDriver))
	}
	if d, ok := sys.connectionToDriver[connectionName]; ok && d != driverName {
		t.Fatalf("connection mapped to unexpected driver, got %q, want %q", d, driverName)
	}
}
