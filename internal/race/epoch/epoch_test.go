package epoch

import (
	"testing"

	"github.com/kolkov/racedetector/internal/race/vectorclock"
)

// TestNewEpoch tests epoch creation and encoding.
func TestNewEpoch(t *testing.T) {
	tests := []struct {
		name      string
		tid       uint16
		clock     uint64
		wantEpoch uint64
	}{
		{
			name:      "zero epoch",
			tid:       0,
			clock:     0,
			wantEpoch: 0x0000000000000000,
		},
		{
			name:      "tid only",
			tid:       5,
			clock:     0,
			wantEpoch: 0x0005000000000000,
		},
		{
			name:      "clock only",
			tid:       0,
			clock:     0x1234,
			wantEpoch: 0x0000000000001234,
		},
		{
			name:      "tid and clock",
			tid:       42,
			clock:     0x123456,
			wantEpoch: 0x002A000000123456,
		},
		{
			name:      "max tid (65535)",
			tid:       65535,
			clock:     0,
			wantEpoch: 0xFFFF000000000000,
		},
		{
			name:      "max clock (48-bit)",
			tid:       0,
			clock:     0x0000FFFFFFFFFFFF,
			wantEpoch: 0x0000FFFFFFFFFFFF,
		},
		{
			name:      "max tid and max clock",
			tid:       65535,
			clock:     0x0000FFFFFFFFFFFF,
			wantEpoch: 0xFFFFFFFFFFFFFFFF,
		},
		{
			name:      "clock overflow (truncation)",
			tid:       1,
			clock:     0xFFFFFFFFFFFFFFFF, // Beyond 48 bits
			wantEpoch: 0x0001FFFFFFFFFFFF, // Should truncate to 48 bits
		},
		{
			name:      "large tid (1000 goroutines)",
			tid:       1000,
			clock:     500000,
			wantEpoch: 0x03E800000007A120,
		},
		{
			name:      "very large clock (1 billion operations)",
			tid:       10,
			clock:     1000000000,
			wantEpoch: 0x000A00003B9ACA00,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NewEpoch(tt.tid, tt.clock)
			if uint64(got) != tt.wantEpoch {
				t.Errorf("NewEpoch(%d, 0x%X) = 0x%X, want 0x%X",
					tt.tid, tt.clock, uint64(got), tt.wantEpoch)
			}
		})
	}
}

// TestEpochDecode tests epoch decoding.
func TestEpochDecode(t *testing.T) {
	tests := []struct {
		name      string
		epoch     Epoch
		wantTID   uint16
		wantClock uint64
	}{
		{
			name:      "zero epoch",
			epoch:     0x0000000000000000,
			wantTID:   0,
			wantClock: 0,
		},
		{
			name:      "tid only",
			epoch:     0x0005000000000000,
			wantTID:   5,
			wantClock: 0,
		},
		{
			name:      "clock only",
			epoch:     0x0000000000001234,
			wantTID:   0,
			wantClock: 0x1234,
		},
		{
			name:      "tid and clock",
			epoch:     0x002A000000123456,
			wantTID:   42,
			wantClock: 0x123456,
		},
		{
			name:      "max tid (65535)",
			epoch:     0xFFFF000000000000,
			wantTID:   65535,
			wantClock: 0,
		},
		{
			name:      "max clock (48-bit)",
			epoch:     0x0000FFFFFFFFFFFF,
			wantTID:   0,
			wantClock: 0x0000FFFFFFFFFFFF,
		},
		{
			name:      "max epoch",
			epoch:     0xFFFFFFFFFFFFFFFF,
			wantTID:   65535,
			wantClock: 0x0000FFFFFFFFFFFF,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotTID, gotClock := tt.epoch.Decode()
			if gotTID != tt.wantTID {
				t.Errorf("Epoch(0x%X).Decode() tid = %d, want %d",
					tt.epoch, gotTID, tt.wantTID)
			}
			if gotClock != tt.wantClock {
				t.Errorf("Epoch(0x%X).Decode() clock = 0x%X, want 0x%X",
					tt.epoch, gotClock, tt.wantClock)
			}
		})
	}
}

// TestEpochRoundTrip tests that NewEpoch and Decode are inverse operations.
func TestEpochRoundTrip(t *testing.T) {
	tests := []struct {
		tid   uint16
		clock uint64
	}{
		{0, 0},
		{1, 100},
		{42, 0x123456},
		{65535, 0x0000FFFFFFFFFFFF},
		{32768, 0x800000000},
		{1000, 1000000000}, // 1000 goroutines, 1 billion operations
	}

	for _, tt := range tests {
		t.Run("roundtrip", func(t *testing.T) {
			epoch := NewEpoch(tt.tid, tt.clock)
			gotTID, gotClock := epoch.Decode()

			// Mask clock to 48 bits for comparison (handles overflow)
			wantClock := tt.clock & ClockMask

			if gotTID != tt.tid {
				t.Errorf("Round-trip TID: got %d, want %d", gotTID, tt.tid)
			}
			if gotClock != wantClock {
				t.Errorf("Round-trip clock: got 0x%X, want 0x%X", gotClock, wantClock)
			}
		})
	}
}

// TestEpochHappensBefore tests the critical happens-before check.
func TestEpochHappensBefore(t *testing.T) {
	tests := []struct {
		name  string
		epoch Epoch
		setup func() *vectorclock.VectorClock
		want  bool
	}{
		{
			name:  "epoch happens-before (clock <)",
			epoch: NewEpoch(3, 42),
			setup: func() *vectorclock.VectorClock {
				vc := vectorclock.New()
				vc.Set(0, 100)
				vc.Set(1, 100)
				vc.Set(2, 100)
				vc.Set(3, 45) // epoch's tid=3, clock=42 < 45
				return vc
			},
			want: true,
		},
		{
			name:  "epoch happens-before (clock ==)",
			epoch: NewEpoch(3, 42),
			setup: func() *vectorclock.VectorClock {
				vc := vectorclock.New()
				vc.Set(0, 100)
				vc.Set(1, 100)
				vc.Set(2, 100)
				vc.Set(3, 42) // epoch's tid=3, clock=42 == 42
				return vc
			},
			want: true,
		},
		{
			name:  "epoch NOT happens-before (clock >)",
			epoch: NewEpoch(3, 42),
			setup: func() *vectorclock.VectorClock {
				vc := vectorclock.New()
				vc.Set(0, 100)
				vc.Set(1, 100)
				vc.Set(2, 100)
				vc.Set(3, 41) // epoch's tid=3, clock=42 > 41
				return vc
			},
			want: false,
		},
		{
			name:  "zero epoch happens-before",
			epoch: NewEpoch(0, 0),
			setup: func() *vectorclock.VectorClock {
				vc := vectorclock.New()
				vc.Set(0, 10)
				return vc
			},
			want: true,
		},
		{
			name:  "max tid happens-before",
			epoch: NewEpoch(65535, 100),
			setup: func() *vectorclock.VectorClock {
				vc := vectorclock.New()
				vc.Set(65535, 200)
				return vc
			},
			want: true,
		},
		{
			name:  "max tid NOT happens-before",
			epoch: NewEpoch(65535, 100),
			setup: func() *vectorclock.VectorClock {
				vc := vectorclock.New()
				vc.Set(65535, 99)
				return vc
			},
			want: false,
		},
		{
			name:  "large tid (1000 goroutines)",
			epoch: NewEpoch(1000, 50000),
			setup: func() *vectorclock.VectorClock {
				vc := vectorclock.New()
				vc.Set(1000, 100000)
				return vc
			},
			want: true,
		},
		{
			name:  "epoch with uninitialized vc entry",
			epoch: NewEpoch(5, 0),
			setup: vectorclock.New, // All zeros
			want:  true,            // 0 <= 0
		},
		{
			name:  "epoch with uninitialized vc entry (non-zero clock)",
			epoch: NewEpoch(5, 1),
			setup: vectorclock.New, // All zeros
			want:  false,           // 1 > 0
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vc := tt.setup()
			got := tt.epoch.HappensBefore(vc)
			if got != tt.want {
				tid, clock := tt.epoch.Decode()
				t.Errorf("Epoch(%d@%d).HappensBefore(vc[%d]=%d) = %v, want %v",
					clock, tid, tid, vc.Get(tid), got, tt.want)
			}
		})
	}
}

// TestEpochSame tests the same-epoch optimization check.
func TestEpochSame(t *testing.T) {
	tests := []struct {
		name string
		e1   Epoch
		e2   Epoch
		want bool
	}{
		{
			name: "identical epochs",
			e1:   NewEpoch(5, 100),
			e2:   NewEpoch(5, 100),
			want: true,
		},
		{
			name: "different tid",
			e1:   NewEpoch(5, 100),
			e2:   NewEpoch(6, 100),
			want: false,
		},
		{
			name: "different clock",
			e1:   NewEpoch(5, 100),
			e2:   NewEpoch(5, 101),
			want: false,
		},
		{
			name: "both zero",
			e1:   NewEpoch(0, 0),
			e2:   NewEpoch(0, 0),
			want: true,
		},
		{
			name: "max epochs identical",
			e1:   NewEpoch(65535, 0x0000FFFFFFFFFFFF),
			e2:   NewEpoch(65535, 0x0000FFFFFFFFFFFF),
			want: true,
		},
		{
			name: "completely different",
			e1:   NewEpoch(1, 100),
			e2:   NewEpoch(200, 50000),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.e1.Same(tt.e2)
			if got != tt.want {
				t.Errorf("Epoch(0x%X).Same(0x%X) = %v, want %v",
					tt.e1, tt.e2, got, tt.want)
			}

			// Test symmetry
			gotReverse := tt.e2.Same(tt.e1)
			if gotReverse != tt.want {
				t.Errorf("Epoch(0x%X).Same(0x%X) = %v, want %v (symmetry check)",
					tt.e2, tt.e1, gotReverse, tt.want)
			}
		})
	}
}

// BenchmarkEpochHappensBefore benchmarks the critical happens-before check.
func BenchmarkEpochHappensBefore(b *testing.B) {
	epoch := NewEpoch(42, 1000)
	vc := vectorclock.New()
	vc.Set(42, 2000)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = epoch.HappensBefore(vc)
	}
}

// BenchmarkEpochDecode benchmarks epoch decoding.
func BenchmarkEpochDecode(b *testing.B) {
	epoch := NewEpoch(42, 0x123456)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = epoch.Decode()
	}
}

// BenchmarkNewEpoch benchmarks epoch creation.
func BenchmarkNewEpoch(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = NewEpoch(42, uint64(i)&ClockMask)
	}
}

// BenchmarkEpochSame benchmarks the same-epoch check.
func BenchmarkEpochSame(b *testing.B) {
	e1 := NewEpoch(42, 1000)
	e2 := NewEpoch(42, 1000)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = e1.Same(e2)
	}
}

// TestEpochString tests the String() method for debugging output.
func TestEpochString(t *testing.T) {
	tests := []struct {
		name  string
		epoch Epoch
		want  string
	}{
		{
			name:  "zero epoch",
			epoch: NewEpoch(0, 0),
			want:  "0@0",
		},
		{
			name:  "simple epoch",
			epoch: NewEpoch(5, 42),
			want:  "42@5",
		},
		{
			name:  "large clock",
			epoch: NewEpoch(3, 123456),
			want:  "123456@3",
		},
		{
			name:  "max tid",
			epoch: NewEpoch(65535, 100),
			want:  "100@65535",
		},
		{
			name:  "max clock (48-bit)",
			epoch: NewEpoch(1, 0x0000FFFFFFFFFFFF),
			want:  "281474976710655@1",
		},
		{
			name:  "large tid (1000 goroutines)",
			epoch: NewEpoch(1000, 500000),
			want:  "500000@1000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.epoch.String()
			if got != tt.want {
				t.Errorf("Epoch(0x%X).String() = %q, want %q",
					tt.epoch, got, tt.want)
			}
		})
	}
}
