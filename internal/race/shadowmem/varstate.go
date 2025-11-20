// Package shadowmem implements shadow memory cells for FastTrack race detection.
//
// Shadow memory stores the access history for every instrumented memory location.
// VarState is the basic building block - a single cell tracking the last write
// and last read to a variable.
package shadowmem

import (
	"sync"

	"github.com/kolkov/racedetector/internal/race/epoch"
	"github.com/kolkov/racedetector/internal/race/vectorclock"
)

// VarState stores the access state for a single variable using adaptive representation.
//
// ADAPTIVE REPRESENTATION (FastTrack Phase 3 optimization):
//   - Common case: Exclusive access (one writer OR one reader)
//   - Uses Epoch only (4 bytes for write + 4 bytes for read = 8 bytes)
//   - O(1) operations, zero allocations
//   - Conflict case: Shared reads or multiple writers
//   - Promotes to VectorClock (1KB allocation for readClock)
//   - O(N) operations, tracks all threads
//
// Promotion triggers:
//   - OnRead: When second concurrent reader detected (readEpoch → readClock)
//   - OnWrite: Write operations demote back to Epoch (readClock → nil)
//
// SMARTTRACK OWNERSHIP TRACKING (v0.2.0 Task 3 optimization):
//   - Track exclusive writer TID to skip happens-before checks
//   - Common case (80%): Single writer pattern (owned)
//   - Fast path: No HB checks when reader == exclusive writer
//   - Shared case (20%): Multiple writers (exclusiveWriter = -1)
//   - Expected impact: 10-20% reduction in HB comparisons (PLDI 2020)
//
// Ownership states:
//   - exclusiveWriter >= 0: Single owner (fast path, skip HB checks)
//   - exclusiveWriter == -1: Shared/multiple writers (full FastTrack)
//   - exclusiveWriter == 0: Uninitialized (no writes yet)
//
// Memory layout (v0.2.0 Task 6 updated):
//   - Base: 8 bytes (W) + 8 bytes (mu) + 4 bytes (readEpoch) + 8 bytes (readClock ptr) = 28 bytes
//   - SmartTrack: + 8 bytes (exclusiveWriter) + 4 bytes (writeCount) = 40 bytes
//   - Stack Traces (Task 6): + 8 bytes (writeStackHash) + 8 bytes (readStackHash) = 56 bytes
//   - Total fast path: 56 bytes per variable
//   - Promoted path: 56 bytes + 1024 bytes (VectorClock allocation) = 1080 bytes
//
// Trade-off: 56 bytes per variable (was 40 bytes) for complete race reports with both stacks.
type VarState struct {
	W  epoch.Epoch // Last write epoch (always present).
	mu sync.Mutex  // Protects readEpoch, readClock, and ownership fields from concurrent access.

	// Read tracking (ADAPTIVE):
	// If readClock == nil → use readEpoch (single reader, common case)
	// If readClock != nil → use readClock (multiple readers, rare case)
	readEpoch epoch.Epoch              // Single reader (fast path, 4 bytes).
	readClock *vectorclock.VectorClock // Multiple readers (promoted, 8 bytes pointer + 1KB allocation).

	// SmartTrack ownership tracking (v0.2.0 Task 3):
	// Tracks exclusive writer to skip expensive happens-before checks.
	exclusiveWriter int64  // TID of sole writer, -1 if shared, 0 if uninitialized.
	writeCount      uint32 // Number of writes (for statistics and debugging).

	// Stack Trace Storage (v0.2.0 Task 6):
	// Hash references to stack depot for previous write/read.
	// Enables complete race reports showing both current and previous stacks.
	writeStackHash uint64 // Hash of stack trace for last write (8 bytes).
	readStackHash  uint64 // Hash of stack trace for last read (8 bytes, only set when read-shared).
}

// NewVarState creates a new zero-initialized variable state.
//
// A zero VarState represents a variable that has never been accessed.
// Both W and readEpoch are zero (TID=0, Clock=0), readClock is nil.
//
//go:nosplit
func NewVarState() *VarState {
	return &VarState{}
}

// Reset resets the variable state to zero.
//
// This is used when a memory location is freed and reused.
// After Reset(), the state represents a fresh, never-accessed variable.
// If promoted, this demotes back to fast path (frees VectorClock).
//
// SmartTrack (v0.2.0 Task 3): Also resets ownership tracking fields.
// Stack Traces (v0.2.0 Task 6): Also clears stack hashes.
//
// Performance: This operation must be zero-allocation and inline-friendly.
// Target: <2ns/op.
//
// Note: Removed //go:nosplit because sync.Mutex.Lock() requires stack space.
func (vs *VarState) Reset() {
	vs.W = 0
	vs.mu.Lock()
	vs.readEpoch = 0
	vs.readClock = nil // Demote if promoted.
	vs.exclusiveWriter = 0
	vs.writeCount = 0
	vs.writeStackHash = 0
	vs.readStackHash = 0
	vs.mu.Unlock()
}

// IsPromoted returns true if VarState uses VectorClock (promoted state).
//
// This is the discriminator for the adaptive representation:
//   - false: Fast path (readEpoch only, 4 bytes)
//   - true: Slow path (readClock, 1KB allocation)
//
// Note: Removed //go:nosplit because sync.Mutex.Lock() requires stack space.
func (vs *VarState) IsPromoted() bool {
	vs.mu.Lock()
	promoted := vs.readClock != nil
	vs.mu.Unlock()
	return promoted
}

// PromoteToReadClock upgrades from single-reader Epoch to multi-reader VectorClock.
//
// This happens when:
//   - Second concurrent reader detected
//   - Current readEpoch conflicts with new read (different TID, not happens-before)
//
// Steps:
//  1. Allocate VectorClock (one-time cost, 1KB)
//  2. Copy existing readEpoch into VectorClock[TID]
//  3. Merge new read VectorClock
//  4. Set readClock != nil (marks as promoted)
//
// After promotion, all subsequent reads use VectorClock path.
//
// Performance: This is a one-time cost (~100ns allocation + copy).
// Should be rare (only 0.1% of variables according to FastTrack paper).
//
// Parameters:
//   - newReadVC: The VectorClock of the new concurrent reader
func (vs *VarState) PromoteToReadClock(newReadVC *vectorclock.VectorClock) {
	vs.mu.Lock()
	defer vs.mu.Unlock()

	// Allocate VectorClock for promoted read tracking.
	vs.readClock = vectorclock.New()

	// Copy existing single reader epoch into VectorClock.
	// If readEpoch is non-zero, it represents a previous reader.
	if vs.readEpoch != 0 {
		tid, clock := vs.readEpoch.Decode()
		//nolint:gosec // G115: Epoch clock is uint64, but per-thread VectorClock uses uint32 (safe truncation).
		vs.readClock.Set(tid, uint32(clock))
	}

	// Merge the new reader's VectorClock.
	vs.readClock.Join(newReadVC)

	// Clear readEpoch as it's no longer used (readClock takes over).
	vs.readEpoch = 0
}

// GetReadEpoch returns the read epoch (fast path only).
//
// PRECONDITION: !IsPromoted() - caller must check this first.
// If promoted, this returns 0 (invalid epoch).
//
// This is used by detector OnRead/OnWrite for fast-path checks.
//
// Note: Removed //go:nosplit because sync.Mutex.Lock() requires stack space.
func (vs *VarState) GetReadEpoch() epoch.Epoch {
	vs.mu.Lock()
	e := vs.readEpoch
	vs.mu.Unlock()
	return e
}

// SetReadEpoch sets the read epoch (fast path only).
//
// This is used by detector OnRead to update single-reader state.
// If already promoted, this is a no-op (readClock takes precedence).
//
// Note: Removed //go:nosplit because sync.Mutex.Lock() requires stack space.
func (vs *VarState) SetReadEpoch(e epoch.Epoch) {
	vs.mu.Lock()
	if vs.readClock == nil { // Not promoted
		vs.readEpoch = e
	}
	vs.mu.Unlock()
}

// GetReadClock returns the read VectorClock (slow path only).
//
// PRECONDITION: IsPromoted() - caller must check this first.
// If not promoted, this returns nil.
//
// Note: Removed //go:nosplit because sync.Mutex.Lock() requires stack space.
func (vs *VarState) GetReadClock() *vectorclock.VectorClock {
	vs.mu.Lock()
	rc := vs.readClock
	vs.mu.Unlock()
	return rc
}

// Demote clears the VectorClock and demotes back to fast path (Epoch only).
//
// This is called by OnWrite after a write operation to reset read tracking.
// Write dominates all previous reads, so we can safely clear the read state.
//
// This is a key optimization: variables with alternating read/write stay in fast path.
//
// Note: Removed //go:nosplit because sync.Mutex.Lock() requires stack space.
func (vs *VarState) Demote() {
	vs.mu.Lock()
	vs.readEpoch = 0
	vs.readClock = nil
	vs.mu.Unlock()
}

// === SmartTrack Ownership Tracking Methods (v0.2.0 Task 3) ===

// IsOwned returns true if the variable has an exclusive writer (owned state).
//
// Ownership states:
//   - exclusiveWriter >= 0: Single owner (fast path, skip HB checks)
//   - exclusiveWriter == -1: Shared/multiple writers (full FastTrack)
//   - exclusiveWriter == 0: Uninitialized (no writes yet)
//
// This is used by the detector to decide whether to skip happens-before checks.
//
// Thread Safety: Protected by mutex.
// Performance: <5ns/op (mutex + field read).
//
// Note: Removed //go:nosplit because sync.Mutex.Lock() requires stack space.
func (vs *VarState) IsOwned() bool {
	vs.mu.Lock()
	owned := vs.exclusiveWriter >= 0
	vs.mu.Unlock()
	return owned
}

// GetExclusiveWriter returns the TID of the exclusive writer, or -1 if shared.
//
// Returns:
//   - TID >= 0: Single exclusive writer (owned)
//   - -1: Shared/multiple writers
//   - 0: Uninitialized (no writes yet)
//
// Thread Safety: Protected by mutex.
// Performance: <5ns/op (mutex + field read).
//
// Note: Removed //go:nosplit because sync.Mutex.Lock() requires stack space.
func (vs *VarState) GetExclusiveWriter() int64 {
	vs.mu.Lock()
	writer := vs.exclusiveWriter
	vs.mu.Unlock()
	return writer
}

// SetExclusiveWriter sets the exclusive writer TID.
//
// This is called when:
//   - First write: Claim ownership (tid >= 0)
//   - Second writer detected: Promote to shared (tid = -1)
//
// Thread Safety: Protected by mutex.
// Performance: <5ns/op (mutex + field write).
//
// Note: Removed //go:nosplit because sync.Mutex.Lock() requires stack space.
func (vs *VarState) SetExclusiveWriter(tid int64) {
	vs.mu.Lock()
	vs.exclusiveWriter = tid
	vs.mu.Unlock()
}

// IncrementWriteCount increments the write counter.
//
// This is called on every write to track total write operations.
// Used for statistics and debugging.
//
// Thread Safety: Protected by mutex.
// Performance: <5ns/op (mutex + field increment).
//
// Note: Removed //go:nosplit because sync.Mutex.Lock() requires stack space.
func (vs *VarState) IncrementWriteCount() {
	vs.mu.Lock()
	vs.writeCount++
	vs.mu.Unlock()
}

// GetWriteCount returns the total number of writes to this variable.
//
// Thread Safety: Protected by mutex.
// Performance: <5ns/op (mutex + field read).
//
// Note: Removed //go:nosplit because sync.Mutex.Lock() requires stack space.
func (vs *VarState) GetWriteCount() uint32 {
	vs.mu.Lock()
	count := vs.writeCount
	vs.mu.Unlock()
	return count
}

// String returns a debug representation of the variable state.
//
// Format:
//   - Unpromoted: "W:<epoch> R:<epoch>"
//   - Promoted: "W:<epoch> R:<vectorclock> [PROMOTED]"
//
// Example:
//   - "W:100@5 R:50@3" (single reader, fast path)
//   - "W:100@5 R:{0:50, 1:60} [PROMOTED]" (multiple readers, slow path)
//
// This method is only used for debugging and race reporting, not on hot path.
func (vs *VarState) String() string {
	// Note: We manually build the string to avoid fmt import overhead.
	wStr := "W:" + vs.W.String()

	vs.mu.Lock()
	defer vs.mu.Unlock()

	if vs.readClock != nil {
		// Promoted: Show VectorClock.
		return wStr + " R:" + vs.readClock.String() + " [PROMOTED]"
	}

	// Fast path: Show Epoch.
	return wStr + " R:" + vs.readEpoch.String()
}

// === Stack Trace Accessor Methods (v0.2.0 Task 6) ===

// SetWriteStack records the stack trace hash for a write access.
//
// This is called by the detector on every write to store the stack trace
// for later retrieval during race reporting.
//
// Parameters:
//   - stackHash: Hash returned by stackdepot.CaptureStack()
//
// Thread Safety: Uses atomic store for lock-free access.
// Performance: ~2ns (atomic store).
//
// Note: Removed //go:nosplit because sync.Mutex.Lock() requires stack space.
func (vs *VarState) SetWriteStack(stackHash uint64) {
	vs.mu.Lock()
	vs.writeStackHash = stackHash
	vs.mu.Unlock()
}

// GetWriteStack retrieves the stack trace hash for the last write.
//
// This is called during race reporting to retrieve the previous write stack.
//
// Returns:
//   - uint64: Hash that can be passed to stackdepot.GetStack()
//   - 0: If no write stack has been captured
//
// Thread Safety: Uses atomic load for lock-free access.
// Performance: ~2ns (atomic load).
//
// Note: Removed //go:nosplit because sync.Mutex.Lock() requires stack space.
func (vs *VarState) GetWriteStack() uint64 {
	vs.mu.Lock()
	hash := vs.writeStackHash
	vs.mu.Unlock()
	return hash
}

// SetReadStack records the stack trace hash for a read access (read-shared case).
//
// This is called by the detector on read to promoted (read-shared) variables.
// For unpromoted variables, read stack is not stored (not needed for races).
//
// Parameters:
//   - stackHash: Hash returned by stackdepot.CaptureStack()
//
// Thread Safety: Protected by mutex (consistent with other read state).
// Performance: ~5ns (mutex + field write).
//
// Note: Removed //go:nosplit because sync.Mutex.Lock() requires stack space.
func (vs *VarState) SetReadStack(stackHash uint64) {
	vs.mu.Lock()
	vs.readStackHash = stackHash
	vs.mu.Unlock()
}

// GetReadStack retrieves the stack trace hash for the last read.
//
// This is called during race reporting to retrieve the previous read stack.
// Only meaningful for promoted (read-shared) variables.
//
// Returns:
//   - uint64: Hash that can be passed to stackdepot.GetStack()
//   - 0: If no read stack has been captured
//
// Thread Safety: Protected by mutex (consistent with other read state).
// Performance: ~5ns (mutex + field read).
//
// Note: Removed //go:nosplit because sync.Mutex.Lock() requires stack space.
func (vs *VarState) GetReadStack() uint64 {
	vs.mu.Lock()
	hash := vs.readStackHash
	vs.mu.Unlock()
	return hash
}
