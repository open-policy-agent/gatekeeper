package util

import (
	"io"
	"log"
	"strings"
)

// tlsHandshakeEOFFilter suppresses benign net/http TLS handshake EOF noise.
//
// Infrastructure TCP probes (load balancer / CNI health checks) often dial the
// webhook port and disconnect without completing a TLS handshake. Go's
// http.Server logs those as:
//
//	http: TLS handshake error from <addr>: EOF
//
// These messages are not actionable for Gatekeeper and can flood controller
// logs. See https://github.com/golang/go/issues/26918 and
// https://github.com/open-policy-agent/gatekeeper/issues/2142.
type tlsHandshakeEOFFilter struct {
	out io.Writer
}

// NewTLSHandshakeEOFFilter returns a writer that drops benign TLS handshake
// EOF lines and forwards all other output unchanged.
func NewTLSHandshakeEOFFilter(out io.Writer) io.Writer {
	if out == nil {
		out = io.Discard
	}
	return &tlsHandshakeEOFFilter{out: out}
}

func (f *tlsHandshakeEOFFilter) Write(p []byte) (int, error) {
	if isBenignTLSHandshakeEOF(p) {
		return len(p), nil
	}
	return f.out.Write(p)
}

func isBenignTLSHandshakeEOF(p []byte) bool {
	// stdlib formats: "http: TLS handshake error from %s: %v\n"
	// When written via the default logger, a date/time prefix may be present.
	s := string(p)
	if !strings.Contains(s, "http: TLS handshake error from") {
		return false
	}
	s = strings.TrimRight(s, "\n")
	return strings.HasSuffix(s, ": EOF")
}

// FilterBenignTLSHandshakeErrors installs a filter on the standard library
// logger used by http.Server when ErrorLog is unset (controller-runtime's
// webhook server). Non-EOF TLS errors continue to be logged.
func FilterBenignTLSHandshakeErrors() {
	log.SetOutput(NewTLSHandshakeEOFFilter(log.Writer()))
}
