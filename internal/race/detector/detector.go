package detector

import (
	"fmt"
	"os"
	"sync"

	"github.com/kolkov/racedetector/internal/race/epoch"
	"github.com/kolkov/racedetector/internal/race/goroutine"
	"github.com/kolkov/racedetector/internal/race/shadowmem"
	"github.com/kolkov/racedetector/internal/race/syncshadow"
)

// PromotionStats tracks adaptive representation statistics (Phase 3).
//
// These metrics help analyze the effectiveness of the adaptive VarState optimization.
// In production, we expect:
//   - 90%+ fast path reads (unpromoted)
//   - <1% promotions (rare concurrent reads)
//   - High promotion success rate
type PromotionStats struct {
	TotalReads    uint64 // Total read operations.
	TotalWrites   uint64 // Total write operations.
	Promotions    uint64 // Epoch → VectorClock promotions.
	Demotions     uint64 // VectorClock → Epoch demotions (on write).
	FastPathReads uint64 // Reads using Epoch (fast).
	SlowPathReads uint64 // Reads using VectorClock (slow).
	PromotedVars  uint64 // Current number of promoted variables.
}

// Detector implements the core FastTrack race detection algorithm.
//
// It maintains global state including shadow memory (tracking access history
// for all memory locations) and goroutine contexts (tracking logical time
// for each thread).
//
// Phase 3 adds adaptive VarState representation with promotion tracking.
// Phase 4 adds synchronization primitive tracking (mutex, rwmutex, channels).
// Phase 5 adds race deduplication to prevent duplicate reports.
type Detector struct {
	// shadowMemory stores VarState cells for all instrumented addresses.
	// This is the core data structure that tracks the last write and read
	// epochs for every memory location.
	shadowMemory *shadowmem.ShadowMemory

	// syncShadow stores SyncVar cells for all synchronization primitives.
	// This tracks release clocks for mutexes, rwmutexes, channels, etc.
	// Added in Phase 4 Task 4.1.
	syncShadow *syncshadow.SyncShadow

	// racesDetected counts the total number of races found.
	// This is used for testing and reporting purposes.
	racesDetected int

	// reportedRaces tracks which races have already been reported.
	// Key format: "{type}:{addr}:{gid1}:{gid2}" (sorted goroutine IDs).
	// This prevents duplicate reports for the same race location.
	// Added in Phase 5 Task 5.3.
	reportedRaces sync.Map

	// stats tracks adaptive representation statistics (Phase 3).
	stats PromotionStats

	// mu protects racesDetected counter and stats updates.
	mu sync.Mutex
}

// NewDetector creates and initializes a new race detector instance.
//
// The detector is ready to use immediately after creation.
// It initializes:
//   - Shadow memory for tracking variable access history
//   - Sync shadow memory for tracking synchronization primitives (Phase 4)
//
// Example:
//
//	d := NewDetector()
//	ctx := goroutine.Alloc(1)
//	d.OnWrite(0x1234, ctx)  // Detect write to address
//	d.OnAcquire(0x5678, ctx)  // Track mutex lock
func NewDetector() *Detector {
	return &Detector{
		shadowMemory: shadowmem.NewShadowMemory(),
		syncShadow:   syncshadow.NewSyncShadow(),
	}
}

// OnWrite handles write access to memory at the given address.
//
// This is the CRITICAL HOT PATH function - it is called on EVERY write access
// in instrumented code. Performance is paramount!
//
// Algorithm: FastTrack [FT WRITE] rules (Phase 3 - Adaptive)
//
//  1. Get current goroutine context
//  2. Get or create shadow cell for address
//  3. Get current epoch from context
//  4. [FT WRITE SAME EPOCH] Fast path: If vs.W == currentEpoch, return (71% of writes)
//  5. Check write-write race: If !vs.W.HappensBefore(ctx.C), report race
//  6. Check read-write race (ADAPTIVE):
//     a. If promoted: Check if readClock happened-before ctx.C
//     b. If not promoted: Check if readEpoch happened-before ctx.C
//  7. Update shadow memory: vs.W = currentEpoch
//  8. Clear read tracking and DEMOTE (write dominates all previous reads)
//  9. Increment logical clock: ctx.IncrementClock()
//
// Phase 3 Adaptive Optimization: Write clears read state and demotes back to fast path.
// This means variables with alternating read/write patterns stay in fast path.
//
// Parameters:
//   - addr: Memory address being written to
//
// Thread Safety: Safe for concurrent calls from multiple goroutines.
//
// Performance Target: <100ns per call (MVP), <50ns ideal.
//
// Zero Allocations: This function MUST NOT allocate on the heap.
// All required objects (VarState, RaceContext) are pre-allocated or
// retrieved from pools.
//
//go:nosplit
func (d *Detector) OnWrite(addr uintptr, ctx *goroutine.RaceContext) {
	// Step 1: Get or create shadow cell for this address.
	// GetOrCreate is thread-safe and may allocate on first access.
	vs := d.shadowMemory.GetOrCreate(addr)

	// Step 2: Get current epoch (TID, Clock) for this goroutine.
	currentEpoch := ctx.GetEpoch()

	// Step 3: [FT WRITE SAME EPOCH] Fast path optimization.
	// If we're writing to the same location in the same epoch, no race possible.
	// This handles 71% of writes according to FastTrack paper.
	if vs.W.Same(currentEpoch) {
		return
	}

	// Step 4: Check write-write race.
	// A race occurs if the previous write did NOT happen-before the current write.
	if !d.happensBeforeWrite(vs.W, ctx) {
		d.reportRaceV2("write-write", addr, vs.W, currentEpoch)
		return // Stop on first race to avoid cascade of reports
	}

	// Step 5: Check read-write race (ADAPTIVE).
	if !vs.IsPromoted() {
		// FAST PATH: Check single reader epoch.
		readEpoch := vs.GetReadEpoch()
		if readEpoch != 0 && !d.happensBeforeRead(readEpoch, ctx) {
			d.reportRaceV2("read-write", addr, readEpoch, currentEpoch)
			return // Stop on first race
		}
	} else {
		// SLOW PATH: Check full read VectorClock.
		readClock := vs.GetReadClock()
		if readClock != nil && !readClock.HappensBefore(ctx.C) {
			// Report race with first conflicting read (use epoch representation for reporting).
			// For simplicity, we report a synthetic epoch from the VectorClock.
			// TODO: Improve race reporting to show all conflicting reads in future version.
			d.reportRaceV2("read-write", addr, epoch.Epoch(0), currentEpoch)
			return // Stop on first race
		}
	}

	// Step 6: Update shadow memory write epoch.
	// Record that this write occurred at currentEpoch.
	vs.W = currentEpoch

	// Step 7: Clear read tracking and DEMOTE back to fast path.
	// Write dominates all previous reads, so we reset read state.
	// This is a key optimization: variables with alternating read/write stay in fast path.
	wasPromoted := vs.IsPromoted()
	vs.Demote()
	if wasPromoted {
		// Track demotion statistics.
		d.mu.Lock()
		d.stats.Demotions++
		d.stats.PromotedVars--
		d.mu.Unlock()
	}

	// Track write statistics.
	d.mu.Lock()
	d.stats.TotalWrites++
	d.mu.Unlock()

	// Step 8: Increment logical clock to advance time.
	// This must be done AFTER updating shadow memory to maintain
	// the happens-before invariant.
	ctx.IncrementClock()
}

// OnRead handles read access to memory at the given address.
//
// This is the CRITICAL HOT PATH function - it is called on EVERY read access
// in instrumented code. Reads are typically MORE frequent than writes, making
// this even more performance-critical than OnWrite.
//
// Algorithm: FastTrack [FT READ] rules (Phase 3 - Adaptive)
//
//  1. Get current goroutine context
//  2. Get or create shadow cell for address
//  3. Get current epoch from context
//  4. Check read-write race: If vs.W != 0 && !vs.W.HappensBefore(ctx.C), report race
//  5. Update read tracking (ADAPTIVE):
//     a. If promoted (vs.IsPromoted()):
//     - Merge current VC into read VC
//     b. If not promoted (fast path):
//     - If same epoch: return (63% of reads - FAST!)
//     - If same TID: update epoch, return
//     - If happens-before: replace epoch, return
//     - Otherwise: PROMOTE to VectorClock
//  6. Increment logical clock
//
// Phase 3 Adaptive Optimization: Most reads (90%+) use epoch-only fast path.
// Only concurrent reads from different threads trigger promotion to VectorClock.
//
// Parameters:
//   - addr: Memory address being read from
//
// Thread Safety: Safe for concurrent calls from multiple goroutines.
//
// Performance Target:
//   - Fast path (unpromoted): <50ns (handles 90%+ of reads)
//   - Slow path (promoted): <300ns
//   - Promotion overhead: <100ns (one-time cost)
//
// Zero Allocations: Fast path allocates nothing. Slow path may allocate VectorClock on promotion.
//
//go:nosplit
func (d *Detector) OnRead(addr uintptr, ctx *goroutine.RaceContext) {
	// Step 1: Get or create shadow cell for this address.
	// GetOrCreate is thread-safe and may allocate on first access.
	vs := d.shadowMemory.GetOrCreate(addr)

	// Step 2: Get current epoch (TID, Clock) for this goroutine.
	currentEpoch := ctx.GetEpoch()

	// Step 3: Check read-write race.
	// A race occurs if there was a write that did NOT happen-before this read.
	// vs.W == 0 means no previous write, so skip check.
	if vs.W != 0 && !d.happensBeforeWrite(vs.W, ctx) {
		d.reportRaceV2("write-read", addr, vs.W, currentEpoch)
		return // Stop on first race to avoid cascade of reports
	}

	// Step 4: Update read tracking (ADAPTIVE).
	//nolint:nestif // FastTrack adaptive algorithm requires nested conditions for performance
	if !vs.IsPromoted() {
		// FAST PATH: Single reader (common case, 90%+ of reads).
		d.mu.Lock()
		d.stats.TotalReads++
		d.stats.FastPathReads++
		d.mu.Unlock()

		// [FT READ SAME EPOCH] Fast path optimization.
		// If we're reading from the same location in the same epoch, no race possible.
		// This handles 63% of reads according to FastTrack paper.
		if vs.GetReadEpoch().Same(currentEpoch) {
			return
		}

		// Check if new reader is same thread as existing reader.
		existingReadEpoch := vs.GetReadEpoch()
		if existingReadEpoch != 0 {
			existingTID, _ := existingReadEpoch.Decode()
			currentTID, _ := currentEpoch.Decode()

			if existingTID == currentTID {
				// Same reader thread - just update clock.
				vs.SetReadEpoch(currentEpoch)
				ctx.IncrementClock()
				return
			}

			// Different reader detected - check if sequential (happens-before).
			if existingReadEpoch.HappensBefore(ctx.C) {
				// Sequential reads (happens-before) - replace epoch.
				vs.SetReadEpoch(currentEpoch)
				ctx.IncrementClock()
				return
			}

			// CONCURRENT READS DETECTED - PROMOTE!
			vs.PromoteToReadClock(ctx.C)
			d.mu.Lock()
			d.stats.Promotions++
			d.stats.PromotedVars++
			d.mu.Unlock()
			ctx.IncrementClock()
			return
		}

		// No previous read - just set epoch.
		vs.SetReadEpoch(currentEpoch)
		ctx.IncrementClock()
		return
	}

	// SLOW PATH: Multiple readers (already promoted, 0.1% of reads).
	d.mu.Lock()
	d.stats.TotalReads++
	d.stats.SlowPathReads++
	d.mu.Unlock()

	vs.GetReadClock().Join(ctx.C)
	ctx.IncrementClock()
}

// happensBeforeWrite checks if a write epoch happened-before the current context.
//
// MVP Implementation: Simplified happens-before check for epoch-only mode.
//
// For a write epoch to happen-before the current write, the previous write's
// clock must be <= the current context's clock for that thread.
//
// Full FastTrack Rule:
//   - If prevWrite.TID == currentTID: prevWrite.Clock <= currentClock
//   - Otherwise: prevWrite.HappensBefore(currentContext.C)
//
// For MVP (single thread), we use simplified logic:
//   - If same TID: Compare clocks directly
//   - If different TID: Use VectorClock.HappensBefore
//
// Parameters:
//   - prevWrite: The previous write epoch from shadow memory
//   - ctx: The current goroutine's RaceContext
//
// Returns:
//   - true if prevWrite happened-before current operation
//   - false if there's a potential race (concurrent access)
//
//go:nosplit
func (d *Detector) happensBeforeWrite(prevWrite epoch.Epoch, ctx *goroutine.RaceContext) bool {
	// Use the epoch's HappensBefore method which checks against vector clock.
	// This handles both same-thread and cross-thread cases correctly.
	return prevWrite.HappensBefore(ctx.C)
}

// happensBeforeRead checks if a read epoch happened-before the current context.
//
// This is identical to happensBeforeWrite for MVP (both use epoch-only tracking).
// In Phase 3, this will become more complex when read epochs can be vector clocks
// for read-shared variables.
//
// Parameters:
//   - prevRead: The previous read epoch from shadow memory
//   - ctx: The current goroutine's RaceContext
//
// Returns:
//   - true if prevRead happened-before current operation
//   - false if there's a potential race
//
//go:nosplit
func (d *Detector) happensBeforeRead(prevRead epoch.Epoch, ctx *goroutine.RaceContext) bool {
	// MVP: Same logic as write happens-before.
	// Phase 3: This will need to handle vector clock reads.
	return prevRead.HappensBefore(ctx.C)
}

// reportRace reports a detected data race to stderr.
//
// Deprecated: Use reportRaceV2() instead. This function is kept for backward
// compatibility with tests but will be removed in Phase 5 Task 5.2.
//
// This is the MVP implementation that prints race information to stderr.
// In Phase 7 (Production Reporting), this will be replaced with:
//   - Stack trace capture for both accesses
//   - Detailed source location information
//   - Race deduplication
//   - Structured output formats (JSON, XML)
//   - Configurable reporting behavior
//
// Parameters:
//   - raceType: Type of race ("write-write" or "read-write")
//   - addr: Memory address where race occurred
//   - prevEpoch: Epoch of the conflicting previous access
//   - currEpoch: Epoch of the current access
//
// Thread Safety: Uses mutex to prevent interleaved output.
//
// Example Output:
//
//	==================
//	WARNING: DATA RACE
//	Type: write-write
//	Address: 0x12345678
//	Previous access: 10@1 (clock=10, tid=1)
//	Current access:  20@1 (clock=20, tid=1)
//	==================
func (d *Detector) reportRace(raceType string, addr uintptr, prevEpoch, currEpoch epoch.Epoch) {
	// Lock to prevent interleaved output from multiple goroutines.
	d.mu.Lock()
	defer d.mu.Unlock()

	// Increment race counter for statistics.
	d.racesDetected++

	// Print race report to stderr.
	// Using fmt.Fprintf for formatted output (not on hot path).
	fmt.Fprintf(os.Stderr, "==================\n")
	fmt.Fprintf(os.Stderr, "WARNING: DATA RACE\n")
	fmt.Fprintf(os.Stderr, "Type: %s\n", raceType)
	fmt.Fprintf(os.Stderr, "Address: 0x%x\n", addr)
	fmt.Fprintf(os.Stderr, "Previous access: %s\n", prevEpoch.String())
	fmt.Fprintf(os.Stderr, "Current access:  %s\n", currEpoch.String())
	fmt.Fprintf(os.Stderr, "==================\n")
}

// RacesDetected returns the total number of races detected.
//
// This is used for testing and reporting purposes. It provides a simple
// count of how many races were found during execution.
//
// Thread Safety: Safe for concurrent calls (protected by mutex).
//
// Returns:
//   - int: Total number of races detected
func (d *Detector) RacesDetected() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.racesDetected
}

// OnAcquire handles mutex lock operations (Phase 4 Task 4.1).
//
// This establishes a happens-before edge from the previous Unlock to this Lock.
// The acquiring thread merges the mutex's release clock into its own clock.
//
// Algorithm: FastTrack [FT ACQUIRE]
//  1. Get lock's SyncVar from sync shadow memory
//  2. If lock has release clock: ctx.C.Join(syncVar.releaseClock)
//  3. ctx.IncrementClock()
//
// This implements: Ct := Ct ⊔ Lm (thread clock joins lock clock).
//
// Parameters:
//   - addr: Address of the mutex being locked
//   - ctx: Current goroutine's RaceContext
//
// Thread Safety: Safe for concurrent calls from multiple goroutines.
//
// Performance Target: <500ns per call (VectorClock join overhead acceptable).
//
// Example:
//
//	mu.Lock()  // Compiler inserts: raceacquire(&mu)
//	// OnAcquire merges previous Unlock's clock into current thread
//	x = 42     // Now happens-after previous critical section
//
//go:nosplit
func (d *Detector) OnAcquire(addr uintptr, ctx *goroutine.RaceContext) {
	// Step 1: Get or create SyncVar for this mutex address.
	syncVar := d.syncShadow.GetOrCreate(addr)

	// Step 2: If lock has a release clock, join it with current thread's clock.
	// This establishes happens-before from the previous Unlock.
	releaseClock := syncVar.GetReleaseClock()
	if releaseClock != nil {
		// Ct := Ct ⊔ Lm (thread clock joins lock clock).
		ctx.C.Join(releaseClock)
	}

	// Step 3: Increment logical clock to advance time.
	// This must be done AFTER joining to maintain happens-before invariant.
	ctx.IncrementClock()
}

// OnRelease handles mutex unlock operations (Phase 4 Task 4.1).
//
// This creates a happens-before edge that future Lock operations will synchronize with.
// The releasing thread captures its current clock into the mutex's release clock.
//
// Algorithm: FastTrack [FT RELEASE]
//  1. Get lock's SyncVar
//  2. Set syncVar.releaseClock = ctx.C (copy current thread's clock)
//  3. ctx.IncrementClock()
//
// This implements: Lm := Ct (lock clock = thread clock).
//
// Parameters:
//   - addr: Address of the mutex being unlocked
//   - ctx: Current goroutine's RaceContext
//
// Thread Safety: Safe for concurrent calls from multiple goroutines.
//
// Performance Target: <300ns per call (VectorClock copy overhead acceptable).
//
// Example:
//
//	x = 42       // Write happens-before Unlock
//	mu.Unlock()  // Compiler inserts: racerelease(&mu)
//	// OnRelease captures current clock for next Lock to see
//
//go:nosplit
func (d *Detector) OnRelease(addr uintptr, ctx *goroutine.RaceContext) {
	// Step 1: Get or create SyncVar for this mutex address.
	syncVar := d.syncShadow.GetOrCreate(addr)

	// Step 2: Set lock's release clock to current thread's clock.
	// This captures the happens-before relationship for future Acquires.
	// Lm := Ct (lock clock = thread clock).
	syncVar.SetReleaseClock(ctx.C)

	// Step 3: Increment logical clock to advance time.
	// This must be done AFTER updating release clock to maintain happens-before.
	ctx.IncrementClock()
}

// OnReleaseMerge handles RWMutex write unlock operations (Phase 4 Task 4.1).
//
// This is used for RWMutex.Unlock (write unlock) where multiple readers may have
// overlapping critical sections. We merge the current thread's clock into the
// lock's release clock to capture the union of all happens-before relationships.
//
// Algorithm: FastTrack [FT RELEASE MERGE]
//  1. Get lock's SyncVar
//  2. syncVar.releaseClock = syncVar.releaseClock ⊔ ctx.C (merge clocks)
//  3. ctx.IncrementClock()
//
// This implements: Lm := Lm ⊔ Ct (lock clock merges with thread clock).
//
// Parameters:
//   - addr: Address of the RWMutex being unlocked
//   - ctx: Current goroutine's RaceContext
//
// Thread Safety: Safe for concurrent calls from multiple goroutines.
//
// Performance Target: <500ns per call (VectorClock merge overhead acceptable).
//
// Example (RWMutex scenario):
//
//	// Reader 1
//	mu.RLock()   // Acquire
//	y = x        // Read
//	mu.RUnlock() // ReleaseMerge (merges Reader 1's clock)
//
//	// Reader 2
//	mu.RLock()   // Acquire
//	z = x        // Read
//	mu.RUnlock() // ReleaseMerge (merges Reader 2's clock)
//
//	// Writer
//	mu.Lock()    // Acquire (sees union of Reader 1 and Reader 2 clocks)
//	x = 42       // Write happens-after both readers
//
//go:nosplit
func (d *Detector) OnReleaseMerge(addr uintptr, ctx *goroutine.RaceContext) {
	// Step 1: Get or create SyncVar for this mutex address.
	syncVar := d.syncShadow.GetOrCreate(addr)

	// Step 2: Merge current thread's clock into lock's release clock.
	// This captures the union of happens-before relationships.
	// Lm := Lm ⊔ Ct (lock clock merges with thread clock).
	syncVar.MergeReleaseClock(ctx.C)

	// Step 3: Increment logical clock to advance time.
	ctx.IncrementClock()
}

// === Channel Synchronization Methods (Phase 4 Task 4.2) ===

// OnChannelSendBefore is called BEFORE a channel send operation.
//
// For MVP, this is a no-op placeholder. In future phases, this could be used
// for detecting invalid operations (e.g., send on closed channel).
//
// Parameters:
//   - ch: Address of the channel being sent to
//   - ctx: Current goroutine's RaceContext
//
// Performance Target: <100ns (minimal overhead).
//
//go:nosplit
func (d *Detector) OnChannelSendBefore(ch uintptr, ctx *goroutine.RaceContext) {
	// MVP: No-op. Future: could check if channel is closed.
	_ = ch
	_ = ctx
}

// OnChannelSendAfter is called AFTER a channel send operation completes.
//
// This establishes a happens-before edge from the sender to future receivers.
// The sender's clock is captured into the channel's sendClock.
//
// Algorithm: FastTrack [FT CHANNEL SEND]
//  1. Get channel's SyncVar from sync shadow memory
//  2. Capture sender's clock: ch.sendClock := ctx.C (copy)
//  3. ctx.IncrementClock()
//
// This implements the happens-before relationship:
//   - Send happens-before Receive (for unbuffered and buffered channels)
//
// Parameters:
//   - ch: Address of the channel being sent to
//   - ctx: Current goroutine's RaceContext
//
// Thread Safety: Safe for concurrent calls from multiple goroutines.
//
// Performance Target: <500ns (VectorClock copy overhead acceptable).
//
// Example:
//
//	ch <- value  // Compiler inserts: racechansendbefore(&ch); ...; racechansendafter(&ch)
//	// OnChannelSendAfter captures sender's clock for receiver to see
//
//go:nosplit
func (d *Detector) OnChannelSendAfter(ch uintptr, ctx *goroutine.RaceContext) {
	// Step 1: Get or create SyncVar for this channel address.
	syncVar := d.syncShadow.GetOrCreate(ch)

	// Step 2: Capture sender's clock into channel's sendClock.
	// This makes the sender's logical time visible to future receivers.
	syncVar.SetChannelSendClock(ctx.C)

	// Step 3: Increment logical clock to advance time.
	// This must be done AFTER capturing the clock to maintain happens-before.
	ctx.IncrementClock()
}

// OnChannelRecvBefore is called BEFORE a channel receive operation.
//
// For MVP, this is a no-op placeholder. In future phases, this could be used
// for detecting invalid operations or optimizations.
//
// Parameters:
//   - ch: Address of the channel being received from
//   - ctx: Current goroutine's RaceContext
//
// Performance Target: <100ns (minimal overhead).
//
//go:nosplit
func (d *Detector) OnChannelRecvBefore(ch uintptr, ctx *goroutine.RaceContext) {
	// MVP: No-op.
	_ = ch
	_ = ctx
}

// OnChannelRecvAfter is called AFTER a channel receive operation completes.
//
// This establishes a happens-before edge from the sender to the receiver.
// The receiver merges the sender's clock to observe all the sender's work.
//
// Algorithm: FastTrack [FT CHANNEL RECV]
//  1. Get channel's SyncVar from sync shadow memory
//  2. If channel has sendClock: ctx.C.Join(ch.sendClock)
//  3. If channel is closed: ctx.C.Join(ch.closeClock)
//  4. ctx.IncrementClock()
//
// This implements the happens-before relationship:
//   - Sender's work happens-before Receiver's subsequent work
//
// Parameters:
//   - ch: Address of the channel being received from
//   - ctx: Current goroutine's RaceContext
//
// Thread Safety: Safe for concurrent calls from multiple goroutines.
//
// Performance Target: <500ns (VectorClock join overhead acceptable).
//
// Example:
//
//	value := <-ch  // Compiler inserts: racechanrecvbefore(&ch); ...; racechanrecvafter(&ch)
//	// OnChannelRecvAfter merges sender's clock into receiver
//	// Receiver now happens-after sender
//
//go:nosplit
func (d *Detector) OnChannelRecvAfter(ch uintptr, ctx *goroutine.RaceContext) {
	// Step 1: Get or create SyncVar for this channel address.
	syncVar := d.syncShadow.GetOrCreate(ch)

	// Step 2: If channel has a send clock, join it with receiver's clock.
	// This establishes happens-before from the sender.
	sendClock := syncVar.GetChannelSendClock()
	if sendClock != nil {
		// Ct := Ct ⊔ Csend (receiver clock joins sender clock).
		ctx.C.Join(sendClock)
	}

	// Step 3: If channel is closed, join with close clock.
	// close(ch) happens-before all receives that observe closure.
	if syncVar.IsChannelClosed() {
		closeClock := syncVar.GetChannelCloseClock()
		if closeClock != nil {
			ctx.C.Join(closeClock)
		}
	}

	// Step 4: Optionally capture receiver's clock (for future bidirectional sync).
	// MVP: Store recvClock but don't use it yet.
	syncVar.SetChannelRecvClock(ctx.C)

	// Step 5: Increment logical clock to advance time.
	// This must be done AFTER joining to maintain happens-before invariant.
	ctx.IncrementClock()
}

// OnChannelClose is called when a channel is closed via close(ch).
//
// This establishes a happens-before edge from the closer to all future receives.
// The closer's clock is captured into the channel's closeClock.
//
// Algorithm: FastTrack [FT CHANNEL CLOSE]
//  1. Get channel's SyncVar from sync shadow memory
//  2. Capture closer's clock: ch.closeClock := ctx.C (copy)
//  3. Set ch.isClosed = true
//  4. ctx.IncrementClock()
//
// This implements the happens-before relationship:
//   - close(ch) happens-before all receives that observe closure
//
// Parameters:
//   - ch: Address of the channel being closed
//   - ctx: Current goroutine's RaceContext
//
// Thread Safety: Safe for concurrent calls from multiple goroutines.
//
// Performance Target: <300ns (VectorClock copy overhead acceptable).
//
// Example:
//
//	close(ch)  // Compiler inserts: racechanclose(&ch)
//	// OnChannelClose captures closer's clock
//	// Future receives will merge this clock
//
//go:nosplit
func (d *Detector) OnChannelClose(ch uintptr, ctx *goroutine.RaceContext) {
	// Step 1: Get or create SyncVar for this channel address.
	syncVar := d.syncShadow.GetOrCreate(ch)

	// Step 2: Capture closer's clock into channel's closeClock.
	// This makes the closer's logical time visible to future receivers.
	syncVar.SetChannelCloseClock(ctx.C)

	// Step 3: Increment logical clock to advance time.
	// This must be done AFTER capturing the clock to maintain happens-before.
	ctx.IncrementClock()
}

// === WaitGroup Synchronization Methods (Phase 4 Task 4.3) ===

// OnWaitGroupAdd handles WaitGroup.Add(delta) operations (Phase 4 Task 4.3).
//
// WaitGroup.Add(delta) increments the wait counter. This is typically called
// before spawning goroutines to establish the expected number of Done() calls.
//
// For happens-before tracking, we only track the counter for optional validation.
// The actual happens-before relationship is established by Done() → Wait().
//
// Algorithm:
//  1. Get or create SyncVar for this WaitGroup address
//  2. Increment the counter by delta
//  3. Increment logical clock (WaitGroup operations are synchronization points)
//
// Parameters:
//   - wg: Address of the sync.WaitGroup
//   - delta: The delta to add (positive for Add, negative would be unusual but supported)
//   - ctx: Current goroutine's RaceContext
//
// Thread Safety: Safe for concurrent calls from multiple goroutines.
//
// Performance Target: <200ns (minimal overhead, just counter increment).
//
// Example:
//
//	var wg sync.WaitGroup
//	wg.Add(1)  // Compiler inserts: racewaitgroupadd(&wg, 1)
//	// OnWaitGroupAdd increments counter to 1
//
//go:nosplit
func (d *Detector) OnWaitGroupAdd(wg uintptr, delta int, ctx *goroutine.RaceContext) {
	// Step 1: Get or create SyncVar for this WaitGroup address.
	syncVar := d.syncShadow.GetOrCreate(wg)

	// Step 2: Increment the WaitGroup counter by delta.
	// This is optional for validation but helps detect misuse patterns.
	syncVar.WaitGroupAdd(delta)

	// Step 3: Increment logical clock to advance time.
	// WaitGroup operations are synchronization points.
	ctx.IncrementClock()
}

// OnWaitGroupDone handles WaitGroup.Done() operations (Phase 4 Task 4.3).
//
// WaitGroup.Done() is equivalent to Add(-1). It signals that a goroutine has
// completed its work. This creates a happens-before edge to the corresponding
// Wait() return.
//
// Algorithm:
//  1. Get or create SyncVar for this WaitGroup address
//  2. Merge current thread's clock into the WaitGroup's doneClock
//  3. Decrement the counter
//  4. Increment logical clock
//
// The key insight: All Done() calls merge their clocks into a single doneClock.
// When Wait() returns, the waiter merges this doneClock, seeing all prior work.
//
// Parameters:
//   - wg: Address of the sync.WaitGroup
//   - ctx: Current goroutine's RaceContext
//
// Thread Safety: Safe for concurrent calls from multiple goroutines.
//
// Performance Target: <500ns (VectorClock merge overhead acceptable).
//
// Example:
//
//	// Child goroutine
//	data = 42          // Write
//	wg.Done()          // Compiler inserts: racewaitgroupdone(&wg)
//	// OnWaitGroupDone merges child's clock into doneClock
//
//go:nosplit
func (d *Detector) OnWaitGroupDone(wg uintptr, ctx *goroutine.RaceContext) {
	// Step 1: Get or create SyncVar for this WaitGroup address.
	syncVar := d.syncShadow.GetOrCreate(wg)

	// Step 2: Merge current thread's clock into doneClock.
	// This accumulates the happens-before relationship from this goroutine.
	syncVar.MergeWaitGroupDoneClock(ctx.C)

	// Step 3: Decrement the counter (Done is Add(-1)).
	syncVar.WaitGroupAdd(-1)

	// Step 4: Increment logical clock to advance time.
	ctx.IncrementClock()
}

// OnWaitGroupWaitBefore handles WaitGroup.Wait() BEFORE it blocks (Phase 4 Task 4.3).
//
// This is called before Wait() blocks waiting for all Done() calls.
// For MVP, this is primarily a placeholder for future optimizations or validation.
//
// We could use this to:
//   - Validate that counter > 0 (wait with counter 0 is a no-op)
//   - Track wait start time for performance monitoring
//   - Prepare for happens-before merge
//
// For now, we just increment the clock to mark this synchronization point.
//
// Parameters:
//   - wg: Address of the sync.WaitGroup
//   - ctx: Current goroutine's RaceContext
//
// Thread Safety: Safe for concurrent calls from multiple goroutines.
//
// Performance Target: <100ns (minimal overhead).
//
// Example:
//
//	wg.Wait()  // Compiler inserts: racewaitgroupwaitbefore(&wg); ...; racewaitgroupwaitafter(&wg)
//
//go:nosplit
func (d *Detector) OnWaitGroupWaitBefore(_ uintptr, ctx *goroutine.RaceContext) {
	// For MVP, just increment the clock to mark the synchronization point.
	// Future phases could add validation or monitoring here.
	// Note: wg parameter unused in MVP, but retained for API consistency and future use
	ctx.IncrementClock()
}

// OnWaitGroupWaitAfter handles WaitGroup.Wait() AFTER it returns (Phase 4 Task 4.3).
//
// This is called after Wait() returns, meaning all Done() calls have completed.
// This is the critical happens-before establishment: the waiter merges all
// accumulated Done() clocks into its own clock.
//
// Algorithm:
//  1. Get SyncVar for this WaitGroup address
//  2. Get the accumulated doneClock from all Done() calls
//  3. Merge doneClock into waiter's clock (happens-before)
//  4. Increment logical clock
//
// After this merge, the waiter's clock reflects all work done by goroutines
// that called Done(), establishing happens-before from all children to parent.
//
// Parameters:
//   - wg: Address of the sync.WaitGroup
//   - ctx: Current goroutine's RaceContext
//
// Thread Safety: Safe for concurrent calls from multiple goroutines.
//
// Performance Target: <500ns (VectorClock merge overhead acceptable).
//
// Example:
//
//	wg.Wait()          // Blocks until all Done() calls
//	// OnWaitGroupWaitAfter merges doneClock into parent's clock
//	_ = data           // Parent can now safely read child's writes (no race)
//
//go:nosplit
func (d *Detector) OnWaitGroupWaitAfter(wg uintptr, ctx *goroutine.RaceContext) {
	// Step 1: Get or create SyncVar for this WaitGroup address.
	syncVar := d.syncShadow.GetOrCreate(wg)

	// Step 2: Get the accumulated doneClock from all Done() calls.
	doneClock := syncVar.GetWaitGroupDoneClock()

	// Step 3: Merge doneClock into waiter's clock (happens-before).
	// If doneClock is nil, no Done() calls have occurred yet (unusual but valid).
	if doneClock != nil {
		ctx.C.Join(doneClock)
	}

	// Step 4: Increment logical clock to advance time.
	// This must be done AFTER merging the doneClock to maintain happens-before.
	ctx.IncrementClock()
}

// Reset resets the detector state for testing.
//
// This clears:
//   - All shadow memory cells
//   - All sync shadow memory cells (Phase 4)
//   - Race counter
//   - Reported races deduplication map (Phase 5)
//   - Promotion statistics
//
// Thread Safety: NOT safe for concurrent access.
// The caller must ensure no other goroutines are using the detector.
//
// This is primarily used in test setup/teardown.
func (d *Detector) Reset() {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Clear shadow memory.
	d.shadowMemory.Reset()

	// Clear sync shadow memory (Phase 4).
	d.syncShadow.Reset()

	// Reset race counter.
	d.racesDetected = 0

	// Clear reported races map (Phase 5 Task 5.3).
	// Range over all keys and delete them.
	d.reportedRaces.Range(func(key, _ interface{}) bool {
		d.reportedRaces.Delete(key)
		return true // Continue iteration
	})

	// Reset promotion statistics.
	d.stats = PromotionStats{}
}

// GetPromotionStats returns a copy of the current promotion statistics.
//
// This provides insight into the adaptive VarState optimization effectiveness:
//   - Fast path percentage: FastPathReads / TotalReads (expect >90%)
//   - Promotion rate: Promotions / TotalReads (expect <1%)
//   - Promoted variables: PromotedVars (should be small)
//
// Thread Safety: Safe for concurrent calls (protected by mutex).
//
// Returns:
//   - PromotionStats: Copy of current statistics
//
// Example usage:
//
//	stats := detector.GetPromotionStats()
//	fastPathRate := float64(stats.FastPathReads) / float64(stats.TotalReads) * 100
//	fmt.Printf("Fast path rate: %.2f%%\n", fastPathRate)
func (d *Detector) GetPromotionStats() PromotionStats {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.stats
}
