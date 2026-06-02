// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package pgnanos is the canonical scan/insert seam between Go time.Time
// and BIGINT epoch-nanosecond columns.
//
// HoloMUSH stores all persistent timestamps as BIGINT representing
// nanoseconds since UNIX epoch (UTC). This package wraps time.Time with
// sql.Scanner + driver.Valuer so the boundary is type-safe and visible
// at every call site:
//
//	var createdAt pgnanos.Time
//	err := pool.QueryRow(ctx, `SELECT created_at FROM x WHERE id=$1`, id).
//	    Scan(&createdAt)
//	t := createdAt.Time()
//
//	_, err = pool.Exec(ctx,
//	    `INSERT INTO x (..., created_at) VALUES (..., $5)`,
//	    ..., pgnanos.From(time.Now()))
//
// See docs/superpowers/specs/2026-05-22-nanosecond-timestamps-design.md
// (INV-STORE-1 through INV-STORE-7) for the spec this package serves.
package pgnanos
