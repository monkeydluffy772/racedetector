// Copyright 2025 The racedetector Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Assembly stub to extract goroutine ID on amd64 architecture.
//
// This file provides ultra-fast access to the current goroutine's ID by
// reading the g pointer from Thread Local Storage (TLS) and extracting
// the goid field.
//
// Performance: <1ns per call (vs ~4.7µs for runtime.Stack parsing).
//
// Architecture: amd64 only. Other architectures use fallback in goid_generic.go.

//go:build amd64 && disabled_for_v0_1_0

// NOTE: Disabled for v0.1.0 - using fallback implementation

#include "textflag.h"

// TLS access macros (from runtime/go_tls.h)
// get_tls(r) loads TLS base address into register r
// g(r) accesses the g pointer at offset 0 from TLS base
#ifdef GOARCH_amd64
#define	get_tls(r)	MOVQ TLS, r
#define	g(r)	0(r)(TLS*1)
#endif

// func getg() unsafe.Pointer
//
// Returns the current goroutine's g pointer.
//
// The g pointer is stored in Thread Local Storage (TLS).
// We use the Go assembler's TLS pseudo-register to access it.
//
// Note: This is NOT the g.goid field yet - just the g pointer.
// The caller will extract goid from this pointer at offset 136.
//
// NOSPLIT is critical: we must not trigger stack growth, as this function
// is called from the race detector hot path which itself may be called
// during stack growth.
//
// Returns:
//   - unsafe.Pointer: Current goroutine's g struct pointer
TEXT ·getg(SB), NOSPLIT, $0-8
	// Load g pointer using the same pattern as runtime.
	// This uses the get_tls(r) and g(r) macros from go_tls.h.
	get_tls(CX)           // Load TLS base into CX
	MOVQ g(CX), AX        // Load g pointer from TLS

	// Store g pointer in return value.
	MOVQ AX, ret+0(FP)

	RET
