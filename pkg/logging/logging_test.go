package logging

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/go-logr/zapr"
	constraintclient "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	"github.com/open-policy-agent/frameworks/constraint/pkg/instrumentation"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func Test_LogStatsEntries(t *testing.T) {
	testBuf := &bytes.Buffer{}

	encCfg := zap.NewProductionEncoderConfig()
	enc := zapcore.NewJSONEncoder(encCfg)

	core := &fakeCore{
		LevelEnabler: zap.LevelEnablerFunc(func(lvl zapcore.Level) bool {
			return lvl >= zapcore.InfoLevel
		}),
		enc:    enc,
		buffer: testBuf,
	}

	zapLogger := zap.New(core)
	defer require.NoError(t, zapLogger.Sync())
	testLogger := zapr.NewLogger(zapLogger)

	// test log lines show up
	LogStatsEntries(
		&constraintclient.Client{},
		testLogger,
		[]*instrumentation.StatsEntry{
			{
				Scope:    "someScope",
				StatsFor: "someConstranint",
				Stats: []*instrumentation.Stat{
					{
						Name:  "someStat",
						Value: "someValue",
						Source: instrumentation.Source{
							Type:  "someType",
							Value: "someValue",
						},
					},
				},
				Labels: []*instrumentation.Label{
					{
						Name:  "someLabel",
						Value: "someLabelValue",
					},
				},
			},
		},
		"test message",
	)

	expectedLogLine := fmt.Sprintf("\"msg\":\"test message\",\"execution_stats\":[{\"scope\":\"someScope\","+
		"\"statsFor\":\"someConstranint\",\"stats\":[{\"name\":\"someStat\",\"value\":\"someValue\","+
		"\"source\":{\"type\":\"someType\",\"value\":\"someValue\"},\"description\":\"%s\"}],"+
		"\"labels\":[{\"name\":\"someLabel\",\"value\":\"someLabelValue\"}]}]}\n", instrumentation.UnknownDescription)
	require.Contains(t, testBuf.String(), expectedLogLine)

	// test that empty stats don't log
	LogStatsEntries(
		&constraintclient.Client{},
		testLogger,
		[]*instrumentation.StatsEntry{},
		"this message should not be logged",
	)
	require.NotContains(t, testBuf.String(), "this message should not be logged")
}

//// logging utilities for testing below /////

// Testing zapcore.Core implementation to intercept log entries in a buffer by choice.
// Consumers can inspect contents of the buffer to see if it matches expectations.
// Reusing buffers or loggers not encouraged since this implementation is not focused
// on thread safety, concurrency or reusability.
type fakeCore struct {
	zapcore.LevelEnabler
	enc    zapcore.Encoder
	buffer *bytes.Buffer
}

func (c *fakeCore) With(fields []zapcore.Field) zapcore.Core {
	clone := c.clone()
	for _, f := range fields {
		f.AddTo(clone.enc)
	}
	return clone
}

func (c *fakeCore) Check(e zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry { //nolint:gocritic
	if c.Enabled(e.Level) {
		return ce.AddCore(e, c)
	}
	return ce
}

func (c *fakeCore) Write(e zapcore.Entry, fields []zapcore.Field) error { //nolint:gocritic
	for _, f := range fields {
		f.AddTo(c.enc)
	}
	buf, err := c.enc.EncodeEntry(e, fields)
	if err != nil {
		return err
	}
	_, err = c.buffer.Write(buf.Bytes())
	return err
}

func (c *fakeCore) Sync() error {
	return nil // TODO(acpana): revisit implementation for GC / https://github.com/open-policy-agent/gatekeeper/issues/2748
}

func (c *fakeCore) clone() *fakeCore {
	return &fakeCore{
		LevelEnabler: c.LevelEnabler,
		enc:          c.enc.Clone(),
		buffer:       c.buffer,
	}
}
