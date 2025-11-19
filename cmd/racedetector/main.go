// Package main implements the racedetector CLI tool.
//
// The racedetector tool provides automatic race detection for Go programs
// without requiring a custom Go toolchain or CGO. It works by:
//
//  1. Parsing Go source files using go/ast
//  2. Instrumenting memory accesses with race detection calls
//  3. Injecting the Pure-Go race detector runtime
//  4. Building/running the instrumented code
//
// Usage:
//
//	racedetector build main.go     # Build with race detection
//	racedetector run main.go       # Run with race detection
//	racedetector test ./...        # Test with race detection
//
// The tool is fully compatible with standard Go toolchain and can be used
// as a drop-in replacement for `go build`, `go run`, and `go test` when
// race detection is needed.
//
// This is the CLI entry point for the standalone race detector tool.
package main

import (
	"fmt"
	"os"
)

const version = "0.1.0"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]

	switch command {
	case "build":
		buildCommand(os.Args[2:])
	case "run":
		runCommand(os.Args[2:])
	case "test":
		testCommand(os.Args[2:])
	case "version", "--version", "-v":
		fmt.Printf("racedetector version %s\n", version)
	case "help", "--help", "-h":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", command)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Print(`racedetector - Pure-Go Race Detector Tool

USAGE:
    racedetector <command> [arguments]

COMMANDS:
    build      Build Go program with race detection
    run        Run Go program with race detection
    test       Test Go packages with race detection
    version    Show version information
    help       Show this help message

EXAMPLES:
    # Build a program with race detection
    racedetector build -o myapp main.go

    # Run a program with race detection
    racedetector run main.go --flag=value

    # Test packages with race detection
    racedetector test -v ./...

    # Test with coverage
    racedetector test -cover ./internal/...

ABOUT:
    racedetector is a standalone tool that provides race detection for Go
    programs without requiring CGO or a custom Go toolchain. It uses the
    Pure-Go Race Detector implementation based on the FastTrack algorithm.

    Unlike the standard Go race detector (which requires CGO_ENABLED=1),
    racedetector works with CGO_ENABLED=0, making it suitable for:
    - Docker containers
    - Cross-compilation
    - Embedded systems
    - Any environment where CGO is not available

    The tool automatically instruments your Go code at the AST level,
    inserting race detection calls and injecting the Pure-Go runtime.

FOR MORE INFORMATION:
    Repository: https://github.com/kolkov/racedetector
    Documentation: https://github.com/kolkov/racedetector/blob/main/README.md
    Issues: https://github.com/kolkov/racedetector/issues

`)
}

// buildCommand is implemented in build.go
// runCommand is implemented in run.go

// testCommand implements the 'racedetector test' command.
//
// This will instrument and run tests with race detection enabled.
// Currently not implemented - planned for v0.2.0.
//
// Example:
//
//	racedetector test -v ./internal/...
func testCommand(_ []string) {
	// TODO: Implement test command in v0.2.0 (args unused until implemented)
	fmt.Fprintln(os.Stderr, "Error: 'test' command not yet implemented")
	fmt.Fprintln(os.Stderr, "This will be implemented in v0.2.0")
	os.Exit(1)
}
