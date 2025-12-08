// Copyright 2025 The racedetector Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Goroutine ID extraction fallback for non-amd64 architectures.
//
// This file provides the slow path for getGoroutineID() on architectures
// where we haven't implemented assembly optimizations yet.
//
// Currently supported (with assembly fast path):
//   - amd64 (x86-64)
//
// Fallback architectures (using this slow path):
//   - arm64, arm, 386, ppc64le, s390x, mips, wasm, etc.
//
// Performance: ~4.7µs per call (runtime.Stack parsing).
//
// Future: Implement assembly fast paths for arm64 and other common architectures.

// NOTE: v0.1.0 - Using this fallback on ALL platforms (assembly disabled for stability).
// The assembly optimization (goid_amd64.go) is disabled via build tag and will be
// re-enabled in v0.4.0 after thorough testing.

//nolint:revive // Package name 'api' is intentional - this is the public API package
package api

import "runtime"

// getGoroutineIDFast is the fallback implementation for non-amd64 architectures.
//
// On architectures without assembly optimization, we simply fall back to
// the slow path using runtime.Stack parsing. The name "Fast" is kept for
// API consistency, but this is actually the slow implementation.
//
// Performance: ~4.7µs per call (same as getGoroutineIDSlow).
//
// Future optimization paths:
//   - arm64: Similar TLS access, goid at same offset (likely)
//   - arm: Different offset, needs investigation
//   - 386: Different TLS access mechanism
//
// Returns:
//   - int64: Goroutine ID (unique per goroutine)
func getGoroutineIDFast() int64 {
	return getGoroutineIDSlow()
}

// getGoroutineIDSlow is the runtime.Stack parsing implementation.
//
// This is the only implementation available on non-amd64 architectures.
// It's SLOW (~4.7µs) but portable and reliable across all platforms.
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
