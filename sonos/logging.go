package sonos

import (
	"log"
	"sync/atomic"
)

var debugLogging atomic.Bool

// SetDebugLogging enables or disables verbose logging inside the sonos package.
func SetDebugLogging(enabled bool) {
	debugLogging.Store(enabled)
}

func isDebugLogging() bool {
	return debugLogging.Load()
}

func logDebug(format string, args ...interface{}) {
	if isDebugLogging() {
		log.Printf(format, args...)
	}
}

func logInfo(format string, args ...interface{}) {
	if isDebugLogging() {
		log.Printf(format, args...)
	}
}
