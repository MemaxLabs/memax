package link

import (
	"context"
	"net"
	"testing"
)

func TestParseGitHubRepoURL(t *testing.T) {
	tests := []struct {
		name      string
		rawURL    string
		wantOwner string
		wantRepo  string
		wantOK    bool
	}{
		{
			name:      "repo root",
			rawURL:    "https://github.com/MemaxLabs/memax-internal",
			wantOwner: "MemaxLabs",
			wantRepo:  "memax-internal",
			wantOK:    true,
		},
		{
			name:      "repo with git suffix",
			rawURL:    "https://github.com/MemaxLabs/memax-internal.git",
			wantOwner: "MemaxLabs",
			wantRepo:  "memax-internal",
			wantOK:    true,
		},
		{
			name:      "repo subpath still resolves repository",
			rawURL:    "https://github.com/MemaxLabs/memax-internal/issues/1",
			wantOwner: "MemaxLabs",
			wantRepo:  "memax-internal",
			wantOK:    true,
		},
		{
			name:   "not github",
			rawURL: "https://example.com/MemaxLabs/memax-internal",
			wantOK: false,
		},
		{
			name:   "not repo path",
			rawURL: "https://github.com/MemaxLabs",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, repo, ok := parseGitHubRepoURL(tt.rawURL)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if owner != tt.wantOwner || repo != tt.wantRepo {
				t.Fatalf("got owner=%q repo=%q, want owner=%q repo=%q", owner, repo, tt.wantOwner, tt.wantRepo)
			}
		})
	}
}

func TestFallbackSummary(t *testing.T) {
	result := fallbackSummary("https://github.com/MemaxLabs/memax-internal", "# Memax\n\nPersistent memory hub for AI agents.")
	if result.Title != "Memax" {
		t.Fatalf("expected heading title, got %q", result.Title)
	}
	if result.Summary == "" {
		t.Fatal("expected fallback summary")
	}
}

func TestExtractTextDropsScriptStyleAndNoscript(t *testing.T) {
	html := `<html>
		<head>
			<style>body { color: red; }</style>
			<script>window.secret = "ignore me";</script>
		</head>
		<body>
			<h1>Visible &amp; Useful</h1>
			<noscript>fallback navigation noise</noscript>
			<p>Keep this text.</p>
		</body>
	</html>`

	text := extractText(html)
	if text != "Visible & Useful Keep this text." {
		t.Fatalf("unexpected extracted text: %q", text)
	}
}

func TestValidateFetchURLRejectsUnsafeTargets(t *testing.T) {
	tests := []string{
		"ftp://example.com/file.txt",
		"http://localhost:8080",
		"http://127.0.0.1:8080",
		"http://169.254.169.254/latest/meta-data",
		"http://10.0.0.1/internal",
	}

	for _, rawURL := range tests {
		t.Run(rawURL, func(t *testing.T) {
			if err := validateFetchURL(context.Background(), rawURL); err == nil {
				t.Fatal("expected unsafe URL to be rejected")
			}
		})
	}
}

func TestIsPublicIP(t *testing.T) {
	if !isPublicIP(net.ParseIP("8.8.8.8")) {
		t.Fatal("expected public IP to be allowed")
	}
	for _, ip := range []string{"127.0.0.1", "10.0.0.1", "172.16.0.1", "192.168.1.1", "169.254.169.254"} {
		if isPublicIP(net.ParseIP(ip)) {
			t.Fatalf("expected %s to be blocked", ip)
		}
	}
}
