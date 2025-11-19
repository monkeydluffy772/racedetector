package shadowmem

import "sync"

// ShadowMemory is the global shadow memory map that stores VarState cells
// for instrumented memory locations.
//
// The shadow memory maps each memory address to a VarState cell that tracks
// the last write and read epochs for that location. This is the foundation
// of the FastTrack race detection algorithm.
//
// Implementation: For MVP (Phase 1), we use sync.Map which provides:
//   - Thread-safe concurrent access without external locking
//   - Optimized for read-heavy workloads (common case for race detection)
//   - Good performance for frequently accessed keys (hot addresses)
//
// The sync.Map stores entries as map[uintptr]*VarState, where:
//   - Key: Memory address (uintptr) of the variable
//   - Value: Pointer to VarState cell tracking access epochs
//
// Performance Characteristics (sync.Map):
//   - Load (cache hit): ~5-10ns (optimized read path)
//   - Load (cache miss): ~20-50ns (requires map lookup)
//   - LoadOrStore (hit): ~10-15ns (similar to Load)
//   - LoadOrStore (miss): ~50-100ns (allocates new VarState + stores)
//
// Memory Granularity: For MVP, we track at exact address granularity.
// In Phase 5, we may optimize to 8-byte alignment for better compression.
//
// Thread Safety: All operations are thread-safe. sync.Map handles
// concurrent access internally without requiring external locks.
//
// Future Optimization (Phase 5): Consider custom lock-free hashmap
// with address compression for better cache locality and lower memory overhead.
type ShadowMemory struct {
	cells sync.Map // map[uintptr]*VarState - shadow cells indexed by memory address
}

// NewShadowMemory creates a new empty shadow memory map.
//
// The returned ShadowMemory is ready to use and safe for concurrent access
// by multiple goroutines.
//
// Example:
//
//	sm := NewShadowMemory()
//	vs := sm.GetOrCreate(0x1234)  // Get or allocate shadow cell
//	vs.W = epoch.NewEpoch(1, 10)  // Record write access
func NewShadowMemory() *ShadowMemory {
	return &ShadowMemory{}
}

// GetOrCreate retrieves the VarState for the given address, creating it if needed.
//
// This is the primary method for accessing shadow memory cells. It implements
// the "get or allocate" pattern required by the FastTrack algorithm.
//
// Parameters:
//   - addr: Memory address to look up or create shadow cell for
//
// Returns:
//   - *VarState: Pointer to the shadow cell (never nil)
//
// Behavior:
//   - If shadow cell exists: Returns existing VarState (fast path, no allocation)
//   - If shadow cell missing: Allocates new VarState, stores it, returns it
//
// Thread Safety: Safe for concurrent calls. If multiple goroutines call
// GetOrCreate for the same address simultaneously, only one VarState is created
// and all callers receive the same instance.
//
// Performance:
//   - Hit (existing cell): <10ns (sync.Map.Load fast path)
//   - Miss (new cell): <50ns (sync.Map.LoadOrStore + VarState allocation)
//
// The fast path (hit) is critical because it's called on every instrumented
// memory access after the first access to an address.
//
//go:nosplit
func (sm *ShadowMemory) GetOrCreate(addr uintptr) *VarState {
	// Fast path: Try to load existing cell.
	if val, ok := sm.cells.Load(addr); ok {
		return val.(*VarState)
	}

	// Slow path: Allocate new cell and store atomically.
	// LoadOrStore ensures only one VarState is created even if multiple
	// goroutines race to create the cell for this address.
	vs := NewVarState()
	actual, _ := sm.cells.LoadOrStore(addr, vs)
	return actual.(*VarState)
}

// Get retrieves the VarState for the given address if it exists.
//
// This method is used when the caller needs to check if a shadow cell exists
// without creating one. This is less common than GetOrCreate but useful for
// certain optimization paths and debugging.
//
// Parameters:
//   - addr: Memory address to look up
//
// Returns:
//   - *VarState: Pointer to shadow cell if it exists, nil otherwise
//
// Behavior:
//   - If shadow cell exists: Returns existing VarState
//   - If shadow cell missing: Returns nil (does NOT create a new cell)
//
// Thread Safety: Safe for concurrent calls.
//
// Performance: <10ns (sync.Map.Load operation)
//
// Zero Allocations: This method performs zero allocations when the entry
// exists OR when the entry doesn't exist (returns nil).
//
//go:nosplit
func (sm *ShadowMemory) Get(addr uintptr) *VarState {
	val, ok := sm.cells.Load(addr)
	if !ok {
		return nil
	}
	return val.(*VarState)
}

// Reset clears all shadow memory cells.
//
// This is primarily used for testing and reinitialization scenarios.
// After Reset(), all previously tracked addresses are forgotten and
// the shadow memory is empty.
//
// Thread Safety: NOT safe for concurrent access during Reset().
// The caller must ensure no other goroutines are accessing the ShadowMemory
// during Reset() (typically used only in test setup/teardown).
//
// Implementation Note: sync.Map doesn't provide a Clear() method in Go 1.21.
// We achieve reset by replacing the entire sync.Map with a new instance.
// This allows the garbage collector to reclaim the old map.
//
// Performance: O(1) time complexity (just pointer assignment).
// The old map will be garbage collected when no references remain.
//
// Example:
//
//	sm.Reset()  // Clear all shadow cells
//	vs := sm.Get(0x1234)  // Returns nil - address forgotten
func (sm *ShadowMemory) Reset() {
	sm.cells = sync.Map{}
}
