package debug

import "log"

var enabled bool

// SetEnabled turns debug logging on or off (-d).
func SetEnabled(on bool) {
	enabled = on
}

// Enabled reports whether debug mode is active.
func Enabled() bool {
	return enabled
}

// Logf writes a debug line when -d is set.
func Logf(format string, args ...any) {
	if enabled {
		log.Printf(format, args...)
	}
}
