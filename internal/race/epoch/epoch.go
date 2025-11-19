// Package epoch implements 32-bit logical timestamps for FastTrack race detector.
//
// Epoch represents a single thread's logical time as a compact 32-bit value:
// - Top 8 bits: Thread ID (0-255)
// - Bottom 24 bits: Clock value (0-16M)
//
// This encoding enables O(1) happens-before checks which are the foundation
// of FastTrack's performance (96%+ operations use epoch-only fast path).
package epoch

import "github.com/kolkov/racedetector/internal/race/vectorclock"

// Epoch is a 32-bit logical timestamp encoding both thread ID and clock value.
// Layout: [TID:8][Clock:24]
//
// Example: 0x05001234 represents TID=5, Clock=0x1234 (4660 decimal).
type Epoch uint32

const (
	// TIDBits is the number of bits allocated for thread ID (8 bits = 256 threads max).
	TIDBits = 8

	// ClockBits is the number of bits allocated for clock value (24 bits = 16M operations max).
	ClockBits = 24

	// ClockMask is the bitmask for extracting clock value (0x00FFFFFF).
	ClockMask = (1 << ClockBits) - 1
)

// NewEpoch creates an epoch from thread ID and clock value.
//
// The TID is stored in the top 8 bits, clock in the bottom 24 bits.
// Clock values beyond 24 bits are truncated (wraps at 16M).
//
//go:nosplit
func NewEpoch(tid uint8, clock uint32) Epoch {
	return Epoch(uint32(tid)<<ClockBits | (clock & ClockMask))
}

// Decode extracts the thread ID and clock value from an epoch.
//
// Returns: (tid uint8, clock uint32)
//
//go:nosplit
func (e Epoch) Decode() (tid uint8, clock uint32) {
	//nolint:gosec // G115: Intentional truncation to extract top 8 bits as TID.
	tid = uint8(e >> ClockBits)
	clock = uint32(e) & ClockMask
	return
}

// HappensBefore checks if this epoch happened before a vector clock.
//
// This is the CRITICAL O(1) operation that makes FastTrack fast!
// Called millions of times, must be zero-allocation, inline-candidate.
//
// Returns true if epoch's clock <= vc[epoch's TID].
//
//go:nosplit
func (e Epoch) HappensBefore(vc *vectorclock.VectorClock) bool {
	tid, clock := e.Decode()
	return clock <= vc.Get(tid)
}

// Same checks if two epochs are identical (same TID and clock).
//
// Used for fast-path same-epoch optimization (71% writes, 63% reads).
//
//go:nosplit
func (e Epoch) Same(other Epoch) bool {
	return e == other
}

// String returns a human-readable representation of the epoch.
//
// Format: "clock@tid" (e.g., "42@5" means clock=42, tid=5).
// This method is only used for debugging and race reporting, not on hot path.
func (e Epoch) String() string {
	tid, clock := e.Decode()
	// Note: Using basic string concatenation to avoid fmt import.
	// For a real runtime library, consider using strconv or runtime-internal formatting.
	return itoa(clock) + "@" + itoa(uint32(tid))
}

// itoa converts an integer to string without fmt import.
// Simple implementation for debugging output only.
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
