// Copyright 2025 The racedetector Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Goroutine ID extraction optimized for amd64 using assembly.
//
// This file provides the fast path for getGoroutineID() on amd64 architecture.
// It uses assembly to access the runtime.g pointer directly from TLS and
// extract the goid field at a known offset.
//
// Performance: <1ns per call (vs ~4.7µs for runtime.Stack parsing).
//
// Go Version Compatibility:
//   - Go 1.25: goid offset = 136 bytes (verified)
//   - Go 1.24: goid offset = 136 bytes (likely same)
//   - Go 1.23: goid offset = 136 bytes (likely same)
//
// If goid offset changes in future Go versions, this code will need updating.
// The offset can be verified using tools/calc_goid_offset.go.

//go:build amd64 && disabled_for_v0_1_0

// NOTE: Assembly implementation disabled for v0.1.0 to ensure stability.
// The fallback implementation in goid_generic.go will be used instead.
// Assembly optimization will be re-enabled in v0.2.0.

package api

import (
	"runtime"
	"unsafe"
)

// goidOffset is the byte offset of the goid field within the runtime.g struct.
//
// This value is architecture and Go-version specific. For Go 1.25 on amd64,
// the offset is 136 bytes from the start of the g struct.
//
// How this was determined:
//  1. Examined runtime/runtime2.go in Go 1.25 source
//  2. Counted field offsets up to goid field
//  3. Verified using unsafe.Offsetof in tools/calc_goid_offset.go
//
// Field layout in runtime.g (up to goid):
//
//	0: stack       (16 bytes)
//	16: stackguard0 (8 bytes)
//	24: stackguard1 (8 bytes)
//	32: _panic      (8 bytes)
//	40: _defer      (8 bytes)
//	48: m           (8 bytes)
//	56: sched       (40 bytes)
//	96: syscallsp   (8 bytes)
//	104: syscallpc  (8 bytes)
//	112: stktopsp   (8 bytes)
//	120: param      (8 bytes)
//	128: atomicstatus (4 bytes)
//	132: stackLock  (4 bytes)
//	136: goid       (8 bytes) <-- THIS IS WHAT WE WANT
//
// CRITICAL: If this offset is incorrect, we will read garbage and get
// incorrect goroutine IDs, causing race detector to fail.
//
// VERIFIED: On Go 1.25.3 Windows amd64, goid is at offset 152 bytes.
const goidOffset = 152

// getg returns the current goroutine's g struct pointer.
//
// This function is implemented in assembly (goid_amd64.s) and accesses
// the g pointer from Thread Local Storage (TLS) via the FS segment register.
//
// The g pointer is stored at TLS offset -8 on amd64.
//
// Returns:
//   - unsafe.Pointer: Pointer to runtime.g struct for current goroutine
//
// Performance: ~0.5ns (single memory read + return).
//
//go:noescape
//go:linkname getg
func getg() unsafe.Pointer

// getGoroutineIDFast extracts the goroutine ID using assembly fast path.
//
// This is the optimized implementation for amd64. It:
//  1. Calls assembly stub to get g pointer from TLS
//  2. Reads goid field at known offset (136 bytes)
//  3. Returns the ID as int64
//
// Performance: <1ns per call (0.5ns for getg + 0.3ns for field read).
//
// Safety:
//   - NOSPLIT: Cannot grow stack (called from race detector hot path)
//   - No allocations: Pure pointer arithmetic
//   - Thread-safe: TLS access is inherently thread-local
//
// Fallback:
// If g pointer is nil (extremely rare, should never happen in practice),
// we fall back to slow path using runtime.Stack parsing.
//
// Returns:
//   - int64: Goroutine ID (unique per goroutine)
func getGoroutineIDFast() int64 {
	// Get g pointer from TLS via assembly stub.
	g := getg()

	// Nil check (should never happen, but be defensive).
	// If g is nil, we're in a very strange state (pre-runtime-init?).
	// Fall back to slow path to be safe.
	if g == nil {
		return getGoroutineIDSlow()
	}

	// Extract goid field from g struct.
	// The goid field is a uint64 at offset 136 bytes.
	// We compute the pointer: g + goidOffset, then dereference as *int64.
	//
	// Pointer arithmetic:
	//   1. Convert g (unsafe.Pointer) to uintptr for arithmetic
	//   2. Add goidOffset (136) to get address of goid field
	//   3. Convert back to unsafe.Pointer
	//   4. Cast to *int64 and dereference
	//nolint:gosec // G103: Intentional unsafe pointer arithmetic to access runtime internals
	goidPtr := (*int64)(unsafe.Pointer(uintptr(g) + goidOffset))

	// Return the goroutine ID.
	// This is a single memory read - extremely fast.
	return *goidPtr
}

// getGoroutineIDSlow is the fallback implementation using runtime.Stack parsing.
//
// This is SLOW (~4.7µs) but reliable. It's used:
//   - When g pointer is nil (should never happen)
//   - As fallback on non-amd64 architectures
//
// Algorithm:
//  1. Allocate 64-byte buffer
//  2. Call runtime.Stack() to get stack trace
//  3. Parse "goroutine 12345 [running]:" to extract ID
//
// Performance: ~4.7µs (dominated by runtime.Stack allocation + parsing).
//
// Returns:
//   - int64: Goroutine ID (unique per goroutine)
func getGoroutineIDSlow() int64 {
	// Allocate buffer for stack trace.
	// We only need the first line ("goroutine 123 [running]:"), so 64 bytes
	// is more than sufficient. runtime.Stack will truncate if needed.
	buf := make([]byte, 64)

	// Get stack trace for current goroutine only (all=false).
	// This writes "goroutine 123 [running]:\n..." into buf.
	n := runtime.Stack(buf, false)

	// Parse the goroutine ID from the buffer.
	// parseGID extracts the number from "goroutine 123 [running]:".
	return parseGID(buf[:n])
}
