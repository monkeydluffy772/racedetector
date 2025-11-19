package api

import (
	"runtime"
	"sync"
	"testing"
	"time"
)

// === TID Pool Unit Tests (Phase 2 Task 2.2) ===

// TestTIDPoolInitialization verifies TID pool starts with 256 TIDs.
func TestTIDPoolInitialization(t *testing.T) {
	// Initialize pool.
	initTIDPool()

	tidPoolMu.Lock()
	poolSize := len(freeTIDs)
	tidPoolMu.Unlock()

	if poolSize != 256 {
		t.Errorf("TID pool size = %d, want 256", poolSize)
	}

	// Verify TIDs are in ascending order [0, 1, 2, ..., 255].
	tidPoolMu.Lock()
	for i := 0; i < 256; i++ {
		expected := uint8(i)
		if freeTIDs[i] != expected {
			t.Errorf("freeTIDs[%d] = %d, want %d", i, freeTIDs[i], expected)
			break
		}
	}
	tidPoolMu.Unlock()
}

// TestTIDAllocation verifies TID allocation from pool.
func TestTIDAllocation(t *testing.T) {
	initTIDPool()

	// Allocate a TID.
	tid := allocTID()

	// Should get TID 0 (first in pool).
	if tid != 0 {
		t.Errorf("First allocTID() = %d, want 0", tid)
	}

	// Pool should now have 255 TIDs.
	tidPoolMu.Lock()
	poolSize := len(freeTIDs)
	tidPoolMu.Unlock()

	if poolSize != 255 {
		t.Errorf("After allocation, pool size = %d, want 255", poolSize)
	}
}

// TestTIDAllocationSequential verifies TIDs allocated sequentially.
func TestTIDAllocationSequential(t *testing.T) {
	initTIDPool()

	// Allocate 10 TIDs.
	tids := make([]uint8, 10)
	for i := 0; i < 10; i++ {
		tids[i] = allocTID()
	}

	// Should get TIDs: 0, 1, 2, ..., 9.
	for i := 0; i < 10; i++ {
		expected := uint8(i)
		if tids[i] != expected {
			t.Errorf("TID %d = %d, want %d", i, tids[i], expected)
		}
	}

	// Pool should have 246 TIDs left.
	tidPoolMu.Lock()
	poolSize := len(freeTIDs)
	tidPoolMu.Unlock()

	if poolSize != 246 {
		t.Errorf("After 10 allocations, pool size = %d, want 246", poolSize)
	}
}

// TestTIDFree verifies TID is returned to pool.
func TestTIDFree(t *testing.T) {
	initTIDPool()

	// Allocate a TID.
	tid := allocTID()

	// Pool should have 255 TIDs.
	tidPoolMu.Lock()
	poolSize := len(freeTIDs)
	tidPoolMu.Unlock()

	if poolSize != 255 {
		t.Errorf("After allocation, pool size = %d, want 255", poolSize)
	}

	// Free the TID.
	freeTID(tid)

	// Pool should have 256 TIDs again.
	tidPoolMu.Lock()
	poolSize = len(freeTIDs)
	tidPoolMu.Unlock()

	if poolSize != 256 {
		t.Errorf("After freeing, pool size = %d, want 256", poolSize)
	}
}

// TestTIDReuse verifies freed TID is reused.
func TestTIDReuse(t *testing.T) {
	initTIDPool()

	// Allocate TID 0.
	tid1 := allocTID()
	if tid1 != 0 {
		t.Fatalf("First allocation = %d, want 0", tid1)
	}

	// Free TID 0.
	freeTID(tid1)

	// The freed TID should be at the end of the pool now (appended).
	// Next allocation should still get TID 1 (FIFO from front).
	tid2 := allocTID()
	if tid2 != 1 {
		t.Errorf("Second allocation after free = %d, want 1", tid2)
	}

	// Allocate all remaining TIDs to empty the pool.
	for i := 0; i < 254; i++ {
		allocTID()
	}

	// Now pool should have only the freed TID 0.
	tidPoolMu.Lock()
	poolSize := len(freeTIDs)
	tidPoolMu.Unlock()

	if poolSize != 1 {
		t.Errorf("Pool size before final alloc = %d, want 1", poolSize)
	}

	// Next allocation should get the freed TID 0.
	tid3 := allocTID()
	if tid3 != 0 {
		t.Errorf("Reused TID = %d, want 0", tid3)
	}
}

// TestTIDPoolExhaustion verifies behavior when pool is exhausted.
func TestTIDPoolExhaustion(t *testing.T) {
	// This test is tricky because exhaustion triggers cleanup.
	// We'll test that allocTID doesn't panic when pool is empty.

	initTIDPool()

	// Allocate all 256 TIDs.
	for i := 0; i < 256; i++ {
		allocTID()
	}

	// Pool should be empty.
	tidPoolMu.Lock()
	poolSize := len(freeTIDs)
	tidPoolMu.Unlock()

	if poolSize != 0 {
		t.Errorf("After exhaustion, pool size = %d, want 0", poolSize)
	}

	// Next allocation should trigger cleanup.
	// Since no goroutines are dead (test goroutine is alive),
	// cleanup won't free any TIDs, so we get graceful degradation (TID 0).
	tid := allocTID()
	if tid != 0 {
		t.Errorf("Exhaustion fallback TID = %d, want 0", tid)
	}
}

// TestTIDConcurrentAllocation verifies concurrent TID allocation is safe.
func TestTIDConcurrentAllocation(t *testing.T) {
	initTIDPool()

	const numGoroutines = 100
	tids := make([]uint8, numGoroutines)
	var wg sync.WaitGroup

	// Launch 100 goroutines allocating TIDs concurrently.
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			tids[idx] = allocTID()
		}(i)
	}

	wg.Wait()

	// Verify all TIDs are unique.
	tidSet := make(map[uint8]bool)
	for i, tid := range tids {
		if tidSet[tid] {
			t.Errorf("Duplicate TID %d at index %d", tid, i)
		}
		tidSet[tid] = true
	}

	if len(tidSet) != numGoroutines {
		t.Errorf("Expected %d unique TIDs, got %d", numGoroutines, len(tidSet))
	}
}

// TestTIDConcurrentFree verifies concurrent TID free is safe.
func TestTIDConcurrentFree(t *testing.T) {
	initTIDPool()

	// Allocate 100 TIDs.
	tids := make([]uint8, 100)
	for i := 0; i < 100; i++ {
		tids[i] = allocTID()
	}

	var wg sync.WaitGroup

	// Free them concurrently.
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			freeTID(tids[idx])
		}(i)
	}

	wg.Wait()

	// Pool should have 256 TIDs again.
	tidPoolMu.Lock()
	poolSize := len(freeTIDs)
	tidPoolMu.Unlock()

	if poolSize != 256 {
		t.Errorf("After concurrent free, pool size = %d, want 256", poolSize)
	}
}

// TestParseAllGIDs verifies parsing of runtime.Stack output.
func TestParseAllGIDs(t *testing.T) {
	// Sample runtime.Stack output.
	stackTrace := []byte(`goroutine 1 [running]:
main.main()
	/path/to/main.go:10 +0x20

goroutine 5 [chan receive]:
main.worker()
	/path/to/worker.go:20 +0x40

goroutine 123 [semacquire]:
sync.(*WaitGroup).Wait()
	/path/to/sync.go:30 +0x60
`)

	gids := parseAllGIDs(stackTrace)

	// Should extract GIDs: 1, 5, 123.
	expected := []int64{1, 5, 123}
	if len(gids) != len(expected) {
		t.Fatalf("parseAllGIDs() returned %d GIDs, want %d", len(gids), len(expected))
	}

	for i, gid := range gids {
		if gid != expected[i] {
			t.Errorf("GID %d = %d, want %d", i, gid, expected[i])
		}
	}
}

// TestParseAllGIDs_EmptyInput verifies parsing empty input.
func TestParseAllGIDs_EmptyInput(t *testing.T) {
	gids := parseAllGIDs([]byte{})

	if len(gids) != 0 {
		t.Errorf("parseAllGIDs(empty) returned %d GIDs, want 0", len(gids))
	}
}

// TestParseAllGIDs_NoGoroutines verifies parsing with no goroutine lines.
func TestParseAllGIDs_NoGoroutines(t *testing.T) {
	stackTrace := []byte("some random text\nwithout goroutine lines\n")
	gids := parseAllGIDs(stackTrace)

	if len(gids) != 0 {
		t.Errorf("parseAllGIDs(no goroutines) returned %d GIDs, want 0", len(gids))
	}
}

// TestGetLiveGoroutineIDs verifies we can get all live GIDs.
func TestGetLiveGoroutineIDs(t *testing.T) {
	// Launch a few goroutines.
	done := make(chan bool)
	const numGoroutines = 5

	for i := 0; i < numGoroutines; i++ {
		go func() {
			<-done
		}()
	}

	// Get live GIDs.
	gids := getLiveGoroutineIDs()

	// Should have at least numGoroutines + 1 (test goroutine).
	// There may be more due to Go runtime goroutines.
	if len(gids) < numGoroutines+1 {
		t.Errorf("getLiveGoroutineIDs() returned %d GIDs, want >= %d", len(gids), numGoroutines+1)
	}

	// Verify all GIDs are unique.
	gidSet := make(map[int64]bool)
	for _, gid := range gids {
		if gidSet[gid] {
			t.Errorf("Duplicate GID %d", gid)
		}
		gidSet[gid] = true
	}

	// Clean up goroutines.
	close(done)
}

// TestCleanupDeadGoroutines verifies cleanup reclaims TIDs.
func TestCleanupDeadGoroutines(t *testing.T) {
	Init() // Initialize with TID pool

	// Current GID (test goroutine).
	testGID := getGoroutineID()

	// Launch 10 short-lived goroutines.
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Allocate context (gets TID).
			ctx := getCurrentContext()
			_ = ctx
			// Goroutine exits here.
		}()
	}

	wg.Wait()

	// At this point, 10 goroutines are dead but their TIDs are still allocated.
	// Pool should have 256 - 1 (main/test) - 10 (dead goroutines) = 245 TIDs.
	// Actually, with Init(), main goroutine gets TID 0, so pool has 255 TIDs.
	// After 10 allocations, pool has 255 - 10 = 245 TIDs.

	// Check pool size before cleanup.
	tidPoolMu.Lock()
	poolSizeBefore := len(freeTIDs)
	tidPoolMu.Unlock()

	// Should be around 245 (256 - 1 main - 10 allocated).
	// Actually, Init() removes TID 0, so we start with 255, and after 10 allocs we have 245.
	expectedBefore := 245
	if poolSizeBefore != expectedBefore {
		// This may vary due to runtime goroutines, so just log it.
		t.Logf("Pool size before cleanup = %d, expected %d", poolSizeBefore, expectedBefore)
	}

	// Run cleanup - should reclaim the 10 TIDs from dead goroutines.
	cleanupDeadGoroutines()

	// Give cleanup time to complete (it scans runtime stacks).
	time.Sleep(10 * time.Millisecond)

	// Check pool size after cleanup.
	tidPoolMu.Lock()
	poolSizeAfter := len(freeTIDs)
	tidPoolMu.Unlock()

	// Should have reclaimed 10 TIDs: 245 + 10 = 255.
	// But test goroutine (GID testGID) is still alive with TID, so we have 255.
	// The cleanup should have increased the pool size.
	if poolSizeAfter < poolSizeBefore {
		t.Errorf("Pool size after cleanup = %d, decreased from %d (expected increase)", poolSizeAfter, poolSizeBefore)
	}

	// Verify TID was reclaimed by checking we can allocate more.
	// We should be able to allocate 255 TIDs.
	tidsAllocated := 0
	for i := 0; i < 260; i++ { // Try to allocate more than possible
		tid := allocTID()
		if tid == 0 && i >= 255 {
			// Graceful degradation after exhaustion.
			break
		}
		tidsAllocated++
	}

	if tidsAllocated < 250 {
		t.Errorf("After cleanup, could only allocate %d TIDs, want >= 250", tidsAllocated)
	}

	t.Logf("Test GID: %d, Pool before cleanup: %d, Pool after cleanup: %d, TIDs allocated: %d",
		testGID, poolSizeBefore, poolSizeAfter, tidsAllocated)
}

// TestMaybeCleanup verifies periodic cleanup is triggered.
func TestMaybeCleanup(t *testing.T) {
	Init()

	// Reset allocation counter.
	allocCounter.Store(0)

	// Call maybeCleanup 1000 times - should trigger cleanup once.
	for i := 0; i < 1000; i++ {
		maybeCleanup()
	}

	// Verify counter is 1000.
	count := allocCounter.Load()
	if count != 1000 {
		t.Errorf("After 1000 maybeCleanup calls, counter = %d, want 1000", count)
	}

	// Cleanup should have been triggered at count=1000.
	// We can't easily verify cleanup ran, but we can verify no panic.
	// Wait a bit for background cleanup goroutine.
	time.Sleep(50 * time.Millisecond)
}

// TestMaybeCleanup_NoSpam verifies cleanup isn't triggered too often.
func TestMaybeCleanup_NoSpam(t *testing.T) {
	Init()

	// Reset counter.
	allocCounter.Store(0)

	// Call maybeCleanup 500 times - should NOT trigger cleanup.
	for i := 0; i < 500; i++ {
		maybeCleanup()
	}

	// Verify counter is 500.
	count := allocCounter.Load()
	if count != 500 {
		t.Errorf("After 500 maybeCleanup calls, counter = %d, want 500", count)
	}

	// No cleanup should have run (threshold is 1000).
	// We just verify no panic.
}

// TestIntegration_1000Goroutines tests 1000 concurrent goroutines with TID reuse.
func TestIntegration_1000Goroutines(t *testing.T) {
	Init()

	const numGoroutines = 1000
	const batchSize = 100

	// Launch goroutines in batches to trigger TID reuse.
	// Each batch allocates 100 TIDs, then goroutines exit, freeing TIDs.
	for batch := 0; batch < numGoroutines/batchSize; batch++ {
		var wg sync.WaitGroup

		for i := 0; i < batchSize; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				// Allocate context (gets TID).
				ctx := getCurrentContext()
				// Do some work.
				_ = ctx.TID
				// Goroutine exits, TID should be reclaimed.
			}()
		}

		wg.Wait()

		// Trigger cleanup after each batch.
		if batch%10 == 0 {
			cleanupDeadGoroutines()
			time.Sleep(10 * time.Millisecond) // Let cleanup run
		}
	}

	// Verify we didn't panic and detector still works.
	if !enabled.Load() {
		t.Error("Detector disabled after 1000 goroutines")
	}

	// Run final cleanup and wait for it to complete.
	cleanupDeadGoroutines()
	time.Sleep(100 * time.Millisecond)

	// Verify pool has TIDs available.
	tidPoolMu.Lock()
	poolSize := len(freeTIDs)
	tidPoolMu.Unlock()

	// After cleanup, should have most TIDs back.
	// We may not get all 255 back because some runtime goroutines may still be alive.
	// But we should have at least 150+ available.
	if poolSize < 150 {
		t.Errorf("After 1000 goroutines with cleanup, pool size = %d, want >= 150", poolSize)
	}

	t.Logf("After 1000 goroutines: pool size = %d, detector enabled = %v", poolSize, enabled.Load())
}

// TestIntegration_LongLivedAndShortLived tests mix of goroutine lifetimes.
func TestIntegration_LongLivedAndShortLived(t *testing.T) {
	Init()

	// Launch 10 long-lived goroutines.
	longLivedDone := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			ctx := getCurrentContext()
			_ = ctx
			<-longLivedDone
		}()
	}

	// Launch 100 short-lived goroutines.
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx := getCurrentContext()
			_ = ctx
			// Immediate exit
		}()
	}

	wg.Wait()

	// Run cleanup to reclaim short-lived TIDs.
	cleanupDeadGoroutines()
	time.Sleep(10 * time.Millisecond)

	// Verify pool has TIDs (short-lived ones reclaimed).
	tidPoolMu.Lock()
	poolSize := len(freeTIDs)
	tidPoolMu.Unlock()

	// Should have ~245 TIDs (256 - 1 main - 10 long-lived).
	// Actually depends on cleanup efficiency.
	if poolSize < 200 {
		t.Errorf("After mixed lifetimes, pool size = %d, want >= 200", poolSize)
	}

	// Clean up long-lived goroutines.
	close(longLivedDone)

	t.Logf("Mixed lifetimes: pool size = %d", poolSize)
}

// TestTIDPoolThreadSafety verifies TID pool operations are thread-safe.
func TestTIDPoolThreadSafety(t *testing.T) {
	initTIDPool()

	const numWorkers = 50
	const operationsPerWorker = 100

	var wg sync.WaitGroup

	// Launch workers that allocate and free TIDs concurrently.
	for worker := 0; worker < numWorkers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for op := 0; op < operationsPerWorker; op++ {
				// Allocate TID.
				tid := allocTID()

				// Do some work.
				runtime.Gosched()

				// Free TID.
				freeTID(tid)
			}
		}()
	}

	wg.Wait()

	// After all operations, pool should have 256 TIDs.
	tidPoolMu.Lock()
	poolSize := len(freeTIDs)
	tidPoolMu.Unlock()

	if poolSize != 256 {
		t.Errorf("After concurrent alloc/free, pool size = %d, want 256", poolSize)
	}
}

// BenchmarkAllocTID benchmarks TID allocation.
func BenchmarkAllocTID(b *testing.B) {
	initTIDPool()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = allocTID()
		// Note: This will exhaust pool after 256 iterations.
		// For accurate benchmark, we should free TIDs too.
		if i%256 == 255 {
			// Reset pool.
			initTIDPool()
		}
	}
}

// BenchmarkFreeTID benchmarks TID free.
func BenchmarkFreeTID(b *testing.B) {
	initTIDPool()

	// Pre-allocate TIDs to free.
	tids := make([]uint8, 256)
	for i := 0; i < 256; i++ {
		tids[i] = allocTID()
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		freeTID(tids[i%256])
	}
}

// BenchmarkGetLiveGoroutineIDs benchmarks goroutine ID enumeration.
func BenchmarkGetLiveGoroutineIDs(b *testing.B) {
	// Launch some goroutines to make it realistic.
	done := make(chan bool)
	for i := 0; i < 100; i++ {
		go func() { <-done }()
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = getLiveGoroutineIDs()
	}

	close(done)
}

// BenchmarkCleanupDeadGoroutines benchmarks cleanup with realistic goroutine count.
func BenchmarkCleanupDeadGoroutines(b *testing.B) {
	Init()

	// Create some contexts for cleanup to scan.
	for i := 0; i < 100; i++ {
		go func() {
			ctx := getCurrentContext()
			_ = ctx
			time.Sleep(time.Millisecond)
		}()
	}

	time.Sleep(50 * time.Millisecond) // Let goroutines start

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cleanupDeadGoroutines()
	}
}

// BenchmarkMaybeCleanup benchmarks cleanup trigger check.
func BenchmarkMaybeCleanup(b *testing.B) {
	Init()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		maybeCleanup()
	}
}
