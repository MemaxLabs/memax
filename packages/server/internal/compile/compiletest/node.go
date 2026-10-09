package compiletest

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/compile"
)

// StartService runs the real compile service (packages/compile-service)
// with Node for the test, and returns its base URL. It builds the compiler
// and the service first when their dist/ is missing or older than their
// src/ (needs the workspace's node_modules, from `pnpm install`). It skips
// the test when Node or node_modules isn't there, unless
// MEMAX_REQUIRE_COMPILE_SERVICE is set, which makes that a failure (CI).
//
// The service requires Token, so every test that reaches it through
// Client also checks that the client authenticates. With
// MEMAX_TEST_COMPILE_SERVICE_URL set, the tests use that service instead
// (the Workers entry under `wrangler dev`, say) with the token in
// MEMAX_TEST_COMPILE_SERVICE_TOKEN.
func StartService(t testing.TB) string {
	t.Helper()
	if url := strings.TrimRight(strings.TrimSpace(os.Getenv("MEMAX_TEST_COMPILE_SERVICE_URL")), "/"); url != "" {
		return url
	}
	root := repoRoot()
	skip := func(format string, args ...any) {
		t.Helper()
		if os.Getenv("MEMAX_REQUIRE_COMPILE_SERVICE") != "" {
			t.Fatalf("compile service: "+format, args...)
		}
		t.Skipf("compile service: "+format+" (set MEMAX_REQUIRE_COMPILE_SERVICE=1 to fail instead)", args...)
	}
	node, err := exec.LookPath("node")
	if err != nil {
		skip("node isn't on PATH")
	}
	tsc := filepath.Join(root, "packages", "compile-service", "node_modules", ".bin", "tsc")
	if _, err := os.Stat(filepath.Join(root, "packages", "compile-service", "node_modules", "@memaxlabs", "compiler")); err != nil {
		skip("run pnpm install first (%v)", err)
	}
	if err := build(root, tsc); err != nil {
		skip("build: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, node, filepath.Join(root, "packages", "compile-service", "dist", "main.js"))
	cmd.Env = append(os.Environ(), "PORT=0", "HOST=127.0.0.1", "SHUTDOWN_GRACE_MS=1000", "COMPILE_SERVICE_TOKEN="+Token())
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = 5 * time.Second
	out, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatalf("compile service: start: %v", err)
	}
	t.Cleanup(func() {
		cancel()
		_ = cmd.Wait()
	})

	port := make(chan int, 1)
	go func() {
		sc := bufio.NewScanner(out)
		for sc.Scan() {
			var line struct {
				Msg  string `json:"msg"`
				Port int    `json:"port"`
			}
			if json.Unmarshal(sc.Bytes(), &line) == nil && line.Msg == "listening" {
				port <- line.Port
			}
		}
	}()
	select {
	case p := <-port:
		return fmt.Sprintf("http://127.0.0.1:%d", p)
	case <-time.After(15 * time.Second):
		t.Fatal("compile service: didn't start within 15s")
	}
	return ""
}

var (
	tokenOnce sync.Once
	token     string
)

// Token is the bearer token of the services StartService starts: random
// for each test binary, or MEMAX_TEST_COMPILE_SERVICE_TOKEN with an
// external service.
func Token() string {
	tokenOnce.Do(func() {
		if os.Getenv("MEMAX_TEST_COMPILE_SERVICE_URL") != "" {
			token = os.Getenv("MEMAX_TEST_COMPILE_SERVICE_TOKEN")
			return
		}
		b := make([]byte, 24)
		_, _ = rand.Read(b)
		token = hex.EncodeToString(b)
	})
	return token
}

// Client is a client for a service StartService returned, with its token.
func Client(baseURL string, opts ...compile.ClientOption) *compile.Client {
	return compile.NewClient(baseURL, append([]compile.ClientOption{compile.WithToken(Token())}, opts...)...)
}

// build compiles the compiler and the service with tsc when stale. A lock
// file serialises it across the test binaries go test runs in parallel.
func build(root, tsc string) error {
	lock, err := os.OpenFile(filepath.Join(os.TempDir(), "memax-compile-service-build.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = lock.Close() }()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer func() { _ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN) }()
	for _, pkg := range []struct{ dir, entry string }{
		{"compiler", "index.js"},
		{"compile-service", "main.js"},
	} {
		dir := filepath.Join(root, "packages", pkg.dir)
		if !stale(filepath.Join(dir, "src"), filepath.Join(dir, "dist", pkg.entry)) {
			continue
		}
		if _, err := os.Stat(tsc); err != nil {
			return fmt.Errorf("no TypeScript compiler at %s", tsc)
		}
		cmd := exec.Command(tsc, "-p", dir)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("tsc -p %s: %v\n%s", pkg.dir, err, out)
		}
	}
	return nil
}

// stale reports whether any file under src is newer than out.
func stale(src, out string) bool {
	built, err := os.Stat(out)
	if err != nil {
		return true
	}
	newer := false
	_ = filepath.WalkDir(src, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".ts") {
			return err
		}
		if info, err := d.Info(); err == nil && info.ModTime().After(built.ModTime()) {
			newer = true
		}
		return nil
	})
	return newer
}

// repoRoot is the monorepo root, from this file's path.
func repoRoot() string {
	_, file, _, _ := runtime.Caller(0)
	// file = .../packages/server/internal/compile/compiletest/node.go
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", "..", ".."))
}

// RepoRoot is the monorepo root (for tests that read compiler fixtures).
func RepoRoot() string { return repoRoot() }
