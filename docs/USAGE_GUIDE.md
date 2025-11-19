# Usage Guide - Pure-Go Race Detector

Complete guide to using the `racedetector` tool for detecting data races in Go programs.

## Table of Contents

- [Quick Start](#quick-start)
- [Command Reference](#command-reference)
  - [build Command](#build-command)
  - [run Command](#run-command)
  - [test Command](#test-command) (future)
- [Integration with CI/CD](#integration-with-cicd)
- [Best Practices](#best-practices)
- [Current Limitations](#current-limitations)
- [FAQ](#faq)

---

## Quick Start

### Detect a Race in 60 Seconds

```bash
# Create a simple program with a data race
cat > race_example.go <<'EOF'
package main

import (
    "fmt"
    "time"
)

func main() {
    var counter int  // Shared variable - NO SYNCHRONIZATION!

    // Start 10 goroutines that all modify counter
    for i := 0; i < 10; i++ {
        go func(id int) {
            val := counter    // READ
            counter = val + 1 // WRITE
            fmt.Printf("Goroutine %d: counter = %d\n", id, counter)
        }(i)
    }

    time.Sleep(100 * time.Millisecond)
    fmt.Printf("Final counter: %d (expected 10)\n", counter)
}
EOF

# Run with race detection
racedetector run race_example.go

# Or build first, then run
racedetector build -o race_example race_example.go
./race_example

# Clean up
rm race_example.go race_example
```

**Expected behavior:**
- Program runs successfully
- `race.Init()` is called automatically
- In Phase 6A (current), no race is reported yet
- In Phase 2 (future), will report: "WARNING: DATA RACE"

---

## Command Reference

### `build` Command

Build a Go program with automatic race detection instrumentation.

**Syntax:**
```bash
racedetector build [go-build-flags] <source-files>
```

**Description:**
Drop-in replacement for `go build` that automatically:
1. Parses Go source files
2. Injects race detector imports
3. Instruments memory accesses (Phase 2 future)
4. Injects `race.Init()` at program start
5. Links Pure-Go race detector runtime
6. Builds instrumented binary

**Examples:**

```bash
# Simple build (single file)
racedetector build main.go

# Build with output file
racedetector build -o myapp main.go

# Build with multiple source files
racedetector build -o server main.go handler.go db.go

# Build with ldflags (strip debug info)
racedetector build -ldflags="-s -w" -o myapp main.go

# Build with build tags
racedetector build -tags=production,debug -o myapp main.go

# Build current directory
racedetector build -o myapp .

# Build with all standard go build flags
racedetector build -v -x -ldflags="-X main.version=1.0.0" main.go
```

**Supported Flags:**

All standard `go build` flags are supported:

| Flag | Description |
|------|-------------|
| `-o <file>` | Output file path |
| `-v` | Verbose output |
| `-x` | Print commands |
| `-ldflags="..."` | Linker flags |
| `-tags="..."` | Build tags |
| `-trimpath` | Remove file paths from binary |
| `-buildmode=<mode>` | Build mode (default, exe, pie, etc.) |
| `-race` | (Ignored - race detection always enabled) |

**Flag Format:**

Both formats are supported:
```bash
-o myapp        # Space-separated
-o=myapp        # Equals-separated
```

**Output:**

```
Built successfully: myapp
```

**What Happens Behind the Scenes:**

1. Creates temporary workspace
2. Copies source files
3. Parses AST and instruments code
4. Generates `go.mod` overlay with race detector dependency
5. Runs `go mod tidy`
6. Compiles with `go build`
7. Moves binary to output path
8. Cleans up temporary files

**Phase 6A MVP Status:**
- ✅ Import injection works
- ✅ `race.Init()` is called automatically
- ✅ All build flags supported
- ⏳ Full memory access instrumentation (Phase 2)

---

### `run` Command

Run a Go program with automatic race detection.

**Syntax:**
```bash
racedetector run [go-build-flags] <source-files> [-- program-args]
```

**Description:**
Drop-in replacement for `go run` that builds instrumented binary in a temporary location and executes it.

**Examples:**

```bash
# Simple run (single file)
racedetector run main.go

# Run with program arguments
racedetector run main.go arg1 arg2

# Run with program flags
racedetector run main.go --port=8080 --debug

# Run multiple source files with arguments
racedetector run main.go helper.go -- --config=prod.yaml

# Run with build flags
racedetector run -ldflags="-s -w" main.go

# Run with build tags
racedetector run -tags=debug main.go --verbose
```

**Argument Parsing:**

The command intelligently separates build flags from program arguments:

```bash
racedetector run -ldflags="-X main.version=1.0" main.go --port 8080
                 ^build flags^                  ^source^ ^program args^
```

**Flow:**
1. Build flags come first (start with `-` and before any `.go` file)
2. Source files (any `.go` file)
3. Program arguments (everything after source files)

**Output Forwarding:**

All streams are forwarded transparently:
- **stdin**: Interactive input works
- **stdout**: Program output printed directly
- **stderr**: Error messages printed directly
- **exit code**: Exit code from program propagated

**Cleanup:**

Temporary binary is automatically deleted after execution (even on Ctrl+C).

**Phase 6A MVP Status:**
- ✅ Builds and runs successfully
- ✅ Argument parsing works correctly
- ✅ Stream forwarding works
- ⏳ Actual race detection (Phase 2)

---

### `test` Command

**(Future - Not yet implemented in Phase 6A MVP)**

Run Go tests with race detection.

**Planned Syntax:**
```bash
racedetector test [go-test-flags] [packages]
```

**Planned Examples:**
```bash
# Test current package
racedetector test

# Test with verbose output
racedetector test -v

# Test all packages
racedetector test ./...

# Test with coverage
racedetector test -cover ./internal/...

# Test specific function
racedetector test -run=TestMyFunction

# Benchmark tests
racedetector test -bench=. -benchmem
```

**Status:** Task A.6 (OPTIONAL - may be skipped for Path B priority)

---

## Integration with CI/CD

### GitHub Actions

Add race detection to your CI pipeline:

```yaml
# .github/workflows/test.yml
name: Tests with Race Detection

on: [push, pull_request]

jobs:
  race-detection:
    runs-on: ubuntu-latest
    steps:
      - name: Checkout code
        uses: actions/checkout@v3

      - name: Setup Go
        uses: actions/setup-go@v4
        with:
          go-version: '1.21'

      - name: Install racedetector
        run: go install github.com/kolkov/racedetector/cmd/racedetector@latest

      - name: Build with race detection
        run: racedetector build ./cmd/myapp

      - name: Run integration tests with race detection
        run: |
          for test in tests/*.go; do
            echo "Running $test..."
            racedetector run "$test" || exit 1
          done
```

### GitLab CI

```yaml
# .gitlab-ci.yml
test:race-detection:
  image: golang:1.21
  stage: test
  script:
    - go install github.com/kolkov/racedetector/cmd/racedetector@latest
    - export PATH="$HOME/go/bin:$PATH"
    - racedetector build ./...
    - racedetector run ./tests/integration_test.go
  artifacts:
    reports:
      junit: race-report.xml
```

### Jenkins

```groovy
// Jenkinsfile
pipeline {
    agent any
    stages {
        stage('Race Detection') {
            steps {
                sh 'go install github.com/kolkov/racedetector/cmd/racedetector@latest'
                sh 'racedetector build ./cmd/myapp'
                sh 'racedetector run ./tests/integration_test.go'
            }
        }
    }
}
```

### Docker Build

```dockerfile
# Dockerfile
FROM golang:1.21 AS builder

# Install racedetector
RUN go install github.com/kolkov/racedetector/cmd/racedetector@latest

WORKDIR /app
COPY . .

# Build with race detection
RUN racedetector build -o myapp ./cmd/myapp

# Final image
FROM alpine:latest
COPY --from=builder /app/myapp /usr/local/bin/
ENTRYPOINT ["/usr/local/bin/myapp"]
```

---

## Best Practices

### When to Use Race Detector

**Always use during:**
- Development (catch races early)
- Code review (validate concurrent code)
- Integration testing (test real scenarios)
- Pre-production testing (final validation)

**Consider skipping for:**
- Production builds (performance overhead)
- Benchmarking (distorts results)
- Single-threaded code (no concurrency)

### How to Fix Races

When a race is detected (in Phase 2 future):

**1. Use Mutexes:**
```go
var (
    counter int
    mu      sync.Mutex
)

func increment() {
    mu.Lock()
    counter++
    mu.Unlock()
}
```

**2. Use Channels:**
```go
func worker(ch chan int) {
    for val := range ch {
        process(val)
    }
}
```

**3. Use Atomic Operations:**
```go
var counter int64

func increment() {
    atomic.AddInt64(&counter, 1)
}
```

**4. Use sync.Once:**
```go
var once sync.Once
var config *Config

func getConfig() *Config {
    once.Do(func() {
        config = loadConfig()
    })
    return config
}
```

### Performance Tips

**Current (Phase 6A MVP):**
- Overhead: ~1-2 seconds for build/run (one-time instrumentation)
- Runtime: Minimal overhead (only `race.Init()` call)

**Future (Phase 2):**
- Expected overhead: 5-15x slowdown (typical for race detectors)
- Memory overhead: 5-10x (shadow memory tracking)

**Optimization:**
- Run race detection in CI, not production
- Use for integration tests, not unit tests
- Profile hot paths separately (without race detection)

---

## Current Limitations

### Phase 6A MVP (Current - November 2025)

**What Works:**
- ✅ AST parsing and import injection
- ✅ `race.Init()` automatic insertion
- ✅ `build` and `run` commands
- ✅ All `go build` flags supported
- ✅ Argument parsing and stream forwarding

**What Doesn't Work Yet:**
- ❌ Actual race detection during runtime (Phase 2 future work)
- ❌ Race report generation (Phase 2)
- ❌ RaceRead/RaceWrite instrumentation (Phase 2)
- ❌ `test` command (Task A.6 - OPTIONAL, may skip)

**Timeline:**
- Phase 6A completion: December 15, 2025 (Path A standalone tool)
- Phase 2 implementation: January 2026 (Path B runtime integration)
- Full race detection: Q1 2026

### Known Issues

**Issue 1: No race detection in Phase 6A MVP**
- **Impact:** Programs run but races aren't detected yet
- **Workaround:** Use Go's standard race detector (`go run -race`)
- **Fix:** Phase 2 will add full detection

**Issue 2: Test command not implemented**
- **Impact:** Can't use `racedetector test`
- **Workaround:** Use `racedetector run test_file.go` for manual tests
- **Fix:** Task A.6 (OPTIONAL - may be skipped)

---

## FAQ

### General Questions

**Q: What is racedetector?**
A: A Pure-Go race detector that works without CGO dependency, enabling race detection with `CGO_ENABLED=0`.

**Q: How is this different from `go run -race`?**
A: Standard Go race detector requires CGO (ThreadSanitizer C++ library). `racedetector` is 100% Go, works without CGO, perfect for Docker containers and cross-compilation.

**Q: Is this production-ready?**
A: Phase 6A MVP (current) is a proof-of-concept. Full production-readiness targeted for Q1 2026 (after Phase 2 runtime integration).

### Installation

**Q: Which Go version do I need?**
A: Go 1.21 or higher. Run `go version` to check.

**Q: Can I use this with older Go versions?**
A: No, requires Go 1.21+ for modern AST features and structured logging.

**Q: Does this work on Windows?**
A: Yes! Fully tested on Windows 10/11, Linux, and macOS.

### Usage

**Q: Can I use this with existing projects?**
A: Yes! Drop-in replacement for `go build` and `go run`.

**Q: Does this slow down my program?**
A: Phase 6A MVP: Minimal overhead (only Init call). Phase 2 (future): 5-15x slowdown (typical for race detectors).

**Q: Can I use this in CI/CD?**
A: Yes! See [Integration with CI/CD](#integration-with-cicd) section.

**Q: Does this detect ALL races?**
A: Phase 6A MVP: No (foundation only). Phase 2 (future): Yes, all races that Go's standard detector finds, plus some it misses (using FastTrack algorithm).

### Technical

**Q: What algorithm does this use?**
A: FastTrack algorithm (PLDI 2009), same as ThreadSanitizer but implemented in Pure Go.

**Q: How does instrumentation work?**
A: Parses Go AST, injects race detector imports, adds Init() call, and (Phase 2 future) inserts RaceRead/RaceWrite around memory accesses.

**Q: Can I see the instrumented code?**
A: Phase 6A MVP: Code is in temporary workspace (auto-deleted). Phase 2 (future): Will add `-x` flag to show instrumented code.

**Q: Does this modify my source files?**
A: NO! All instrumentation happens in a temporary copy. Original files are never touched.

### Troubleshooting

**Q: Error: "command not found: racedetector"**
A: Install path not in PATH. See [Installation Guide - Troubleshooting](INSTALLATION.md#troubleshooting).

**Q: Error: "cannot find package"**
A: Missing dependencies. Run `go mod tidy`.

**Q: Build fails with "syntax error"**
A: Likely a bug in AST instrumentation. Please report with code sample.

**Q: Program runs but no race detected**
A: Phase 6A MVP doesn't detect races yet (foundation only). Phase 2 (Q1 2026) will add detection.

### Contributing

**Q: How can I contribute?**
A: See [CONTRIBUTING.md](../CONTRIBUTING.md) (future). Currently a solo research project, will open for contributions after Path B completion.

**Q: Can I use this in my company?**
A: Yes! MIT/Apache 2.0 license (TBD). Currently in alpha, recommend waiting for Phase 2 completion (Q1 2026).

**Q: Is there a roadmap?**
A: Yes! See [PRODUCTION_ROADMAP.md](PRODUCTION_ROADMAP.md) for full 12-month plan.

---

## Getting Help

If you need help:

1. Read this guide thoroughly
2. Check [Installation Guide](INSTALLATION.md)
3. Try [Dogfooding Demo](../examples/dogfooding/)
4. Search [GitHub Issues](https://github.com/kolkov/racedetector/issues)
5. Open a new issue with:
   - OS, Go version
   - Full command and output
   - Minimal reproduction case

---

## Next Steps

- Read [Architecture Documentation](dev/ARCHITECTURE.md) - Understand how it works
- Try [Integration Examples](../examples/integration/) - Real-world use cases
- Follow [Roadmap](PRODUCTION_ROADMAP.md) - Track progress to production

---

*Last Updated: November 19, 2025*
*Version: 0.1.0-alpha (Phase 6A MVP)*
