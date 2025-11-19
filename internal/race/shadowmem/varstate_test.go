package shadowmem

import (
	"testing"
	"unsafe"

	"github.com/kolkov/racedetector/internal/race/epoch"
)

// TestVarStateSize verifies that VarState has expected size.
// Phase 3: 24 bytes (W + mu + readEpoch + readClock pointer).
// This is larger than original Phase 3 (16 bytes) due to sync.Mutex for race-free operation.
// Trade-off: +8 bytes per VarState for correctness (no data races in detector itself).
func TestVarStateSize(t *testing.T) {
	const expectedSize = 24 // W(8) + mu(8) + readEpoch(4) + readClock(8) = 28, aligned to 24
	actualSize := unsafe.Sizeof(VarState{})

	if actualSize != expectedSize {
		t.Errorf("VarState size = %d bytes, want %d bytes (W + mu + readEpoch + pointer)", actualSize, expectedSize)
	}

	t.Logf("VarState size: %d bytes (adaptive representation + race-safe)", actualSize)
}

// TestVarStateNewZero verifies that NewVarState creates a zero-initialized state.
func TestVarStateNewZero(t *testing.T) {
	vs := NewVarState()

	if vs == nil {
		t.Fatal("NewVarState() returned nil")
	}

	// Both W and readEpoch should be zero.
	if vs.W != 0 {
		t.Errorf("NewVarState().W = %v, want 0", vs.W)
	}
	if vs.GetReadEpoch() != 0 {
		t.Errorf("NewVarState().GetReadEpoch() = %v, want 0", vs.GetReadEpoch())
	}

	// Should not be promoted.
	if vs.IsPromoted() {
		t.Error("NewVarState() should not be promoted")
	}

	// Verify epochs decode to zero TID and clock.
	wTID, wClock := vs.W.Decode()
	rTID, rClock := vs.GetReadEpoch().Decode()

	if wTID != 0 || wClock != 0 {
		t.Errorf("NewVarState().W decoded = (tid=%d, clock=%d), want (0, 0)", wTID, wClock)
	}
	if rTID != 0 || rClock != 0 {
		t.Errorf("NewVarState().GetReadEpoch() decoded = (tid=%d, clock=%d), want (0, 0)", rTID, rClock)
	}

	t.Logf("NewVarState() correctly initialized: W=%s R=%s", vs.W, vs.GetReadEpoch())
}

// TestVarStateReset verifies that Reset zeros both W and readEpoch fields.
func TestVarStateReset(t *testing.T) {
	vs := NewVarState()

	// Set both W and readEpoch to non-zero values.
	vs.W = epoch.NewEpoch(5, 100)
	vs.SetReadEpoch(epoch.NewEpoch(3, 50))

	// Verify they were set.
	if vs.W == 0 || vs.GetReadEpoch() == 0 {
		t.Fatalf("Setup failed: W=%v readEpoch=%v, expected non-zero", vs.W, vs.GetReadEpoch())
	}

	// Reset should zero both fields.
	vs.Reset()

	if vs.W != 0 {
		t.Errorf("After Reset(), W = %v, want 0", vs.W)
	}
	if vs.GetReadEpoch() != 0 {
		t.Errorf("After Reset(), GetReadEpoch() = %v, want 0", vs.GetReadEpoch())
	}
	if vs.IsPromoted() {
		t.Error("After Reset(), should not be promoted")
	}

	t.Logf("Reset() correctly zeroed state: W=%s R=%s", vs.W, vs.GetReadEpoch())
}

// TestVarStateReadWrite verifies that W and R epochs can be set and read.
func TestVarStateReadWrite(t *testing.T) {
	tests := []struct {
		name     string
		wTID     uint8
		wClock   uint32
		rTID     uint8
		rClock   uint32
		wantWStr string // Expected W.String() format.
		wantRStr string // Expected R.String() format.
	}{
		{
			name:     "simple write and read",
			wTID:     5,
			wClock:   100,
			rTID:     3,
			rClock:   50,
			wantWStr: "100@5",
			wantRStr: "50@3",
		},
		{
			name:     "same thread write and read",
			wTID:     7,
			wClock:   200,
			rTID:     7,
			rClock:   199,
			wantWStr: "200@7",
			wantRStr: "199@7",
		},
		{
			name:     "zero epochs",
			wTID:     0,
			wClock:   0,
			rTID:     0,
			rClock:   0,
			wantWStr: "0@0",
			wantRStr: "0@0",
		},
		{
			name:     "max thread ID (255)",
			wTID:     255,
			wClock:   1000,
			rTID:     255,
			rClock:   999,
			wantWStr: "1000@255",
			wantRStr: "999@255",
		},
		{
			name:     "large clock values",
			wTID:     1,
			wClock:   0xFFFFFF, // Max 24-bit clock.
			rTID:     2,
			rClock:   0xFFFFFE,
			wantWStr: "16777215@1", // 0xFFFFFF = 16777215 decimal.
			wantRStr: "16777214@2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vs := NewVarState()

			// Set W and readEpoch.
			vs.W = epoch.NewEpoch(tt.wTID, tt.wClock)
			vs.SetReadEpoch(epoch.NewEpoch(tt.rTID, tt.rClock))

			// Verify W epoch.
			wTID, wClock := vs.W.Decode()
			if wTID != tt.wTID {
				t.Errorf("W.TID = %d, want %d", wTID, tt.wTID)
			}
			if wClock != tt.wClock {
				t.Errorf("W.Clock = %d, want %d", wClock, tt.wClock)
			}

			// Verify readEpoch.
			rTID, rClock := vs.GetReadEpoch().Decode()
			if rTID != tt.rTID {
				t.Errorf("GetReadEpoch().TID = %d, want %d", rTID, tt.rTID)
			}
			if rClock != tt.rClock {
				t.Errorf("GetReadEpoch().Clock = %d, want %d", rClock, tt.rClock)
			}

			// Verify String() output.
			wStr := vs.W.String()
			if wStr != tt.wantWStr {
				t.Errorf("W.String() = %q, want %q", wStr, tt.wantWStr)
			}
			rStr := vs.GetReadEpoch().String()
			if rStr != tt.wantRStr {
				t.Errorf("GetReadEpoch().String() = %q, want %q", rStr, tt.wantRStr)
			}

			t.Logf("VarState: W=%s R=%s", vs.W, vs.GetReadEpoch())
		})
	}
}

// TestVarStateString verifies the String() method's debug output format.
func TestVarStateString(t *testing.T) {
	tests := []struct {
		name string
		vs   func() *VarState
		want string
	}{
		{
			name: "zero state",
			vs: func() *VarState {
				return &VarState{}
			},
			want: "W:0@0 R:0@0",
		},
		{
			name: "write epoch set",
			vs: func() *VarState {
				vs := NewVarState()
				vs.W = epoch.NewEpoch(5, 100)
				return vs
			},
			want: "W:100@5 R:0@0",
		},
		{
			name: "read epoch set",
			vs: func() *VarState {
				vs := NewVarState()
				vs.SetReadEpoch(epoch.NewEpoch(3, 50))
				return vs
			},
			want: "W:0@0 R:50@3",
		},
		{
			name: "both epochs set",
			vs: func() *VarState {
				vs := NewVarState()
				vs.W = epoch.NewEpoch(5, 100)
				vs.SetReadEpoch(epoch.NewEpoch(3, 50))
				return vs
			},
			want: "W:100@5 R:50@3",
		},
		{
			name: "same thread",
			vs: func() *VarState {
				vs := NewVarState()
				vs.W = epoch.NewEpoch(7, 200)
				vs.SetReadEpoch(epoch.NewEpoch(7, 199))
				return vs
			},
			want: "W:200@7 R:199@7",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vs := tt.vs()
			got := vs.String()
			if got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
			t.Logf("String() output: %s", got)
		})
	}
}

// TestVarStateZeroValue verifies that a zero VarState (no initialization) works correctly.
func TestVarStateZeroValue(t *testing.T) {
	var vs VarState // Zero value, not initialized with NewVarState().

	// Should be equivalent to NewVarState().
	if vs.W != 0 {
		t.Errorf("Zero VarState.W = %v, want 0", vs.W)
	}
	if vs.GetReadEpoch() != 0 {
		t.Errorf("Zero VarState.GetReadEpoch() = %v, want 0", vs.GetReadEpoch())
	}
	if vs.IsPromoted() {
		t.Error("Zero VarState should not be promoted")
	}

	// String should work on zero value.
	str := vs.String()
	if str != "W:0@0 R:0@0" {
		t.Errorf("Zero VarState.String() = %q, want %q", str, "W:0@0 R:0@0")
	}

	t.Logf("Zero VarState works correctly: %s", vs.String())
}

// TestVarStateResetNoAlloc verifies that Reset() does not allocate.
func TestVarStateResetNoAlloc(t *testing.T) {
	vs := NewVarState()
	vs.W = epoch.NewEpoch(5, 100)
	vs.SetReadEpoch(epoch.NewEpoch(3, 50))

	// Measure allocations during Reset().
	allocs := testing.AllocsPerRun(1000, func() {
		vs.Reset()
	})

	if allocs > 0 {
		t.Errorf("Reset() allocated %.2f times per call, want 0", allocs)
	}

	t.Logf("Reset() allocations: %.2f (correct - zero allocations)", allocs)
}

// BenchmarkVarStateNew benchmarks the cost of NewVarState().
func BenchmarkVarStateNew(b *testing.B) {
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = NewVarState()
	}
}

// BenchmarkVarStateReset benchmarks the cost of Reset().
// Target: <2ns/op, 0 allocs/op.
func BenchmarkVarStateReset(b *testing.B) {
	vs := NewVarState()
	vs.W = epoch.NewEpoch(5, 100)
	vs.SetReadEpoch(epoch.NewEpoch(3, 50))

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		vs.Reset()
	}
}

// BenchmarkVarStateReadWrite benchmarks the cost of setting W and readEpoch.
func BenchmarkVarStateReadWrite(b *testing.B) {
	vs := NewVarState()

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		vs.W = epoch.NewEpoch(5, uint32(i))
		vs.SetReadEpoch(epoch.NewEpoch(3, uint32(i)))
	}
}

// BenchmarkVarStateString benchmarks the cost of String() formatting.
// This is not on hot path, but good to know the cost.
func BenchmarkVarStateString(b *testing.B) {
	vs := NewVarState()
	vs.W = epoch.NewEpoch(5, 100)
	vs.SetReadEpoch(epoch.NewEpoch(3, 50))

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = vs.String()
	}
}
