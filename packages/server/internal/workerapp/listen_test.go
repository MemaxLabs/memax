package workerapp

import (
	"context"
	"testing"

	"github.com/MemaxLabs/memax/packages/server/internal/dbpool"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
)

// The worker listens on a direct connection when its pool can't and one
// can be reached; otherwise River listens on the pool, or polls.
func TestListenPool(t *testing.T) {
	db := testdb.Open(t, testdb.Options{})
	url := db.Pool.Config().ConnString()
	ctx := context.Background()

	t.Run("a pool that can listen needs none", func(t *testing.T) {
		t.Setenv(dbpool.DirectEnv, "")
		if p := listenPool(ctx, url); p != nil {
			p.Close()
			t.Error("listenPool opened a pool for a DATABASE_URL that isn't pooled")
		}
	})
	t.Run("a direct connection it can reach", func(t *testing.T) {
		t.Setenv(dbpool.DirectEnv, url)
		p := listenPool(ctx, url)
		if p == nil {
			t.Fatal("listenPool = nil, want the direct connection")
		}
		defer p.Close()
		if got := p.Config().ConnConfig.RuntimeParams["application_name"]; got != dbpool.ListenApplicationName {
			t.Errorf("application_name = %q, want %q", got, dbpool.ListenApplicationName)
		}
	})
	t.Run("one it can't reach is left out", func(t *testing.T) {
		t.Setenv(dbpool.DirectEnv, "postgres://u:p@127.0.0.1:1/db?sslmode=disable&connect_timeout=2")
		if p := listenPool(ctx, url); p != nil {
			p.Close()
			t.Error("listenPool kept a connection it couldn't reach")
		}
	})
}
