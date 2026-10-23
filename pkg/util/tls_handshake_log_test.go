package util

import (
	"bytes"
	"log"
	"strings"
	"testing"
)

func TestIsBenignTLSHandshakeEOF(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		line string
		want bool
	}{
		{
			name: "stdlib eof",
			line: "http: TLS handshake error from 100.65.12.2:45678: EOF\n",
			want: true,
		},
		{
			name: "with default logger timestamp prefix",
			line: "2026/03/24 10:54:06 http: TLS handshake error from 100.65.12.2:45678: EOF\n",
			want: true,
		},
		{
			name: "without trailing newline",
			line: "http: TLS handshake error from 10.0.0.1:1234: EOF",
			want: true,
		},
		{
			name: "bad certificate still logged",
			line: "http: TLS handshake error from 10.0.0.1:1234: remote error: tls: bad certificate\n",
			want: false,
		},
		{
			name: "connection reset still logged",
			line: "http: TLS handshake error from 10.0.0.1:1234: read: connection reset by peer\n",
			want: false,
		},
		{
			name: "unrelated log line",
			line: "something else: EOF\n",
			want: false,
		},
		{
			name: "empty",
			line: "",
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isBenignTLSHandshakeEOF([]byte(tc.line)); got != tc.want {
				t.Fatalf("isBenignTLSHandshakeEOF(%q) = %v, want %v", tc.line, got, tc.want)
			}
		})
	}
}

func TestTLSHandshakeEOFFilterWrite(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	w := NewTLSHandshakeEOFFilter(&buf)

	if _, err := w.Write([]byte("http: TLS handshake error from 1.2.3.4:5: EOF\n")); err != nil {
		t.Fatalf("Write eof: %v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("expected EOF handshake line to be dropped, got %q", buf.String())
	}

	msg := "http: TLS handshake error from 1.2.3.4:5: remote error: tls: bad certificate\n"
	if _, err := w.Write([]byte(msg)); err != nil {
		t.Fatalf("Write other: %v", err)
	}
	if got := buf.String(); got != msg {
		t.Fatalf("expected non-EOF line to pass through, got %q want %q", got, msg)
	}
}

func TestFilterBenignTLSHandshakeErrors(t *testing.T) {
	var buf bytes.Buffer
	prevFlags := log.Flags()
	prevPrefix := log.Prefix()
	prevOut := log.Writer()
	t.Cleanup(func() {
		log.SetFlags(prevFlags)
		log.SetPrefix(prevPrefix)
		log.SetOutput(prevOut)
	})

	log.SetFlags(0)
	log.SetPrefix("")
	log.SetOutput(&buf)
	FilterBenignTLSHandshakeErrors()

	log.Print("http: TLS handshake error from 100.65.12.2:45678: EOF")
	log.Print("http: TLS handshake error from 100.65.12.2:45678: remote error: tls: bad certificate")
	log.Print("keep me")

	out := buf.String()
	if strings.Contains(out, "EOF") {
		t.Fatalf("expected EOF handshake noise to be filtered, got %q", out)
	}
	if !strings.Contains(out, "bad certificate") {
		t.Fatalf("expected non-EOF TLS error to remain, got %q", out)
	}
	if !strings.Contains(out, "keep me") {
		t.Fatalf("expected unrelated log to remain, got %q", out)
	}
}
