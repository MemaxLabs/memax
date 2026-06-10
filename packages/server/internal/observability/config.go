package observability

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Env returns the deployment environment string from MEMAX_ENV.
// Falls back to "dev" when unset. Used as the `environment` label on
// every log line AND as the hardcoded filter in the admin logs query
// path so that one env's dashboard cannot see another env's logs.
func Env() string {
	if value := strings.TrimSpace(os.Getenv("MEMAX_ENV")); value != "" {
		return value
	}
	return "dev"
}

func environmentName() string { return Env() }

func intEnv(name string, fallback int) (int, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("invalid %s=%q: must be a positive integer", name, value)
	}
	return parsed, nil
}

func durationMsEnv(name string, fallback time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("invalid %s=%q: must be a positive integer number of milliseconds", name, value)
	}
	return time.Duration(parsed) * time.Millisecond, nil
}
