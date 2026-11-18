package audit

import "time"

// logReminderInterval is how often an unchanged failure is logged again.
const logReminderInterval = 10 * time.Minute

// logLimiter decides when a repeated failure is worth logging: when it first
// happens, when it changes, and as a reminder while it persists. Audit runs
// are sequential, so it needs no locking. The zero value is ready to use.
type logLimiter struct {
	now     func() time.Time
	failing bool
	lastKey string
	lastLog time.Time
}

// failed records a failure identified by key and reports whether to log it.
func (l *logLimiter) failed(key string) bool {
	now := time.Now()
	if l.now != nil {
		now = l.now()
	}
	if l.failing && l.lastKey == key && now.Sub(l.lastLog) < logReminderInterval {
		return false
	}
	l.failing = true
	l.lastKey = key
	l.lastLog = now
	return true
}

// succeeded records a success and reports whether it ends a failure.
func (l *logLimiter) succeeded() bool {
	recovered := l.failing
	l.reset()
	return recovered
}

// reset forgets any failure without reporting a recovery.
func (l *logLimiter) reset() {
	l.failing = false
	l.lastKey = ""
	l.lastLog = time.Time{}
}
