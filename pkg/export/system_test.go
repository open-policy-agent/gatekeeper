package export

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/export/dapr"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/export/disk"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/export/driver"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/export/testdriver"
	exportutil "github.com/open-policy-agent/gatekeeper/v3/pkg/export/util"
	"github.com/stretchr/testify/assert"
)

var testSystem *System

type recordingDriver struct {
	published []any
}

func (driver *recordingDriver) Publish(_ context.Context, _ string, data interface{}, _ string) error {
	driver.published = append(driver.published, data)
	return nil
}

func (*recordingDriver) CloseConnection(string) error { return nil }

func (*recordingDriver) UpdateConnection(context.Context, string, interface{}) error { return nil }

func (*recordingDriver) CreateConnection(context.Context, string, interface{}) error { return nil }

func TestMain(m *testing.M) {
	ctx := context.Background()
	supportedDrivers = map[string]driver.Driver{
		dapr.Name: dapr.FakeConn,
	}
	testSystem = NewSystem()
	cfg := map[string]interface{}{
		dapr.Name: map[string]interface{}{
			"component": "pubsub",
		},
	}
	for name, fakeConn := range supportedDrivers {
		testSystem.connectionToDriver[name] = name
		_ = fakeConn.CreateConnection(ctx, name, cfg[name])
	}
	r := m.Run()
	for name, fakeConn := range testSystem.connectionToDriver {
		_ = supportedDrivers[fakeConn].CloseConnection(name)
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
				supportedDrivers[dapr.Name] = dapr.FakeConn
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
				supportedDrivers[dapr.Name] = dapr.FakeConn
				return supportedDrivers[dapr.Name].CreateConnection(ctx, "conn1", map[string]interface{}{"component": "pubsub"})
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
				supportedDrivers[dapr.Name] = dapr.FakeConn
				supportedDrivers[testdriver.Name] = testdriver.FakeConn
				return supportedDrivers[testdriver.Name].CreateConnection(ctx, "conn4", "config4")
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
				supportedDrivers[dapr.Name] = dapr.FakeConn
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

func TestSystemPublishBatchFallbackPreservesMessageTypes(t *testing.T) {
	const driverName = "recording"
	recorder := &recordingDriver{}
	oldDrivers := supportedDrivers
	supportedDrivers = map[string]driver.Driver{driverName: recorder}
	t.Cleanup(func() { supportedDrivers = oldDrivers })
	system := &System{connectionToDriver: map[string]string{"connection": driverName}}
	raw := json.RawMessage(`{"eventType":"violation_admission"}`)
	typed := exportutil.ExportMsg{ID: "audit-1", Message: exportutil.AuditStartedMsg}

	errorsByMessage := system.PublishBatch(context.Background(), "connection", "topic", []any{raw, typed})

	assert.Len(t, errorsByMessage, 2)
	assert.NoError(t, errorsByMessage[0])
	assert.NoError(t, errorsByMessage[1])
	assert.Equal(t, []any{raw, typed}, recorder.published)
}

func TestSystemSupportsAuditAndAdmissionOnSharedDiskConnection(t *testing.T) {
	oldDrivers := supportedDrivers
	supportedDrivers = map[string]driver.Driver{disk.Name: disk.Connections}
	t.Cleanup(func() { supportedDrivers = oldDrivers })

	ctx := context.Background()
	system := NewSystem()
	connectionName := "shared-disk-sources"
	path := t.TempDir()
	config := map[string]interface{}{
		"path":            path,
		"maxAuditResults": float64(1),
	}
	if err := system.UpsertConnection(ctx, config, connectionName, disk.Name); err != nil {
		t.Fatalf("UpsertConnection() error = %v", err)
	}
	t.Cleanup(func() { _ = system.CloseConnection(connectionName) })

	if err := system.Publish(ctx, connectionName, "audit", exportutil.ExportMsg{ID: "audit-1", Message: exportutil.AuditStartedMsg}); err != nil {
		t.Fatalf("Publish(audit start) error = %v", err)
	}
	admission, err := json.Marshal(exportutil.ExportMsg{EventType: exportutil.AdmissionViolationEventType, ResourceName: "denied-pod"})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	batchResults := system.PublishBatch(ctx, connectionName, "audit", []any{
		json.RawMessage(admission),
		exportutil.ExportMsg{EventType: exportutil.AdmissionViolationEventType, ResourceName: "second-denied-pod"},
	})
	for i, result := range batchResults {
		if result != nil {
			t.Fatalf("PublishBatch(admission) result %d error = %v", i, result)
		}
	}
	if err := system.Publish(ctx, connectionName, "audit", exportutil.ExportMsg{ID: "audit-1", Message: exportutil.AuditCompletedMsg}); err != nil {
		t.Fatalf("Publish(audit end) error = %v", err)
	}

	if _, err := os.Stat(filepath.Join(path, "audit", "audit-1.log")); err != nil {
		t.Fatalf("expected audit file: %v", err)
	}
	files, err := os.ReadDir(filepath.Join(path, "audit"))
	if err != nil || len(files) != 2 {
		t.Fatalf("expected audit and admission files in one channel, files=%v err=%v", files, err)
	}
	var foundAdmission bool
	for _, file := range files {
		foundAdmission = foundAdmission || strings.HasPrefix(file.Name(), "admission-")
	}
	if !foundAdmission {
		t.Fatalf("expected admission-prefixed file, got %v", files)
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
				supportedDrivers[dapr.Name] = dapr.FakeConn
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
				// Do not add to supportedDrivers
			},
			args:                args{connectionName: "conn2"},
			wantErr:             false,
			expectConnectionDel: true,
		},
		{
			name: "close connection returns error from driver",
			setup: func(s *System) {
				s.connectionToDriver["conn3"] = testdriver.ErrName
				supportedDrivers[testdriver.ErrName] = testdriver.FakeErrConn
				_ = supportedDrivers[testdriver.ErrName].CreateConnection(ctx, "conn3", "config3")
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
