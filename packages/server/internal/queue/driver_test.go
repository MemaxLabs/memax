package queue

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/MemaxLabs/memax/packages/server/internal/dbpool"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
)

type listenProbeArgs struct{}

func (listenProbeArgs) Kind() string { return "listen_probe" }

// A worker whose pool can't LISTEN (Neon's pooler) listens on its own
// connection: the insert's NOTIFY reaches River there, and a job starts
// long before River's poll would have found it.
func TestDriverListensOnItsOwnPool(t *testing.T) {
	t.Parallel()
	db := testdb.Open(t, testdb.Options{})
	ctx := context.Background()

	// The listener's pool, as dbpool.OpenListen opens it, on the same database.
	cfg := db.Pool.Config()
	cfg.MaxConns = dbpool.ListenMaxConns
	cfg.ConnConfig.RuntimeParams["application_name"] = dbpool.ListenApplicationName
	listen, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(listen.Close)

	ran := make(chan struct{}, 1)
	workers := river.NewWorkers()
	river.AddWorker(workers, river.WorkFunc(func(context.Context, *river.Job[listenProbeArgs]) error {
		ran <- struct{}{}
		return nil
	}))
	client, err := river.NewClient(Driver(db.Pool, listen), &river.Config{
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		Queues:  map[string]river.QueueConfig{river.QueueDefault: {MaxWorkers: 1}},
		Workers: workers,
		// Only a notification can start the job within the test.
		FetchPollInterval: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Stop(context.Background()) })

	// The LISTEN went out on the listener's pool.
	deadline := time.Now().Add(20 * time.Second)
	for {
		var listening bool
		err := db.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity
			WHERE datname = current_database() AND application_name = $1 AND query ILIKE 'listen%')`,
			dbpool.ListenApplicationName).Scan(&listening)
		if err != nil {
			t.Fatal(err)
		}
		if listening {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("River never listened on the listener's pool")
		}
		time.Sleep(50 * time.Millisecond)
	}

	if _, err := client.Insert(ctx, listenProbeArgs{}, nil); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ran:
	case <-time.After(30 * time.Second):
		t.Fatal("the job didn't start: its insert's notification never reached River")
	}
}

func TestDriverWithoutAListenerIsRiversOwn(t *testing.T) {
	t.Parallel()
	if _, ok := Driver(nil, nil).(*listenDriver); ok {
		t.Error("Driver(pool, nil) wrapped the driver; want riverpgxv5's own")
	}
}
