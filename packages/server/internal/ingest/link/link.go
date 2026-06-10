package link

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/anthropic"
)

const maxFetchedPageBytes = 2 * 1024 * 1024

type LinkResult struct {
	Title         string   `json:"title"`
	Summary       string   `json:"summary"`
	RetrievalHint string   `json:"retrieval_hint"`
	KeyTopics     []string `json:"key_topics"`
}

var (
	scriptRe   = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	styleRe    = regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
	noscriptRe = regexp.MustCompile(`(?is)<noscript[^>]*>.*?</noscript>`)
	tagRe      = regexp.MustCompile(`<[^>]+>`)
	spaceRe    = regexp.MustCompile(`\s+`)
)

type Processor struct {
	client *anthropic.Client
	model  string
}

func New(client *anthropic.Client) *Processor {
	if client == nil {
		return nil
	}
	return &Processor{client: client, model: anthropic.ModelFromEnv("LINK_MODEL")}
}

func (p *Processor) Process(url string) (*LinkResult, error) {
	return p.ProcessContext(context.Background(), url)
}

func (p *Processor) ProcessContext(ctx context.Context, url string) (*LinkResult, error) {
	client := secureHTTPClient(ctx)
	text, err := fetchGitHubRepoText(ctx, client, url)
	if err != nil {
		slog.Warn("GitHub repository metadata fetch failed, falling back to page fetch", "url", url, "error", err)
	}
	if text == "" {
		text, err = fetchPageText(ctx, client, url)
		if err != nil {
			return nil, err
		}
	}
	if len(text) > 8000 {
		text = text[:8000]
	}
	if len(text) < 50 {
		return nil, fmt.Errorf("extracted text too short (%d chars), page may be JavaScript-rendered", len(text))
	}

	result, err := p.summarizeWithLLM(ctx, url, text)
	if err != nil {
		slog.Warn("link LLM summary failed, using deterministic fallback", "url", url, "error", err)
		return fallbackSummary(url, text), nil
	}
	return result, nil
}

func secureHTTPClient(ctx context.Context) *http.Client {
	return &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			return validateFetchURL(ctx, req.URL.String())
		},
	}
}

type githubRepo struct {
	FullName        string   `json:"full_name"`
	Description     string   `json:"description"`
	HTMLURL         string   `json:"html_url"`
	Language        string   `json:"language"`
	DefaultBranch   string   `json:"default_branch"`
	Topics          []string `json:"topics"`
	StargazersCount int      `json:"stargazers_count"`
	ForksCount      int      `json:"forks_count"`
	OpenIssuesCount int      `json:"open_issues_count"`
	License         *struct {
		Name string `json:"name"`
	} `json:"license"`
}

func fetchGitHubRepoText(ctx context.Context, client *http.Client, rawURL string) (string, error) {
	owner, repo, ok := parseGitHubRepoURL(rawURL)
	if !ok {
		return "", nil
	}

	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s", url.PathEscape(owner), url.PathEscape(repo))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return "", fmt.Errorf("create GitHub repo request: %w", err)
	}
	setGitHubHeaders(req)
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch GitHub repo: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetch GitHub repo returned status %d", resp.StatusCode)
	}
	var repoInfo githubRepo
	if err := json.NewDecoder(resp.Body).Decode(&repoInfo); err != nil {
		return "", fmt.Errorf("decode GitHub repo: %w", err)
	}

	readme, err := fetchGitHubReadme(ctx, client, owner, repo)
	if err != nil {
		slog.Warn("GitHub README fetch failed", "owner", owner, "repo", repo, "error", err)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "GitHub repository: %s\n", repoInfo.FullName)
	if repoInfo.Description != "" {
		fmt.Fprintf(&b, "Description: %s\n", repoInfo.Description)
	}
	if repoInfo.HTMLURL != "" {
		fmt.Fprintf(&b, "URL: %s\n", repoInfo.HTMLURL)
	}
	if repoInfo.Language != "" {
		fmt.Fprintf(&b, "Primary language: %s\n", repoInfo.Language)
	}
	if repoInfo.DefaultBranch != "" {
		fmt.Fprintf(&b, "Default branch: %s\n", repoInfo.DefaultBranch)
	}
	if repoInfo.License != nil && repoInfo.License.Name != "" {
		fmt.Fprintf(&b, "License: %s\n", repoInfo.License.Name)
	}
	fmt.Fprintf(&b, "Stars: %s\n", strconv.Itoa(repoInfo.StargazersCount))
	fmt.Fprintf(&b, "Forks: %s\n", strconv.Itoa(repoInfo.ForksCount))
	fmt.Fprintf(&b, "Open issues: %s\n", strconv.Itoa(repoInfo.OpenIssuesCount))
	if len(repoInfo.Topics) > 0 {
		fmt.Fprintf(&b, "Topics: %s\n", strings.Join(repoInfo.Topics, ", "))
	}
	if strings.TrimSpace(readme) != "" {
		b.WriteString("\nREADME:\n")
		b.WriteString(readme)
	}
	return b.String(), nil
}

func fetchGitHubReadme(ctx context.Context, client *http.Client, owner, repo string) (string, error) {
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/readme", url.PathEscape(owner), url.PathEscape(repo))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return "", fmt.Errorf("create GitHub README request: %w", err)
	}
	setGitHubHeaders(req)
	req.Header.Set("Accept", "application/vnd.github.raw")
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch GitHub README: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", nil
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetch GitHub README returned status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read GitHub README: %w", err)
	}
	return strings.TrimSpace(string(body)), nil
}

func setGitHubHeaders(req *http.Request) {
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "Memax/1.0 (knowledge hub; +https://memax.app)")
}

func parseGitHubRepoURL(rawURL string) (string, string, bool) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return "", "", false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", "", false
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "github.com" && host != "www.github.com" {
		return "", "", false
	}
	parts := strings.Split(strings.Trim(parsed.EscapedPath(), "/"), "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	repo, err := url.PathUnescape(parts[1])
	if err != nil {
		return "", "", false
	}
	repo = strings.TrimSuffix(repo, ".git")
	owner, err := url.PathUnescape(parts[0])
	if err != nil || owner == "" || repo == "" {
		return "", "", false
	}
	return owner, repo, true
}

func fallbackSummary(rawURL string, text string) *LinkResult {
	title := rawURL
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(strings.TrimLeft(line, "# "))
		if line != "" {
			title = line
			break
		}
	}
	if len(title) > 120 {
		title = title[:120]
	}
	summary := strings.TrimSpace(text)
	if len(summary) > 700 {
		summary = strings.TrimSpace(summary[:700]) + "..."
	}
	return &LinkResult{
		Title:         title,
		Summary:       summary,
		RetrievalHint: "Retrieve this when the user asks about this linked resource.",
		KeyTopics:     nil,
	}
}

func fetchPageText(ctx context.Context, client *http.Client, rawURL string) (string, error) {
	if err := validateFetchURL(ctx, rawURL); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", "Memax/1.0 (knowledge hub; +https://memax.app)")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetch URL returned status %d", resp.StatusCode)
	}
	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	if contentType != "" &&
		!strings.Contains(contentType, "text/html") &&
		!strings.Contains(contentType, "text/plain") &&
		!strings.Contains(contentType, "application/xhtml+xml") {
		return "", fmt.Errorf("unsupported content type %q", contentType)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxFetchedPageBytes+1))
	if err != nil {
		return "", fmt.Errorf("read response body: %w", err)
	}
	if len(body) > maxFetchedPageBytes {
		return "", fmt.Errorf("response body exceeds %d bytes", maxFetchedPageBytes)
	}

	return extractText(string(body)), nil
}

func validateFetchURL(ctx context.Context, rawURL string) error {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return fmt.Errorf("parse URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("unsupported URL scheme %q", parsed.Scheme)
	}
	host := parsed.Hostname()
	if host == "" {
		return fmt.Errorf("URL host is required")
	}
	if strings.EqualFold(host, "localhost") {
		return fmt.Errorf("localhost URLs are not allowed")
	}
	if ip := net.ParseIP(host); ip != nil {
		if !isPublicIP(ip) {
			return fmt.Errorf("URL host resolves to a private or reserved address")
		}
		return nil
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return fmt.Errorf("resolve URL host: %w", err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("URL host has no resolved IP addresses")
	}
	for _, addr := range ips {
		if !isPublicIP(addr.IP) {
			return fmt.Errorf("URL host resolves to a private or reserved address")
		}
	}
	return nil
}

func isPublicIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return false
	}
	if ip4 := ip.To4(); ip4 != nil {
		switch {
		case ip4[0] == 0:
			return false
		case ip4[0] == 10:
			return false
		case ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127:
			return false
		case ip4[0] == 127:
			return false
		case ip4[0] == 169 && ip4[1] == 254:
			return false
		case ip4[0] == 172 && ip4[1] >= 16 && ip4[1] <= 31:
			return false
		case ip4[0] == 192 && ip4[1] == 0 && ip4[2] == 0:
			return false
		case ip4[0] == 192 && ip4[1] == 168:
			return false
		case ip4[0] == 198 && (ip4[1] == 18 || ip4[1] == 19):
			return false
		case ip4[0] >= 224:
			return false
		}
		return true
	}
	return ip.IsGlobalUnicast()
}

func extractText(html string) string {
	text := scriptRe.ReplaceAllString(html, "")
	text = styleRe.ReplaceAllString(text, "")
	text = noscriptRe.ReplaceAllString(text, "")
	text = tagRe.ReplaceAllString(text, " ")
	text = strings.ReplaceAll(text, "&amp;", "&")
	text = strings.ReplaceAll(text, "&lt;", "<")
	text = strings.ReplaceAll(text, "&gt;", ">")
	text = strings.ReplaceAll(text, "&quot;", "\"")
	text = strings.ReplaceAll(text, "&#39;", "'")
	text = strings.ReplaceAll(text, "&nbsp;", " ")
	text = spaceRe.ReplaceAllString(text, " ")
	return strings.TrimSpace(text)
}

func (p *Processor) summarizeWithLLM(ctx context.Context, url string, text string) (*LinkResult, error) {
	prompt := fmt.Sprintf(`Summarize this web page for a knowledge retrieval system. The summary will be stored and searched later.

URL: %s

Page content:
---
%s
---

Respond with JSON only:
{
  "title": "Page title or best descriptive title",
  "summary": "2-5 sentence description of the page content. Focus on the key information and conclusions.",
  "retrieval_hint": "Retrieve this when the user asks about: topic1, topic2, topic3",
  "key_topics": ["topic1", "topic2", "topic3"]
}`, url, text)

	resp, err := p.client.Complete(ctx, anthropic.CompleteRequest{
		Model:     p.model,
		MaxTokens: 500,
		Prompt:    prompt,
		Purpose:   "ingest.link_summarize",
	})
	if err != nil {
		return nil, err
	}

	rawJSON := anthropic.ExtractJSONObject(resp.Text)
	var result LinkResult
	if err := json.Unmarshal([]byte(rawJSON), &result); err != nil {
		slog.Warn("failed to parse link summary JSON", "error", err, "raw", resp.Text)
		return nil, fmt.Errorf("parse LLM JSON: %w", err)
	}

	return &result, nil
}
