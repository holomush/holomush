// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pgnanos

import (
	"database/sql/driver"
	"fmt"
	"time"
)

// Time is the canonical scan/insert seam for BIGINT-epoch-nanosecond
// columns. Construct via From; read via Time().
type Time time.Time

// From constructs a Time from a time.Time, preserving nanosecond
// precision and normalizing to UTC. Callers MUST NOT have already
// truncated t (INV-STORE-3).
func From(t time.Time) Time { return Time(t.UTC()) }

// Time returns the underlying time.Time in UTC.
func (n Time) Time() time.Time { return time.Time(n) }

// IsZero reports whether the underlying time is the zero value.
func (n Time) IsZero() bool { return time.Time(n).IsZero() }

// Scan implements sql.Scanner. Accepts int64 (the column's native type)
// and treats it as nanoseconds since UNIX epoch (UTC).
func (n *Time) Scan(src any) error {
	switch v := src.(type) {
	case int64:
		*n = Time(time.Unix(0, v).UTC())
		return nil
	case nil:
		*n = Time{}
		return nil
	default:
		return fmt.Errorf("pgnanos.Time: cannot scan %T", src)
	}
}

// Value implements driver.Valuer. Emits int64 nanoseconds since UNIX
// epoch. Zero time.Time values emit 0; callers MUST distinguish "unset"
// via column nullability (use *pgnanos.Time for nullable columns), not
// via the in-band zero.
func (n Time) Value() (driver.Value, error) {
	t := time.Time(n)
	if t.IsZero() {
		return int64(0), nil
	}
	return t.UnixNano(), nil
}
