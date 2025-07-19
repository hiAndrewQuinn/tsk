// Package logger provides a unified interface for logging across the application.
// It supports different log levels and automatically handles debug-only messages.
package logger

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/hiAndrewQuinn/tsk/internal/config"
)

// getCallerFunctionName retrieves the name of the function that called it, skipping a
// specified number of stack frames.
func getCallerFunctionName(skip int) string {
	pc, _, _, ok := runtime.Caller(skip)
	if !ok {
		return "???"
	}
	fn := runtime.FuncForPC(pc)
	if fn == nil {
		return "???"
	}
	fullName := fn.Name()
	lastDot := strings.LastIndex(fullName, ".")
	return filepath.Base(fullName[:lastDot]) + "." + fullName[lastDot+1:]
}

// Enter logs a standard "Entering." message and returns the caller's function
// name. This is useful for pairing with a defer statement to log exiting.
// Only logs if config.Debug is true.
func Enter() string {
	if !config.Debug {
		return ""
	}
	caller := getCallerFunctionName(2)
	log.Printf("%s: Entering.", caller)
	return caller
}

// Tracef logs a formatted string, automatically prepending the caller's function name.
// Only logs if config.Debug is true.
func Tracef(format string, args ...any) {
	if !config.Debug {
		return
	}
	caller := getCallerFunctionName(2)
	log.Printf(fmt.Sprintf("%s: %s", caller, format), args...)
}

// Infof logs a formatted informational message. It always prints, regardless of debug mode.
// It uses the standard logger, which is configured in cmd/root.go.
func Infof(format string, args ...any) {
	log.Printf("[INFO] "+format, args...)
}

// Warnf logs a formatted warning message. It always prints, regardless of debug mode.
// Use this for non-fatal issues the user might need to know about.
func Warnf(format string, args ...any) {
	log.Printf("[WARN] "+format, args...)
}

// Fatalf logs a formatted error message to stderr and exits the program with status 1.
// Use this for unrecoverable errors.
func Fatalf(format string, args ...any) {
	// We use Fprintf here to guarantee output to stderr, even if the standard
	// logger is redirected to a file.
	fmt.Fprintf(os.Stderr, "[FATAL] "+format+"\n", args...)
	os.Exit(1)
}
