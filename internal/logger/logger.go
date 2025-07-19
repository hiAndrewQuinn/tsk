package logger

import (
	"fmt"
	"log"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/hiAndrewQuinn/tsk/internal/config"
)

// getCallerFunctionName retrieves the name of the function that called it.
func getCallerFunctionName() string {
	// runtime.Caller(2) goes up two call stacks:
	// 0: getCallerFunctionName() (this function)
	// 1: Enter() or Tracef() (the function that called this one)
	// 2: The actual function we want to log (e.g., findLongestPrefix)
	pc, _, _, ok := runtime.Caller(2)
	if !ok {
		return "???"
	}

	// Get the function object from the program counter
	fn := runtime.FuncForPC(pc)
	if fn == nil {
		return "???"
	}

	// The name includes the full package path, so we clean it up.
	// e.g., "github.com/hiAndrewQuinn/tsk/internal/data.findLongestPrefix"
	// becomes "data.findLongestPrefix"
	fullName := fn.Name()
	lastDot := strings.LastIndex(fullName, ".")

	// filepath.Base gets the last path element, which is the function name.
	return filepath.Base(fullName[:lastDot]) + "." + fullName[lastDot+1:]
}

// Enter logs a standard "Entering." message and returns the caller's function
// name. This is useful for pairing with a defer statement to log exiting.
func Enter() string {
	if !config.Debug {
		return "" // Return early if not debugging
	}
	caller := getCallerFunctionName()
	log.Printf("%s: Entering.", caller)
	return caller
}

// Tracef logs a formatted string, automatically prepending the caller's function name.
func Tracef(format string, args ...any) {
	if !config.Debug {
		return // Return early if not debugging
	}
	caller := getCallerFunctionName()
	log.Printf(fmt.Sprintf("%s: %s", caller, format), args...)
}
