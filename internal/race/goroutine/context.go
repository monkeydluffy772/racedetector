package goroutine

import (
	"github.com/kolkov/racedetector/internal/race/epoch"
	"github.com/kolkov/racedetector/internal/race/vectorclock"
)

// RaceContext represents the race detection state for a single goroutine.
//
// Each goroutine has its own RaceContext tracking logical time and happens-before
// relationships. The context maintains both a full vector clock (C) and a cached
// epoch (Epoch) for the current thread.
//
// The epoch cache enables FastTrack's critical optimization: most operations
// (96%+) only need the epoch value, avoiding expensive vector clock operations.
//
// Layout:
//   - TID: Thread/Goroutine ID (0-65535, uint16)
//   - C: Full vector clock [65536]uint32 tracking all threads
//   - Epoch: Cached value of C[TID] as compact 64-bit epoch
//
// Invariant: Epoch must ALWAYS equal epoch.NewEpoch(TID, C[TID]).
// This invariant is maintained by IncrementClock() which atomically updates both.
type RaceContext struct {
	// TID is the thread/goroutine identifier (0-65535).
	// 16-bit uint for production (65,536 concurrent goroutines max).
	TID uint16

	// C is the full vector clock tracking logical time for all threads.
	// C[i] represents the logical time for thread i.
	// This is used for happens-before checks when epoch fast-path fails.
	C *vectorclock.VectorClock

	// Epoch is the cached epoch for this goroutine: Epoch == C[TID].
	// This enables O(1) access to the current logical time without
	// accessing the full vector clock array.
	//
	// CRITICAL: This field is on the hot path for every memory access!
	// Must be kept in sync with C[TID] at all times.
	Epoch epoch.Epoch
}

// Alloc creates and initializes a new RaceContext for the given thread ID.
//
// The context is initialized with:
//   - TID set to the provided tid
//   - C initialized as a zero vector clock (all threads at time 0)
//   - Epoch set to epoch.NewEpoch(tid, 0) (TID@0)
//
// This represents a newly started goroutine at the beginning of logical time.
//
// Example:
//
//	ctx := Alloc(5)
//	// ctx.TID = 5
//	// ctx.C = {0:0, 1:0, ..., 65535:0}
//	// ctx.Epoch = 0@5 (clock=0, tid=5)
func Alloc(tid uint16) *RaceContext {
	ctx := &RaceContext{
		TID: tid,
		C:   vectorclock.New(),
	}
	// Initialize epoch cache to TID@0 (clock 0 for new goroutine).
	ctx.Epoch = epoch.NewEpoch(tid, 0)
	return ctx
}

// IncrementClock advances the logical clock for this goroutine.
//
// This is called on every memory access by this goroutine to represent
// forward progress in logical time. It performs two atomic updates:
//  1. Increments C[TID] in the vector clock
//  2. Updates the cached Epoch to reflect the new C[TID] value
//
// The updates are performed sequentially to maintain the invariant:
//
//	Epoch == epoch.NewEpoch(TID, C[TID])
//
// Performance: Target <200ns/op (VectorClock.Increment + Epoch creation).
// This is on the hot path - called millions of times during execution.
//
// Example:
//
//	ctx := Alloc(5)
//	// ctx.C[5] = 0, ctx.Epoch = 0@5
//	ctx.IncrementClock()
//	// ctx.C[5] = 1, ctx.Epoch = 1@5
//	ctx.IncrementClock()
//	// ctx.C[5] = 2, ctx.Epoch = 2@5
func (rc *RaceContext) IncrementClock() {
	// Step 1: Increment the vector clock for this thread.
	rc.C.Increment(rc.TID)

	// Step 2: Update the cached epoch to match C[TID].
	// This maintains the invariant: Epoch == epoch.NewEpoch(TID, C[TID]).
	rc.Epoch = epoch.NewEpoch(rc.TID, uint64(rc.C.Get(rc.TID)))
}

// GetEpoch returns the cached epoch for this goroutine.
//
// This is the CRITICAL HOT PATH operation - called on every memory access
// for race detection. It must be:
//   - O(1): Just a field access, no computation
//   - Zero allocations
//   - Inline-candidate (no function call overhead)
//   - //go:nosplit to prevent stack growth
//
// The cached epoch represents the current logical time for this goroutine
// as a compact 32-bit value (TID in top 8 bits, clock in bottom 24 bits).
//
// Performance: Target <1ns/op (single field read).
//
// Example:
//
//	ctx := Alloc(5)
//	e := ctx.GetEpoch()  // Returns 0@5 (clock=0, tid=5)
//	ctx.IncrementClock()
//	e = ctx.GetEpoch()   // Returns 1@5 (clock=1, tid=5)
//
//go:nosplit
func (rc *RaceContext) GetEpoch() epoch.Epoch {
	return rc.Epoch
}
