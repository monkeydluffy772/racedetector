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
// Memory layout:
//   - Fast path (unpromoted): 8 bytes (W + readEpoch) + 8 bytes (readClock pointer = nil) = 16 bytes
//   - Slow path (promoted): 16 bytes + 1024 bytes (VectorClock allocation) = 1040 bytes
//
// This design achieves 64x memory savings in the common case (16 bytes vs 1040 bytes).
type VarState struct {
	W  epoch.Epoch // Last write epoch (always present).
	mu sync.Mutex  // Protects readEpoch and readClock from concurrent access.

	// Read tracking (ADAPTIVE):
	// If readClock == nil → use readEpoch (single reader, common case)
	// If readClock != nil → use readClock (multiple readers, rare case)
	readEpoch epoch.Epoch              // Single reader (fast path, 4 bytes).
	readClock *vectorclock.VectorClock // Multiple readers (promoted, 8 bytes pointer + 1KB allocation).
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
// Performance: This operation must be zero-allocation and inline-friendly.
// Target: <2ns/op.
//
// Note: Removed //go:nosplit because sync.Mutex.Lock() requires stack space.
func (vs *VarState) Reset() {
	vs.W = 0
	vs.mu.Lock()
	vs.readEpoch = 0
	vs.readClock = nil // Demote if promoted.
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
		vs.readClock.Set(tid, clock)
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
