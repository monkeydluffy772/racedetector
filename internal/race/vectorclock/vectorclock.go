// Package vectorclock implements vector clocks for tracking happens-before relations.
//
// Vector clocks are used in FastTrack algorithm for read-shared data (0.1% of accesses).
// Most operations (99%+) use lightweight epochs, but when concurrent reads occur,
// we promote to vector clocks to precisely track partial order across all threads.
//
// Key operations:
//   - Join: Synchronization (point-wise maximum) - used on lock acquire
//   - LessOrEqual: Happens-before check (partial order) - used for race detection
//
// Performance targets: Join < 500ns, LessOrEqual < 300ns, zero allocations.
package vectorclock

import "strings"

const (
	// MaxThreads is the maximum number of concurrent threads supported.
	// This is a fixed-size array for zero-allocation operation.
	//
	// 65,536 threads = 16-bit TID space (sufficient for 99%+ of real programs).
	// Memory: 65,536 × 4 bytes = 262,144 bytes = 256KB per VectorClock.
	//
	// Trade-off:
	//   - Pro: Supports up to 65K concurrent goroutines (vs 256 in MVP).
	//   - Con: 256KB per VectorClock (vs 1KB in MVP) - 256x memory increase.
	//   - Mitigation: VectorClock only allocated for read-shared variables (rare).
	//     FastTrack's adaptive algorithm keeps most variables in Epoch mode (8 bytes).
	//
	// v0.4 will add dynamic TID mapping for unlimited goroutines with compact storage.
	MaxThreads = 65536
)

// VectorClock represents logical time across multiple threads.
//
// Each element vc[tid] stores the clock value for thread tid.
// This is a fixed-size array (not a slice) to avoid heap allocations.
//
// Layout: [Thread0, Thread1, ..., Thread65535]
// Example: {0: 50, 1: 30, 2: 60, ...} means Thread0@50, Thread1@30, Thread2@60.
//
// Size: 65,536 × 4 bytes = 256KB (large, but only allocated for read-shared variables).
type VectorClock [MaxThreads]uint32

// New creates a zero-initialized vector clock.
//
// All thread clocks start at 0, representing the beginning of logical time.
// Returns a pointer to avoid copying the 1KB array on return.
func New() *VectorClock {
	return &VectorClock{}
}

// Clone creates a deep copy of the vector clock.
//
// This is used when we need to preserve a snapshot of logical time,
// for example when storing a vector clock in shadow memory.
//
// Returns a pointer to the new copy to avoid copying on return.
func (vc *VectorClock) Clone() *VectorClock {
	clone := &VectorClock{}
	*clone = *vc // Copy the array
	return clone
}

// Join performs point-wise maximum: vc = vc ⊔ other.
//
// This is the synchronization operation for happens-before in FastTrack.
// Used when a thread acquires a lock: Ct := Ct ⊔ Lm (thread clock joins lock clock).
//
// Algorithm: For each thread i, vc[i] = max(vc[i], other[i])
//
// Performance: Critical operation, must be fast. Uses simple loop for compiler
// auto-vectorization. Target: < 500ns, 0 allocs.
//
//go:nosplit
func (vc *VectorClock) Join(other *VectorClock) {
	for i := 0; i < MaxThreads; i++ {
		if other[i] > vc[i] {
			vc[i] = other[i]
		}
	}
}

// LessOrEqual checks partial order: vc ⊑ other.
//
// Returns true if vc[i] <= other[i] for all threads i.
// This implements the happens-before relation check.
//
// Used in FastTrack to check if a write epoch happens-before a read:
// If write's VC ⊑ read's VC, then no race (write happened-before read).
//
// Performance: Critical operation on race check path. Target: < 300ns, 0 allocs.
//
//go:nosplit
func (vc *VectorClock) LessOrEqual(other *VectorClock) bool {
	for i := 0; i < MaxThreads; i++ {
		if vc[i] > other[i] {
			return false
		}
	}
	return true
}

// HappensBefore checks if this VectorClock happened-before another VectorClock.
//
// This is an alias for LessOrEqual for better API clarity.
// Returns true if vc ⊑ other (all elements vc[i] <= other[i]).
//
// Used in adaptive VarState to check if read VectorClock happened-before write VectorClock.
//
// Performance: Same as LessOrEqual, < 300ns, 0 allocs.
//
//go:nosplit
func (vc *VectorClock) HappensBefore(other *VectorClock) bool {
	return vc.LessOrEqual(other)
}

// Increment advances the clock for thread tid.
//
// This is called on every memory access by thread tid.
// Increments vc[tid] to represent forward progress in logical time.
func (vc *VectorClock) Increment(tid uint16) {
	vc[tid]++
}

// Get returns the clock value for thread tid.
//
// Used to read a specific thread's logical time from the vector clock.
func (vc *VectorClock) Get(tid uint16) uint32 {
	return vc[tid]
}

// Set sets the clock value for thread tid.
//
// Used to update a specific thread's logical time in the vector clock.
// Typically used during initialization or synchronization operations.
func (vc *VectorClock) Set(tid uint16, clock uint32) {
	vc[tid] = clock
}

// String returns a debug representation of the vector clock.
//
// Format: "{tid1:clock1, tid2:clock2, ...}" showing only non-zero clocks.
// This is used for debugging and race reporting, not on hot path.
//
// Example: "{0:50, 1:30, 5:42}" means Thread0=50, Thread1=30, Thread5=42.
func (vc *VectorClock) String() string {
	var parts []string
	for i := 0; i < MaxThreads; i++ {
		if vc[i] != 0 {
			//nolint:gosec // G115: i is in range [0, 255], safe to convert to uint32.
			parts = append(parts, itoa(uint32(i))+":"+itoa(vc[i]))
		}
	}
	if len(parts) == 0 {
		return "{}"
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

// itoa converts an integer to string without fmt import.
// Simple implementation for debugging output only.
// This avoids importing fmt which can cause circular dependencies in runtime code.
func itoa(n uint32) string {
	if n == 0 {
		return "0"
	}

	// Calculate number of digits.
	tmp := n
	digits := 0
	for tmp > 0 {
		digits++
		tmp /= 10
	}

	// Build string from right to left.
	buf := make([]byte, digits)
	for i := digits - 1; i >= 0; i-- {
		buf[i] = byte('0' + n%10)
		n /= 10
	}

	return string(buf)
}
