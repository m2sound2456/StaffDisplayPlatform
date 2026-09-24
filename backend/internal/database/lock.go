// Package database owns the PostgreSQL connection (GORM) and the versioned SQL
// migration runner.
package database

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// SchemaLockKey is the PostgreSQL advisory lock id that serialises every schema
// writer: `cmd/migrate up` and the integration test suites share it, so two
// applies can never interleave (a half applied schema is the one state a
// forward-only migration set must never reach).
//
// The value is the ASCII of "STORDSPL" and must never collide with an advisory
// lock the application itself uses.
const SchemaLockKey int64 = 0x53544F524453504C

// schemaLockReleaseTimeout bounds the unlock round trip during cleanup.
const schemaLockReleaseTimeout = 10 * time.Second

// LockSchema acquires the shared schema lock on a dedicated connection and
// returns an idempotent release function.
//
// The lock is session scoped, so it is taken on a dedicated *sql.Conn that is
// held (and finally closed) by the returned function — never on a pooled
// connection that could leak the lock back to another caller.
func (d *Database) LockSchema(ctx context.Context) (func(), error) {
	if d == nil || d.sqlDB == nil {
		return nil, errors.New("database is not initialised")
	}

	conn, err := d.sqlDB.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire schema lock connection: %w", err)
	}

	if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_lock($1)", SchemaLockKey); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("acquire schema lock: %w", err)
	}

	var once sync.Once
	release := func() {
		once.Do(func() {
			releaseCtx, cancel := context.WithTimeout(context.Background(), schemaLockReleaseTimeout)
			defer cancel()
			// The unlock is best effort: closing the session releases the lock
			// anyway, so a timeout here must not mask the real test failure.
			_, _ = conn.ExecContext(releaseCtx, "SELECT pg_advisory_unlock($1)", SchemaLockKey)
			_ = conn.Close()
		})
	}
	return release, nil
}
