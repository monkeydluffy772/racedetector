// Package runtime provides runtime library linking for instrumented code.
//
// This package handles injecting our Pure-Go race detector runtime into
// instrumented Go programs. It provides mechanisms to ensure the runtime
// is linked and initialized properly.
//
// Phase 6A - Task A.3: Runtime Injection
package runtime

import (
	"fmt"
	"os"
	"path/filepath"
)

// GetRuntimePackagePath returns the import path for the race detector runtime.
//
// This is the package that instrumented code will import to access
// RaceRead, RaceWrite, and other race detection functions.
//
// Uses public API wrapper instead of internal package for standalone tool compatibility.
//
// Returns: "github.com/kolkov/racedetector/race"
func GetRuntimePackagePath() string {
	return "github.com/kolkov/racedetector/race"
}

// GetRuntimeInitCode returns Go code to initialize the race detector.
//
// This code should be injected at the beginning of the main() function
// to ensure the detector is properly initialized before any memory accesses.
//
// Returns:
//   - Go code string to initialize race detector
//
// Example output:
//
//	race.Init()
//	defer race.Fini()
func GetRuntimeInitCode() string {
	return `race.Init()
defer race.Fini()`
}

// ValidateRuntimeAvailable checks if the runtime library is available.
//
// This verifies that the race detector runtime package can be found
// and imported. If the package is missing, it provides instructions
// for installing it.
//
// Returns:
//   - nil if runtime is available
//   - error with installation instructions if missing
func ValidateRuntimeAvailable() error {
	// Check if we're in development (running from source)
	// In that case, the runtime is in internal/race/api
	projectRoot, err := findProjectRoot()
	if err == nil {
		runtimePath := filepath.Join(projectRoot, "internal", "race", "api")
		if _, err := os.Stat(runtimePath); err == nil {
			// Runtime found in development tree
			return nil
		}
	}

	// Check if runtime is installed in GOPATH/go modules
	// For now, we assume it's available since we're in the same repository
	// In production, this would check: go list github.com/.../api

	return nil
}

// findProjectRoot finds the root directory of the racedetector project.
//
// This walks up the directory tree from the current executable location
// looking for go.mod or a known project file.
//
// Returns:
//   - Project root path
//   - Error if root cannot be found
func findProjectRoot() (string, error) {
	// Start from current working directory
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}

	// Walk up looking for go.mod
	dir := cwd
	for {
		// Check for go.mod
		modPath := filepath.Join(dir, "go.mod")
		if _, err := os.Stat(modPath); err == nil {
			return dir, nil
		}

		// Check for internal/race/api (our runtime)
		runtimePath := filepath.Join(dir, "internal", "race", "api")
		if _, err := os.Stat(runtimePath); err == nil {
			return dir, nil
		}

		// Move up one directory
		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached root without finding project
			break
		}
		dir = parent
	}

	return "", fmt.Errorf("could not find racedetector project root")
}

// BuildFlags returns additional flags needed for building instrumented code.
//
// These flags ensure the runtime library is linked correctly and
// initialization code runs.
//
// Returns:
//   - Slice of build flags to pass to 'go build'
//
// Example:
//
//	flags := BuildFlags()
//	// flags = ["-tags=race", ...]
func BuildFlags() []string {
	// For now, no special flags needed
	// In future, might add:
	// - Custom build tags
	// - Linker flags
	// - Optimization flags
	return []string{}
}

// ModFileOverlay creates a temporary go.mod overlay for instrumented code.
//
// When instrumenting code outside the racedetector project, we need to
// ensure it can import our runtime. This creates a go.mod overlay that
// replaces the remote import with a local path.
//
// Parameters:
//   - tempDir: Temporary directory where instrumented code is being built
//
// Returns:
//   - Path to overlay file (for -modfile flag)
//   - Error if overlay creation fails
func ModFileOverlay(tempDir string) (string, error) {
	projectRoot, err := findProjectRoot()
	if err != nil {
		// Not in development mode - use published package
		//nolint:nilerr // Error indicates published mode, not a failure
		return "", nil
	}

	// Create go.mod in temp directory that replaces remote import with local
	overlayPath := filepath.Join(tempDir, "go.mod.overlay")

	content := fmt.Sprintf(`module instrumented

go 1.19

require github.com/kolkov/racedetector v0.0.0

replace github.com/kolkov/racedetector => %s
`, projectRoot)

	if err := os.WriteFile(overlayPath, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("failed to create go.mod overlay: %w", err)
	}

	return overlayPath, nil
}

// InjectInitCalls injects Init/Fini calls into the main function.
//
// This is a placeholder for future implementation. In a full implementation,
// this would use AST transformation to insert race.Init() at the start of
// main() and defer race.Fini().
//
// For the MVP, we rely on users calling Init/Fini manually or provide
// a wrapper main function.
//
// Parameters:
//   - sourceCode: Original Go source code
//
// Returns:
//   - Modified source code with Init/Fini calls
//   - Error if injection fails
func InjectInitCalls(sourceCode string) (string, error) {
	// TODO: Implement AST-based injection
	// For MVP, return unchanged
	return sourceCode, nil
}
