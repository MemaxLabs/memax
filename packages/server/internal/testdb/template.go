package testdb

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MemaxLabs/memax/packages/server/internal/migrate"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb/catalock"
)

// Templates are content-addressed: memax_tpl_<hash>_<unix seconds>,
// where the hash covers the migration files and River's version. Every
// process (and every worktree) with the same migrations clones from one
// template instead of building its own; a per-process template was never
// dropped (Postgres refuses to drop a template database), and hundreds
// piled up. The build is serialised by an advisory lock on the admin
// database, so concurrent processes wait for one build instead of racing.
const templateLockKey int64 = 0x6d656d6178_7470 // "memax" + "tp"

// Leaked databases older than this, with nobody connected, are dropped by
// the janitor; templates for other migration sets after templateMaxAge.
const (
	cloneMaxAge    = 2 * time.Hour
	templateMaxAge = 72 * time.Hour
)

// buildTemplate finds or builds the template for the current migrations.
func buildTemplate(ctx context.Context, admin *pgxpool.Pool) (string, error) {
	hash, err := migrationsHash(findMigrationsDir())
	if err != nil {
		return "", err
	}
	prefix := "memax_tpl_" + hash + "_"

	conn, err := admin.Acquire(ctx)
	if err != nil {
		return "", err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", templateLockKey); err != nil {
		return "", fmt.Errorf("template lock: %w", err)
	}
	defer func() { _, _ = conn.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", templateLockKey) }()

	// A finished template is marked IS_TEMPLATE; one without the mark
	// is a build a crashed process left behind.
	var name string
	var ready bool
	err = conn.QueryRow(ctx, `
		SELECT datname, datistemplate FROM pg_database
		 WHERE starts_with(datname, $1)
		 ORDER BY datistemplate DESC, datname DESC LIMIT 1`, prefix).Scan(&name, &ready)
	switch {
	case err == nil && ready:
		return name, nil
	case err == nil:
		_ = catalock.DropDatabase(ctx, admin, name)
	case !errors.Is(err, pgx.ErrNoRows):
		return "", fmt.Errorf("find template: %w", err)
	}

	name = prefix + strconv.FormatInt(time.Now().Unix(), 10)
	if _, err := conn.Exec(ctx, fmt.Sprintf("CREATE DATABASE %q", name)); err != nil {
		return "", fmt.Errorf("create template DB: %w", err)
	}
	fail := func(step string, err error) (string, error) {
		_ = catalock.DropDatabase(context.Background(), admin, name)
		return "", fmt.Errorf("%s: %w", step, err)
	}
	if err := migrate.Run(connStringFor(name), findMigrationsDir()); err != nil {
		return fail("migrate template", err)
	}
	// River's own tables (river_job, river_leader, …) for the tests that
	// query them; its migrations are idempotent and advisory-locked.
	if err := runRiverMigrations(ctx, connStringFor(name)); err != nil {
		return fail("migrate river template", err)
	}
	// Marking it a template makes accidental connections loud, and marks
	// the build finished for the next process.
	if _, err := conn.Exec(ctx, fmt.Sprintf("ALTER DATABASE %q IS_TEMPLATE true", name)); err != nil {
		return fail("mark template", err)
	}
	return name, nil
}

// migrationsHash is a short hash of every migration file's name and
// contents, and of the River version the template's River tables come from.
func migrationsHash(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("read migrations: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	h := sha256.New()
	for _, n := range names {
		b, err := os.ReadFile(filepath.Join(dir, n))
		if err != nil {
			return "", err
		}
		fmt.Fprintf(h, "%s\x00%d\x00", n, len(b))
		h.Write(b)
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, dep := range info.Deps {
			if dep.Path == "github.com/riverqueue/river" {
				fmt.Fprintf(h, "river\x00%s", dep.Version)
			}
		}
	}
	return hex.EncodeToString(h.Sum(nil))[:12], nil
}

// sweepLeaked drops test databases a crashed or interrupted run left
// behind: clones and copies older than cloneMaxAge, the old per-process
// templates, and templates for other migration sets older than
// templateMaxAge. Only databases nobody is connected to; best effort.
func sweepLeaked(ctx context.Context, admin *pgxpool.Pool, keep string) {
	rows, err := admin.Query(ctx, `
		SELECT d.datname, d.datistemplate
		  FROM pg_database d
		 WHERE d.datname ~ '^memax_(test|copy|migtest|tpl)_'
		   AND d.datname <> $1
		   AND NOT EXISTS (SELECT 1 FROM pg_stat_activity a WHERE a.datname = d.datname)`, keep)
	if err != nil {
		return
	}
	type db struct {
		name     string
		template bool
	}
	var stale []db
	now := time.Now()
	for rows.Next() {
		var d db
		if rows.Scan(&d.name, &d.template) != nil {
			continue
		}
		if created, kind, ok := createdAt(d.name); ok {
			limit := cloneMaxAge
			if kind == "tpl_hash" {
				limit = templateMaxAge
			}
			if now.Sub(created) > limit {
				stale = append(stale, d)
			}
		}
	}
	rows.Close()
	for _, d := range stale {
		if d.template {
			if _, err := admin.Exec(ctx, fmt.Sprintf("ALTER DATABASE %q IS_TEMPLATE false", d.name)); err != nil {
				continue
			}
		}
		_ = catalock.DropDatabase(ctx, admin, d.name)
	}
}

// createdAt reads the creation time from a test database's name:
// memax_<kind>_<unix nanoseconds>_<rand> for clones, copies, migrate tests
// and the old per-process templates, and memax_tpl_<hash>_<unix seconds>
// for content-addressed templates.
func createdAt(name string) (time.Time, string, bool) {
	parts := strings.Split(name, "_")
	if len(parts) != 4 {
		return time.Time{}, "", false
	}
	// A hash is 12 characters; the old names carry a 19-digit timestamp.
	if parts[1] == "tpl" && len(parts[2]) == 12 {
		secs, err := strconv.ParseInt(parts[3], 10, 64)
		if err != nil {
			return time.Time{}, "", false
		}
		return time.Unix(secs, 0), "tpl_hash", true
	}
	nanos, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return time.Time{}, "", false
	}
	return time.Unix(0, nanos), parts[1], true
}
