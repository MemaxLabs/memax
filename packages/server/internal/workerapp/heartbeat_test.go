package workerapp

import (
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestWorkerClientID_PrefersFlyMachineID(t *testing.T) {
	t.Setenv("FLY_MACHINE_ID", "6e8263e0a47d98")
	t.Setenv("MEMAX_WORKER_CLIENT_ID", "")
	if got := workerClientID(); got != "6e8263e0a47d98" {
		t.Errorf("expected fly machine id, got %q", got)
	}
}

func TestWorkerClientID_OverrideBeatsHostname(t *testing.T) {
	t.Setenv("FLY_MACHINE_ID", "")
	t.Setenv("MEMAX_WORKER_CLIENT_ID", "custom-worker-name")
	if got := workerClientID(); got != "custom-worker-name" {
		t.Errorf("expected override, got %q", got)
	}
}

func TestWorkerClientID_HostnameFallbackIncludesPID(t *testing.T) {
	// The big reason to include PID: two `go run ./cmd/worker`
	// invocations on the same dev laptop would otherwise UPSERT
	// over each other, silently masking one of them in the pulse.
	t.Setenv("FLY_MACHINE_ID", "")
	t.Setenv("MEMAX_WORKER_CLIENT_ID", "")

	host, err := os.Hostname()
	if err != nil || host == "" {
		t.Skip("no hostname available in this environment")
	}
	got := workerClientID()
	pid := strconv.Itoa(os.Getpid())
	want := host + "-" + pid
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func TestWorkerClientMetadata_StripsEmptyKeys(t *testing.T) {
	// Dev environment usually has FLY_* unset. Those keys should
	// be stripped from the JSON rather than left as empty strings,
	// so the metadata stays compact and doesn't advertise "fly-ness"
	// we don't have.
	t.Setenv("FLY_REGION", "")
	t.Setenv("FLY_MACHINE_ID", "")
	t.Setenv("FLY_APP_NAME", "")
	t.Setenv("MEMAX_ENV", "dev")

	buf := workerClientMetadata()
	s := string(buf)
	if !strings.Contains(s, `"service":"memax-worker"`) {
		t.Errorf("service key missing: %s", s)
	}
	if !strings.Contains(s, `"memax_env":"dev"`) {
		t.Errorf("memax_env missing: %s", s)
	}
	if strings.Contains(s, `"fly_region":""`) ||
		strings.Contains(s, `"fly_machine":""`) ||
		strings.Contains(s, `"fly_app":""`) {
		t.Errorf("empty fly_* keys should have been stripped: %s", s)
	}
}
