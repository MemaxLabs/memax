package queue

import (
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river/riverdriver"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
)

// Driver is River's driver on pool, with its notifier listening on listen
// when that is set. A pool that goes through a transaction pooler can't
// LISTEN (dbpool.ListenURL), so the worker passes a direct connection
// here; jobs and everything else stay on pool. A nil listen listens on
// pool, as riverpgxv5.New does.
func Driver(pool, listen *pgxpool.Pool) riverdriver.Driver[pgx.Tx] {
	if listen == nil {
		return riverpgxv5.New(pool)
	}
	return &listenDriver{Driver: riverpgxv5.New(pool), listen: riverpgxv5.New(listen)}
}

type listenDriver struct {
	*riverpgxv5.Driver
	listen *riverpgxv5.Driver
}

func (d *listenDriver) GetListener(params *riverdriver.GetListenenerParams) riverdriver.Listener {
	return d.listen.GetListener(params)
}
