package logger

import (
	"fmt"
	"os"
)

// Stderr writes structured log messages to stderr.
type Stderr struct{}

// NewStderr creates a logger that writes to stderr.
func NewStderr() *Stderr {
	return &Stderr{}
}

// Info logs an informational message.
func (l *Stderr) Info(msg string, args ...any) {
	fmt.Fprintf(os.Stderr, "codetap: %s", msg)
	for i := 0; i+1 < len(args); i += 2 {
		fmt.Fprintf(os.Stderr, " %v=%v", args[i], args[i+1])
	}
	fmt.Fprintln(os.Stderr)
}

// Error logs an error message.
func (l *Stderr) Error(msg string, args ...any) {
	fmt.Fprintf(os.Stderr, "codetap: ERROR: %s", msg)
	for i := 0; i+1 < len(args); i += 2 {
		fmt.Fprintf(os.Stderr, " %v=%v", args[i], args[i+1])
	}
	fmt.Fprintln(os.Stderr)
}
