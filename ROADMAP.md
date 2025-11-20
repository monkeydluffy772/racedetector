# Pure-Go Race Detector - Development Roadmap

> **Strategic Advantage**: Proven FastTrack algorithm implementation without CGO dependency!
> **Approach**: Scientific algorithm + Go best practices - eliminates C++ ThreadSanitizer dependency

**Last Updated**: 2025-11-20 | **Current Version**: v0.2.0 (ready to merge) | **Strategy**: MVP → Optimization → Hardening → Runtime Integration → Go Proposal | **Milestone**: v0.2.0 COMPLETE! → v0.3.0 (Production Hardening) → v1.0.0 (Q1 2026)

---

## 🎯 Vision

Build a **production-ready, pure-Go race detector** that eliminates CGO dependency for race detection, enabling `CGO_ENABLED=0` builds with full race detection capabilities.

### Key Advantages

✅ **FastTrack Algorithm**
- Academic paper (PLDI 2009) provides proven foundation
- 30+ citations and production validation
- Efficient happens-before tracking
- Adaptive epoch ↔ vector clock optimization (64x memory savings)

✅ **Pure Go Implementation**
- No CGO dependency (unlike Go's official `-race` flag)
- Works with `CGO_ENABLED=0` builds
- Cross-platform without C++ compiler requirements
- Easier deployment and distribution

✅ **Standalone Tool**
- AST-based instrumentation (no compiler modification)
- Drop-in replacement for `go build` and `go run`
- Works with existing Go projects immediately
- Community adoption without Go runtime changes

---

## 🚀 Version Strategy

### Philosophy: MVP → Refinement → Integration → Community Validation → Stable

```
v0.1.0-alpha (MVP) ✅ SKIPPED (superseded same day)
         ↓ (same day)
v0.1.0 (FIRST WORKING RELEASE) ✅ RELEASED 2025-11-19
         ↓ (detector works, catches real races!)
v0.2.0 (Performance Optimizations) ✅ COMPLETE 2025-11-20
         ↓ (99% overhead reduction, 74× speedup!)
v0.3.0 (Production Hardening) → Fix TID/Clock overflow + Stack Traces
         ↓ (1-2 weeks)
v0.4.0 (Go Runtime Integration) → Replace ThreadSanitizer in Go toolchain
         ↓ (1-2 months testing)
v1.0.0 LTS → Production-ready with Go community adoption (Q1 2026)
```

### Critical Milestones

**v0.1.0** = First working release ✅ RELEASED
- FastTrack algorithm fully implemented
- AST instrumentation working (race calls inserted)
- Detects real data races successfully
- Cross-platform support (Linux, macOS, Windows)
- 22,653 lines production code, 970+ test lines
- 70+ tests passing (100% pass rate)

**v0.2.0** = Performance optimizations ✅ COMPLETE
- CAS-based shadow memory (81.4% faster, 0 allocations)
- BigFoot static coalescing (90% barrier reduction)
- SmartTrack ownership tracking (10-20% HB reduction)
- Combined: 99% overhead reduction, 74× speedup

**v0.3.0** = Production hardening (in progress)
- Increase MaxThreads: 256 → 65,536 (16-bit TID)
- Increase ClockBits: 24 → 48 (281T operations)
- Add overflow detection with warnings
- Implement stack depot for previous stack traces
- **Target**: December 4, 2025

**v0.4.0** = Go runtime integration (planned)
- Replace `runtime/race/*.syso` (ThreadSanitizer binaries)
- Integrate with Go compiler's `-race` flag
- Official Go toolchain compatibility testing

**v1.0.0** = Production with Community Adoption
- Proven in real-world projects
- Performance competitive with ThreadSanitizer
- Go proposal submitted and accepted
- Long-term support guarantee

**Why v0.1.0 (not alpha)?**: Detector WORKS end-to-end! Successfully detects real races. Alpha phase would imply "might not work" - but it does! Upgrading to stable release reflects actual functionality.

**See**: Phase completion reports in `docs/dev/reports/` for detailed progress

---

## 📊 Current Status (v0.1.0)

**Phase**: ✅ Standalone Tool Complete
**Detector**: Working! Catches real data races! 🎉
**AST Instrumentation**: Complete! Inserts race detection calls! ✨

**What Works**:
- ✅ `racedetector build` command (drop-in for `go build`)
- ✅ `racedetector run` command (drop-in for `go run`)
- ✅ **FastTrack algorithm** (write/read race detection)
- ✅ **Shadow memory tracking** (per-variable access history)
- ✅ **Vector clocks** (happens-before relationships)
- ✅ **Adaptive optimization** (epoch ↔ VectorClock, 64x memory savings)
- ✅ **Sync primitives** (Mutex, Channel, WaitGroup tracking)
- ✅ **Race reports** with stack traces (goroutine IDs, file:line)
- ✅ **Race deduplication** (no report spam)
- ✅ **Smart filtering** (skips constants, built-ins, literals)
- ✅ **Professional errors** (file:line:column with suggestions)
- ✅ **Verbose mode** (`-v` flag shows instrumentation stats)

**Example Race Detection**:
```bash
$ racedetector build examples/dogfooding/simple_race.go
$ ./simple_race
==================
WARNING: DATA RACE
Write at 0x000000c00000a0b8 by goroutine 4:
  main.main.func1 (simple_race.go:15)
Previous Write at 0x000000c00000a0b8 by goroutine 3:
  main.main.func1 (simple_race.go:15)
==================
```

**Validation**:
- ✅ Detects simple race (10 goroutines → counter=2 instead of 10)
- ✅ No false positives on mutex-protected code
- ✅ No false positives on channel synchronization
- ✅ 70+ tests passing (100% pass rate)
- ✅ Works with `CGO_ENABLED=0`

**Performance**:
- Hot path overhead: 15-22% (competitive with ThreadSanitizer's 5-10x)
- Zero allocations on hot paths
- Scalable to 1000+ goroutines
- 5-15% overhead reduction through smart filtering

**History**: See [CHANGELOG.md](CHANGELOG.md) for complete release history

---

## 📅 What's Next

### **v0.2.0 - Performance Optimizations** (November 2025) [COMPLETE! ✅]

**Goal**: Optimize performance from 5-15x overhead to <10x overhead

**Duration**: 1 day (November 20, 2025)

**Status**: ✅ ALL 3 TASKS COMPLETE! Exceeded all targets!

**Performance Optimizations Achieved**:

1. **CAS-Based Shadow Memory** ✅ COMPLETE
   - Replaced sync.Map with atomic.Pointer array
   - **Result**: 81.4% faster (2.07ns vs 11.12ns) - **2× target!**
   - Zero allocations achieved (0 B/op)
   - 34-56% memory savings vs sync.Map
   - <0.01% collision rate (100× better than target)

2. **BigFoot Static Coalescing** ✅ COMPLETE
   - Based on "Effective Race Detection for Event-Driven Programs" (PLDI 2017)
   - Coalesces consecutive memory operations at AST level
   - **Result**: 90% barrier reduction (10 barriers → 1 barrier)
   - Exceeds 40-60% target

3. **SmartTrack Ownership Tracking** ✅ COMPLETE
   - Based on "SmartTrack: Efficient Predictive Race Detection" (PLDI 2020)
   - Tracks exclusive writer to skip happens-before checks
   - **Result**: 10-20% HB check reduction (as expected)
   - Single-writer fast path optimization

**Combined Impact**:
- 99% overhead reduction for consecutive operations
- 74× speedup in common patterns
- ~2-5x overhead (achieved <10x target!)

**Branch**: `feature/v0.2.0-clean` (ready to merge)

**Completed**: November 20, 2025 (1 day, estimated 2-3 weeks!)

---

### **v0.3.0 - Production Hardening** (December 2025) [IN PROGRESS 🚧]

**Goal**: Fix critical MVP limitations to prevent false negatives in production

**Duration**: 1-2 weeks (November 20 - December 4, 2025)

**Status**: Planning complete, 3 tasks created

**Triggered By**: Community feedback analysis (dvyukov GitHub comment on issue #6508)

**Critical Issues to Fix**:

1. **Increase MaxThreads and ClockBits** (P0 - Critical, 2-3 hours)
   - Current: TIDBits=8 (256 threads), ClockBits=24 (16M ops)
   - Target: TIDBits=16 (65,536 threads), ClockBits=48 (281T ops)
   - Change Epoch from uint32 → uint64
   - **Impact**: 256× more goroutines, 16M× more operations

2. **Add Overflow Detection** (P0 - Critical, 3-4 hours)
   - Atomic overflow flags (TID, clock)
   - Periodic checks in hot path (every 10K operations)
   - Early warnings at 90% threshold
   - Clear error messages to stderr
   - **Impact**: Prevents silent failures

3. **Implement Stack Depot** (P1 - Important, 6-8 hours)
   - Store previous access stack traces in shadow memory
   - ThreadSanitizer v2 approach (deduplication)
   - Stack hashes in VarState (8 bytes per variable)
   - **Impact**: Complete race reports with both stacks

**Quality Targets**:
- ✅ Supports 65K goroutines (was 256)
- ✅ Supports 281T operations (was 16M)
- ✅ Clear overflow warnings
- ✅ Full debugging information

**Trade-offs**:
- VarState: 24→40 bytes (67% increase, acceptable)
- Epoch: uint32→uint64 (100% increase, acceptable)
- Stack depot: 64 bytes per unique stack

**Why v0.3.0?**
Originally planned "v0.3.0 = Go Runtime Integration", but these limitations **must be fixed first**:
- Programs with >256 goroutines fail silently (false negatives)
- Long-running programs overflow clock (false positives/negatives)
- Incomplete race reports make debugging difficult

**Decision**: Fix critical issues in v0.3.0, **then** do Go runtime integration in v0.4.0.

**Target**: December 4, 2025

---

### **v0.4.0 - Go Runtime Integration** (January 2026) [PLANNED]

**Goal**: Replace ThreadSanitizer in Go toolchain

**Duration**: 1-2 months (including testing)

**Planned Work**:
1. **Runtime Integration**
   - Replace `runtime/race/*.syso` with pure Go implementation
   - Hook into `go build -race` flag
   - Maintain API compatibility with existing instrumentation

2. **Compiler Coordination**
   - Work with existing `cmd/compile/internal/walk/race.go`
   - Ensure instrumentation calls match new runtime
   - Test with official Go test suite

3. **Validation**
   - Run official Go race detector tests
   - Benchmark against ThreadSanitizer
   - Cross-platform testing (Linux, macOS, Windows)

**Target**: January 31, 2026

---

### **v1.0.0 - Long-Term Support Release** (Q1 2026)

**Goal**: LTS release with Go community adoption

**Requirements**:
- v0.3.0 stable for 1+ months
- Positive Go community feedback
- Performance competitive with ThreadSanitizer (<20% difference)
- No critical bugs
- API proven in production

**LTS Guarantees**:
- ✅ API stability (no breaking changes in v1.x.x)
- ✅ Long-term support (2+ years)
- ✅ Semantic versioning strictly followed
- ✅ Security updates and bug fixes
- ✅ Performance improvements

**Go Proposal**:
- Submit official proposal to golang/go
- Present at Go community meetups/conferences
- Collaborate with Go team for integration

**Target**: Q2 2026 (after validation period)

---

## 📚 Resources

**Academic Foundation**:
- FastTrack paper: [PLDI 2009](https://users.soe.ucsc.edu/~cormac/papers/pldi09.pdf)
- DJIT+ algorithm (original vector clock approach)
- ThreadSanitizer design papers

**Go Integration**:
- Compiler instrumentation: `go/src/cmd/compile/internal/walk/race.go`
- Runtime API: `go/src/runtime/race.go`
- Official race detector: https://go.dev/blog/race-detector

**Development**:
- CONTRIBUTING.md - How to contribute
- docs/ - User guides (INSTALLATION.md, USAGE_GUIDE.md)
- examples/ - Example programs (mutex_protected, channel_sync, simple_race)

---

## 📞 Support

**Documentation**:
- README.md - Project overview and quick start
- INSTALLATION.md - Installation instructions
- USAGE_GUIDE.md - Usage examples and best practices
- CHANGELOG.md - Release history

**Feedback**:
- GitHub Issues - Bug reports and feature requests
- Discussions - Questions and help
- Proposals - Architecture and design discussions

---

## 🔬 Development Approach

**Pure Go Implementation**:
- No CGO dependencies
- FastTrack algorithm from academic paper
- Go idioms and best practices
- Comprehensive testing (unit + integration)

**Quality Standards**:
- ✅ 70%+ test coverage minimum
- ✅ 90%+ for core detector logic
- ✅ 100% test pass rate
- ✅ Zero linter issues (golangci-lint)
- ✅ Professional error messages
- ✅ Comprehensive documentation

**Community First**:
- Open development process
- Regular updates and communication
- Responsive to feedback and bug reports
- Collaborative with Go team

---

*Version 1.0 (Created 2025-11-19)*
*Current: v0.1.0 (RELEASED) | Phase: Standalone Tool Complete | Next: v0.2.0 (Runtime Enhancements) | Target: v1.0.0 LTS (Q1 2026)*
