package goroutine

import (
	"testing"

	"github.com/kolkov/racedetector/internal/race/epoch"
	"github.com/kolkov/racedetector/internal/race/vectorclock"
)

// TestAlloc tests RaceContext allocation and initialization.
func TestAlloc(t *testing.T) {
	tests := []struct {
		name    string
		tid     uint16
		wantTID uint16
	}{
		{
			name:    "zero tid",
			tid:     0,
			wantTID: 0,
		},
		{
			name:    "small tid",
			tid:     5,
			wantTID: 5,
		},
		{
			name:    "mid tid",
			tid:     128,
			wantTID: 128,
		},
		{
			name:    "max tid",
			tid:     255,
			wantTID: 255,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := Alloc(tt.tid)

			// Verify TID is set correctly.
			if ctx.TID != tt.wantTID {
				t.Errorf("Alloc(%d).TID = %d, want %d", tt.tid, ctx.TID, tt.wantTID)
			}

			// Verify vector clock is allocated and zero-initialized.
			if ctx.C == nil {
				t.Fatal("Alloc() returned nil vector clock")
			}
			for i := 0; i < vectorclock.MaxThreads; i++ {
				if ctx.C.Get(uint16(i)) != 0 {
					t.Errorf("Alloc() C[%d] = %d, want 0", i, ctx.C.Get(uint16(i)))
				}
			}

			// Verify epoch cache is initialized correctly.
			wantEpoch := epoch.NewEpoch(tt.tid, 0)
			if ctx.Epoch != wantEpoch {
				t.Errorf("Alloc(%d).Epoch = 0x%X, want 0x%X", tt.tid, ctx.Epoch, wantEpoch)
			}

			// Verify epoch cache matches C[TID] (invariant).
			tidClock := ctx.C.Get(ctx.TID)
			epochFromVC := epoch.NewEpoch(ctx.TID, uint64(tidClock))
			if ctx.Epoch != epochFromVC {
				t.Errorf("Epoch cache out of sync: Epoch=0x%X, NewEpoch(TID=%d, C[%d]=%d)=0x%X",
					ctx.Epoch, ctx.TID, ctx.TID, tidClock, epochFromVC)
			}
		})
	}
}

// verifyClockValue checks that the clock value matches expected.
func verifyClockValue(t *testing.T, ctx *RaceContext, wantClock uint64, increments int) {
	t.Helper()
	gotClock := ctx.C.Get(ctx.TID)
	if gotClock != wantClock {
		t.Errorf("After %d increments, C[%d] = %d, want %d",
			increments, ctx.TID, gotClock, wantClock)
	}
}

// verifyEpochCache checks that epoch cache is synchronized with C[TID].
func verifyEpochCache(t *testing.T, ctx *RaceContext, tid uint16, wantClock uint64, increments int) {
	t.Helper()
	wantEpoch := epoch.NewEpoch(tid, uint64(wantClock))
	if ctx.Epoch != wantEpoch {
		t.Errorf("After %d increments, Epoch = 0x%X, want 0x%X",
			increments, ctx.Epoch, wantEpoch)
	}

	gotTID, gotEpochClock := ctx.Epoch.Decode()
	if gotTID != tid {
		t.Errorf("Epoch.Decode() tid = %d, want %d", gotTID, tid)
	}
	if gotEpochClock != uint64(wantClock) {
		t.Errorf("Epoch.Decode() clock = %d, want %d", gotEpochClock, wantClock)
	}
}

// verifyThreadIsolation checks that other threads' clocks are unchanged.
func verifyThreadIsolation(t *testing.T, ctx *RaceContext) {
	t.Helper()
	for i := 0; i < vectorclock.MaxThreads; i++ {
		if uint16(i) == ctx.TID {
			continue
		}
		if ctx.C.Get(uint16(i)) != 0 {
			t.Errorf("IncrementClock() affected other thread: C[%d] = %d, want 0",
				i, ctx.C.Get(uint16(i)))
		}
	}
}

// TestIncrementClock tests logical clock advancement.
func TestIncrementClock(t *testing.T) {
	tests := []struct {
		name       string
		tid        uint8
		increments int
		wantClock  uint32
	}{
		{
			name:       "single increment",
			tid:        5,
			increments: 1,
			wantClock:  1,
		},
		{
			name:       "multiple increments",
			tid:        10,
			increments: 100,
			wantClock:  100,
		},
		{
			name:       "zero increments",
			tid:        0,
			increments: 0,
			wantClock:  0,
		},
		{
			name:       "max tid increments",
			tid:        255,
			increments: 42,
			wantClock:  42,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := Alloc(uint16(tt.tid))

			// Perform increments.
			for i := 0; i < tt.increments; i++ {
				ctx.IncrementClock()
			}

			// Verify all invariants using helper functions.
			verifyClockValue(t, ctx, tt.wantClock, tt.increments)
			verifyEpochCache(t, ctx, uint16(tt.tid), tt.wantClock, tt.increments)
			verifyThreadIsolation(t, ctx)
		})
	}
}

// TestIncrementClockEpochSync tests epoch cache stays in sync with C[TID].
func TestIncrementClockEpochSync(t *testing.T) {
	ctx := Alloc(42)

	// Perform many increments and verify sync after each.
	for i := 1; i <= 1000; i++ {
		ctx.IncrementClock()

		// Verify epoch cache matches C[TID].
		expectedEpoch := epoch.NewEpoch(ctx.TID, uint64(ctx.C.Get(ctx.TID)))
		if ctx.Epoch != expectedEpoch {
			t.Errorf("Iteration %d: Epoch cache out of sync: got 0x%X, want 0x%X",
				i, ctx.Epoch, expectedEpoch)
		}

		// Verify epoch decodes to correct clock value.
		_, clock := ctx.Epoch.Decode()
		if clock != uint64(i) {
			t.Errorf("Iteration %d: Epoch clock = %d, want %d", i, clock, i)
		}
	}
}

// TestGetEpoch tests cached epoch retrieval.
func TestGetEpoch(t *testing.T) {
	tests := []struct {
		name       string
		tid        uint8
		increments int
	}{
		{
			name:       "initial epoch (clock=0)",
			tid:        5,
			increments: 0,
		},
		{
			name:       "after single increment",
			tid:        10,
			increments: 1,
		},
		{
			name:       "after many increments",
			tid:        42,
			increments: 1000,
		},
		{
			name:       "max tid",
			tid:        255,
			increments: 100,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := Alloc(uint16(tt.tid))

			// Perform increments.
			for i := 0; i < tt.increments; i++ {
				ctx.IncrementClock()
			}

			// Get epoch using GetEpoch().
			gotEpoch := ctx.GetEpoch()

			// Verify it matches the cached Epoch field.
			if gotEpoch != ctx.Epoch {
				t.Errorf("GetEpoch() = 0x%X, want 0x%X (ctx.Epoch)", gotEpoch, ctx.Epoch)
			}

			// Verify it matches the expected epoch from C[TID].
			wantEpoch := epoch.NewEpoch(uint16(tt.tid), uint64(ctx.C.Get(uint16(tt.tid))))
			if gotEpoch != wantEpoch {
				t.Errorf("GetEpoch() = 0x%X, want 0x%X (from C[%d])", gotEpoch, wantEpoch, uint64(tt.tid))
			}

			// Verify epoch decodes correctly.
			gotTID, gotClock := gotEpoch.Decode()
			if gotTID != tt.tid {
				t.Errorf("GetEpoch().Decode() tid = %d, want %d", gotTID, uint64(tt.tid))
			}
			if gotClock != uint32(tt.increments) {
				t.Errorf("GetEpoch().Decode() clock = %d, want %d", gotClock, tt.increments)
			}
		})
	}
}

// TestGetEpochMultipleCalls tests that GetEpoch() is idempotent.
func TestGetEpochMultipleCalls(t *testing.T) {
	ctx := Alloc(7)
	ctx.IncrementClock()
	ctx.IncrementClock()
	ctx.IncrementClock()

	// Call GetEpoch() multiple times.
	e1 := ctx.GetEpoch()
	e2 := ctx.GetEpoch()
	e3 := ctx.GetEpoch()

	// All should return the same epoch.
	if e1 != e2 || e2 != e3 {
		t.Errorf("GetEpoch() not idempotent: e1=0x%X, e2=0x%X, e3=0x%X", e1, e2, e3)
	}

	// Verify the epoch is correct.
	wantEpoch := epoch.NewEpoch(7, 3)
	if e1 != wantEpoch {
		t.Errorf("GetEpoch() = 0x%X, want 0x%X", e1, wantEpoch)
	}
}

// TestTIDRange tests all valid TID values (0-255).
func TestTIDRange(t *testing.T) {
	for tid := 0; tid <= 255; tid++ {
		t.Run("tid_"+string(rune(tid)), func(t *testing.T) {
			ctx := Alloc(uint16(tid))

			// Verify TID is stored correctly.
			if ctx.TID != uint16(tid) {
				t.Errorf("Alloc(%d).TID = %d, want %d", tid, ctx.TID, tid)
			}

			// Increment and verify epoch cache.
			ctx.IncrementClock()
			wantEpoch := epoch.NewEpoch(uint16(tid), 1)
			if ctx.Epoch != wantEpoch {
				t.Errorf("TID %d: Epoch = 0x%X, want 0x%X", tid, ctx.Epoch, wantEpoch)
			}

			// Verify epoch decodes correctly.
			gotTID, gotClock := ctx.Epoch.Decode()
			if gotTID != uint16(tid) {
				t.Errorf("TID %d: Epoch.Decode() tid = %d, want %d", tid, gotTID, tid)
			}
			if gotClock != 1 {
				t.Errorf("TID %d: Epoch.Decode() clock = %d, want 1", tid, gotClock)
			}
		})
	}
}

// TestEpochCacheInvariant tests the critical invariant: Epoch == NewEpoch(TID, C[TID]).
func TestEpochCacheInvariant(t *testing.T) {
	ctx := Alloc(99)

	// Perform random increments and verify invariant holds.
	increments := []int{1, 5, 10, 50, 100, 1, 1, 1}
	for _, inc := range increments {
		for i := 0; i < inc; i++ {
			ctx.IncrementClock()
		}

		// Check invariant: ctx.Epoch == epoch.NewEpoch(ctx.TID, uint64(ctx.C.Get(ctx.TID)))
		expectedEpoch := epoch.NewEpoch(ctx.TID, uint64(ctx.C.Get(ctx.TID)))
		if ctx.Epoch != expectedEpoch {
			t.Errorf("Invariant violated: Epoch = 0x%X, NewEpoch(TID=%d, C[%d]=%d) = 0x%X",
				ctx.Epoch, ctx.TID, ctx.TID, ctx.C.Get(ctx.TID), expectedEpoch)
		}
	}
}

// TestVectorClockIsolation tests that incrementing one context doesn't affect others.
func TestVectorClockIsolation(t *testing.T) {
	ctx1 := Alloc(1)
	ctx2 := Alloc(2)
	ctx3 := Alloc(3)

	// Increment each context different amounts.
	ctx1.IncrementClock()
	ctx1.IncrementClock()
	ctx1.IncrementClock()

	ctx2.IncrementClock()

	ctx3.IncrementClock()
	ctx3.IncrementClock()

	// Verify each context has correct clock.
	if ctx1.C.Get(1) != 3 {
		t.Errorf("ctx1.C[1] = %d, want 3", ctx1.C.Get(1))
	}
	if ctx2.C.Get(2) != 1 {
		t.Errorf("ctx2.C[2] = %d, want 1", ctx2.C.Get(2))
	}
	if ctx3.C.Get(3) != 2 {
		t.Errorf("ctx3.C[3] = %d, want 2", ctx3.C.Get(3))
	}

	// Verify each context doesn't affect others' TID clocks.
	if ctx1.C.Get(2) != 0 || ctx1.C.Get(3) != 0 {
		t.Error("ctx1 affected other threads' clocks")
	}
	if ctx2.C.Get(1) != 0 || ctx2.C.Get(3) != 0 {
		t.Error("ctx2 affected other threads' clocks")
	}
	if ctx3.C.Get(1) != 0 || ctx3.C.Get(2) != 0 {
		t.Error("ctx3 affected other threads' clocks")
	}
}

// TestEpochClockOverflow tests behavior when clock reaches 24-bit limit.
func TestEpochClockOverflow(t *testing.T) {
	ctx := Alloc(5)

	// Set clock to near 24-bit max (0x00FFFFFF = 16,777,215).
	maxClock := uint32(0x00FFFFFF)
	ctx.C.Set(5, maxClock-1)
	ctx.Epoch = epoch.NewEpoch(5, maxClock-1)

	// Increment to max.
	ctx.IncrementClock()

	// Verify clock is at max.
	if ctx.C.Get(5) != maxClock {
		t.Errorf("C[5] = %d, want %d (max 24-bit)", ctx.C.Get(5), maxClock)
	}

	// Verify epoch cache matches.
	wantEpoch := epoch.NewEpoch(5, uint64(maxClock))
	if ctx.Epoch != wantEpoch {
		t.Errorf("Epoch at max = 0x%X, want 0x%X", ctx.Epoch, wantEpoch)
	}

	// Increment past max (overflow).
	ctx.IncrementClock()

	// Epoch will truncate to 24 bits (wrap to 0).
	actualClock := ctx.C.Get(5)                 // Will be 0x01000000 in VC
	epochClock := actualClock & epoch.ClockMask // Truncated to 0

	_, epochClockDecoded := ctx.Epoch.Decode()
	if epochClockDecoded != epochClock {
		t.Errorf("After overflow, epoch clock = %d, want %d (truncated)", epochClockDecoded, epochClock)
	}
}

// ========== BENCHMARKS ==========

// BenchmarkGetEpoch benchmarks the critical hot-path GetEpoch() operation.
// Target: <1ns/op, 0 allocs/op (just a field read).
func BenchmarkGetEpoch(b *testing.B) {
	ctx := Alloc(42)
	ctx.IncrementClock()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ctx.GetEpoch()
	}
}

// BenchmarkIncrementClock benchmarks logical clock advancement.
// Target: <200ns/op, 0 allocs/op (VectorClock update + epoch creation).
func BenchmarkIncrementClock(b *testing.B) {
	ctx := Alloc(42)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx.IncrementClock()
	}
}

// BenchmarkAlloc benchmarks RaceContext allocation.
func BenchmarkAlloc(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Alloc(uint8(i % 256))
	}
}

// BenchmarkGetEpochWithIncrement benchmarks realistic usage pattern.
// This simulates the typical cycle: increment clock, then read epoch.
func BenchmarkGetEpochWithIncrement(b *testing.B) {
	ctx := Alloc(42)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx.IncrementClock()
		_ = ctx.GetEpoch()
	}
}

// BenchmarkMultipleContexts benchmarks isolation with multiple contexts.
func BenchmarkMultipleContexts(b *testing.B) {
	contexts := make([]*RaceContext, 10)
	for i := range contexts {
		contexts[i] = Alloc(uint16(i))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx := contexts[i%10]
		ctx.IncrementClock()
		_ = ctx.GetEpoch()
	}
}
