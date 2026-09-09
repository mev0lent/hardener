package ui

import "sync"

// LogEntry is unstyled text. A dashboard can render it without the plain
// printer's fixed-width boxes or terminal control sequences.
type LogEntry struct {
	Level   string
	Message string
}

var logSink struct {
	sync.RWMutex
	fn func(LogEntry)
}

// SetLogSink installs a sink for the duration of one CLI run. Call the returned
// restore function after the execution worker has finished.
func SetLogSink(fn func(LogEntry)) func() {
	logSink.Lock()
	previous := logSink.fn
	logSink.fn = fn
	logSink.Unlock()
	return func() {
		logSink.Lock()
		logSink.fn = previous
		logSink.Unlock()
	}
}

func emit(level, message string) bool {
	logSink.RLock()
	fn := logSink.fn
	logSink.RUnlock()
	if fn == nil {
		return false
	}
	fn(LogEntry{Level: level, Message: message})
	return true
}
