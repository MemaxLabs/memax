package dbpool

import (
	"context"
	"testing"
)

func TestConfigSize(t *testing.T) {
	const url = "postgres://u:p@localhost:5432/db?sslmode=disable"
	for _, tc := range []struct {
		name string
		url  string
		env  string
		want int32
	}{
		{"default", url, "", APIMaxConns},
		{"env", url, "40", 40},
		{"connection string wins", url + "&pool_max_conns=7", "40", 7},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("DB_MAX_CONNS", tc.env)
			cfg, err := Config(tc.url, APIMaxConns)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.MaxConns != tc.want {
				t.Errorf("MaxConns = %d, want %d", cfg.MaxConns, tc.want)
			}
		})
	}
}

func TestConfigRejectsABadSize(t *testing.T) {
	for _, v := range []string{"0", "-3", "many"} {
		t.Setenv("DB_MAX_CONNS", v)
		if _, err := Config("postgres://u:p@localhost/db", APIMaxConns); err == nil {
			t.Errorf("DB_MAX_CONNS=%q: want an error", v)
		}
	}
}

func TestListenURL(t *testing.T) {
	const pooled = "postgresql://app:p%40ss@ep-quiet-meadow-a1b2c3d4-pooler.c-3.us-west-2.aws.neon.tech/memax?sslmode=require&channel_binding=require"
	for _, tc := range []struct {
		name, url, env, want string
	}{
		{"Neon's pooler listens on its endpoint's direct host", pooled, "",
			"postgresql://app:p%40ss@ep-quiet-meadow-a1b2c3d4.c-3.us-west-2.aws.neon.tech/memax?sslmode=require&channel_binding=require"},
		{"a port stays", "postgres://u:p@ep-a-b-pooler.us-east-2.aws.neon.tech:5432/db", "",
			"postgres://u:p@ep-a-b.us-east-2.aws.neon.tech:5432/db"},
		{"a direct Neon host listens on its pool", "postgres://u:p@ep-a-b.us-east-2.aws.neon.tech/db", "", ""},
		{"local Postgres listens on its pool", "postgres://memax:memax@localhost:5432/memax?sslmode=disable", "", ""},
		{"-pooler elsewhere isn't Neon's", "postgres://u:p@db-pooler.example.com/db", "", ""},
		{"not a URL", "host=localhost dbname=memax", "", ""},
		{"DATABASE_DIRECT_URL wins", pooled, "postgres://u:p@direct.example.com/db", "postgres://u:p@direct.example.com/db"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(DirectEnv, tc.env)
			if got := ListenURL(tc.url); got != tc.want {
				t.Errorf("ListenURL = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestOpenListenIsSmallAndNamed(t *testing.T) {
	pool, err := OpenListen(context.Background(), "postgres://u:p@localhost:1/db?sslmode=disable&pool_max_conns=50")
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	cfg := pool.Config()
	if cfg.MaxConns != ListenMaxConns {
		t.Errorf("MaxConns = %d, want %d", cfg.MaxConns, ListenMaxConns)
	}
	if got := cfg.ConnConfig.RuntimeParams["application_name"]; got != ListenApplicationName {
		t.Errorf("application_name = %q, want %q", got, ListenApplicationName)
	}
}
