// Package epoch implements 64-bit logical timestamps for FastTrack race detector.
//
// Epoch represents a single thread's logical time as a compact 64-bit value:
// - Top 16 bits: Thread ID (0-65,535)
// - Bottom 48 bits: Clock value (0-281 trillion)
//
// This encoding enables O(1) happens-before checks which are the foundation
// of FastTrack's performance (96%+ operations use epoch-only fast path).
//
// Design rationale for uint64 (vs uint32):
// - Production workloads easily spawn 1000+ goroutines (256 was too small).
// - Tight loops can exceed 16M operations in seconds (24-bit clock overflowed).
// - Go 1.25+ has excellent 64-bit performance on all platforms.
// - Memory cost: 4 bytes → 8 bytes per variable (acceptable for reliability).
package epoch

import "github.com/kolkov/racedetector/internal/race/vectorclock"

// Epoch is a 64-bit logical timestamp encoding both thread ID and clock value.
// Layout: [TID:16][Clock:48]
//
// Example: 0x0005000000001234 represents TID=5, Clock=0x1234 (4660 decimal).
//
// Limits:
//   - Max TID: 65,535 (16-bit) - supports up to 65K concurrent goroutines.
//   - Max Clock: 281,474,976,710,655 (48-bit) - 281 trillion operations.
type Epoch uint64

const (
	// TIDBits is the number of bits allocated for thread ID.
	// 16 bits = 65,536 threads max (vs 256 in MVP).
	// This covers 99%+ of real-world programs; v0.4 will add dynamic TID mapping.
	TIDBits = 16

	// ClockBits is the number of bits allocated for clock value.
	// 48 bits = 281,474,976,710,655 operations max (vs 16M in MVP).
	// This is practically unlimited for any real program.
	ClockBits = 48

	// ClockMask is the bitmask for extracting clock value (0x0000FFFFFFFFFFFF).
	ClockMask = (1 << ClockBits) - 1
)

// NewEpoch creates an epoch from thread ID and clock value.
//
// The TID is stored in the top 16 bits, clock in the bottom 48 bits.
// Clock values beyond 48 bits are truncated (wraps at 281T, practically never happens).
//
//go:nosplit
func NewEpoch(tid uint16, clock uint64) Epoch {
	return Epoch(uint64(tid)<<ClockBits | (clock & ClockMask))
}

// Decode extracts the thread ID and clock value from an epoch.
//
// Returns: (tid uint16, clock uint64)
//
//go:nosplit
func (e Epoch) Decode() (tid uint16, clock uint64) {
	//nolint:gosec // G115: Intentional truncation to extract top 16 bits as TID.
	tid = uint16(e >> ClockBits)
	clock = uint64(e) & ClockMask
	return
}

// HappensBefore checks if this epoch happened before a vector clock.
//
// This is the CRITICAL O(1) operation that makes FastTrack fast!
// Called millions of times, must be zero-allocation, inline-candidate.
//
// Returns true if epoch's clock <= vc[epoch's TID].
//
// Note: VectorClock stores uint32 clocks per thread, but Epoch uses uint64 global clock.
// The comparison is safe since per-thread clocks rarely exceed 32-bit range.
//
//go:nosplit
func (e Epoch) HappensBefore(vc *vectorclock.VectorClock) bool {
	tid, clock := e.Decode()
	return clock <= uint64(vc.Get(tid))
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
	return itoa64(clock) + "@" + itoa64(uint64(tid))
}

// itoa64 converts a uint64 to string without fmt import.
// Simple implementation for debugging output only.
func itoa64(n uint64) string {
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
