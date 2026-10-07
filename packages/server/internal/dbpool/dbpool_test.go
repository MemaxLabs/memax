package dbpool

import "testing"

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
