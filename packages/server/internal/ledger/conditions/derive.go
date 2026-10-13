package conditions

import (
	"path"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/MemaxLabs/memax/packages/server/internal/secrets"
)

// Derivation (the stale-detection design §1.2): the conditions Memax reads
// off a memory on its own, in the same command that writes a version. It
// is generous on purpose: a condition flags nothing until it has held once
// on the default branch (arming), so a path that never existed (a module
// path that looks like a file, a path relative to some package) never
// flags. It is careful where people see it: the words have to name the
// thing, and Memax never takes a file it compiles over itself.

// Input is what one version of a memory says about the repository.
type Input struct {
	// Statement is the version's words.
	Statement string
	// Files are the repository files its sources cite ("go.mod:14",
	// "packages/server/internal/ask/config.go"): file sources' refs or
	// paths. A ":line" suffix is dropped.
	Files []string
	// ScopePaths are its scope's path globs ("packages/web/**").
	ScopePaths []string
}

// The extensions that make a bare token (not in backticks) a path.
var fileExtensions = []string{
	"go", "mod", "sum", "ts", "tsx", "mts", "cts", "js", "jsx", "mjs", "cjs", "json", "jsonc", "yaml", "yml",
	"toml", "md", "mdx", "py", "pyi", "rs", "rb", "java", "kt", "kts", "swift", "c", "h", "cc", "cpp", "hpp",
	"cs", "sql", "sh", "bash", "zsh", "css", "scss", "sass", "less", "html", "vue", "svelte", "astro", "lock",
	"proto", "graphql", "gql", "tf", "hcl", "gradle", "xml", "ini", "cfg", "conf", "txt", "csv", "dockerfile",
}

// The top-level folders that make a bare token with a slash a path even
// without an extension ("packages/server/internal/queue").
var topFolders = []string{
	"packages", "apps", "app", "src", "lib", "libs", "internal", "cmd", "pkg", "docs", "doc", "test", "tests",
	"spec", "scripts", "tools", "migrations", "config", "configs", "deploy", "infra", "plugins", "e2e", "public",
	"assets", "web", "server", "client", "api", "services", "crates", "modules", "components", "examples",
	"eval", "bin", "build", "ci", ".github", ".circleci",
}

// The Go module hosts dep_present derives from.
var goHosts = []string{
	"github.com", "gitlab.com", "bitbucket.org", "golang.org", "google.golang.org", "gopkg.in", "go.uber.org",
	"k8s.io", "sigs.k8s.io", "cloud.google.com", "go.opentelemetry.io", "go.etcd.io", "honnef.co", "modernc.org",
}

// Excluded reports a file Memax compiles over or an agent keeps its
// instructions in (the files `memax init` reads, and every target path):
// a condition on one would flag Memax's own writes.
func Excluded(p string) bool {
	base := path.Base(p)
	switch base {
	case "AGENTS.md", "AGENT.md", "CLAUDE.md", "CLAUDE.local.md", "GEMINI.md", ".cursorrules", ".windsurfrules",
		"copilot-instructions.md":
		return true
	}
	for _, dir := range []string{".cursor/rules", ".github/instructions", ".claude", ".codex", ".gemini",
		".devin/rules", ".windsurf/rules", ".memax"} {
		if p == dir || strings.HasPrefix(p, dir+"/") || strings.Contains(p, "/"+dir+"/") {
			return true
		}
	}
	return false
}

var (
	backticked = regexp.MustCompile("`([^`\n]{1,300})`")
	identifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{2,99}$`)
	npmScoped  = regexp.MustCompile(`^@[a-z0-9][a-z0-9._~-]*/[a-z0-9][a-z0-9._~-]*$`)
	goModule   = regexp.MustCompile(`^[a-z0-9.-]+\.[a-z]{2,}(/[A-Za-z0-9._~-]+)+$`)
	lineSuffix = regexp.MustCompile(`:\d+(-\d+)?(:\d+)?$`)
	numeric    = regexp.MustCompile(`^[0-9][0-9.]*$`)
)

// candidate is something the words name.
type candidate struct {
	text string
	// ticked: it was in backticks, so it is code, not prose.
	ticked bool
}

// tokens is everything the statement names: backticked spans first, in
// order, then the bare words around them.
func tokens(statement string) []candidate {
	var out []candidate
	for _, m := range backticked.FindAllStringSubmatch(statement, -1) {
		out = append(out, candidate{text: strings.TrimSpace(m[1]), ticked: true})
	}
	prose := backticked.ReplaceAllString(statement, " ")
	for _, w := range strings.FieldsFunc(prose, unicode.IsSpace) {
		w = strings.TrimLeft(w, `("'[{<“‘`)
		w = strings.TrimRight(w, `)"'],;:!?}>”’.…`)
		if w != "" {
			out = append(out, candidate{text: w})
		}
	}
	return out
}

func hasExtension(p string) bool {
	ext := strings.ToLower(strings.TrimPrefix(path.Ext(p), "."))
	return ext != "" && slices.Contains(fileExtensions, ext)
}

// productName: "Node.js", "Next.js": a capitalised stem with a script
// extension and no folder is a name, not a file.
func productName(p string) bool {
	if strings.Contains(p, "/") {
		return false
	}
	ext := strings.ToLower(path.Ext(p))
	r, _ := utf8.DecodeRuneInString(p)
	return (ext == ".js" || ext == ".ts" || ext == ".io") && unicode.IsUpper(r)
}

// domainLike: "memax.app/docs", "github.com/x": a first segment that is a
// host, not a folder.
func domainLike(p string) bool {
	first, _, _ := strings.Cut(p, "/")
	if !strings.Contains(first, ".") || strings.HasPrefix(first, ".") {
		return false
	}
	ext := strings.ToLower(strings.TrimPrefix(path.Ext(first), "."))
	return !slices.Contains(fileExtensions, ext)
}

// asPath is c as a repository path, or "".
func asPath(c candidate) string {
	p := strings.TrimPrefix(c.text, "./")
	if strings.HasSuffix(p, "/") {
		p = strings.TrimRight(p, "/")
	}
	switch {
	case p == "" || strings.Contains(p, "://") || strings.HasPrefix(p, "/") || strings.HasPrefix(p, "~") ||
		strings.HasPrefix(p, "$") || strings.HasPrefix(p, "@") || strings.ContainsAny(p, "*?") ||
		numeric.MatchString(p) || domainLike(p) || productName(p) || !ValidPath(p):
		return ""
	case Excluded(p):
		return ""
	}
	slash := strings.Contains(p, "/")
	if c.ticked {
		if slash || hasExtension(p) {
			return p
		}
		return ""
	}
	if hasExtension(p) {
		return p
	}
	if slash {
		first, _, _ := strings.Cut(p, "/")
		if slices.Contains(topFolders, first) {
			return p
		}
	}
	return ""
}

// asDependency is c as a dependency (its manifest and name), or "".
func asDependency(c candidate) (manifest, name string) {
	t := c.text
	if at := strings.LastIndex(t, "@"); at > 0 {
		t = t[:at] // a version: "github.com/x/y@v1.2.0", "@scope/pkg@^2"
	}
	if npmScoped.MatchString(t) {
		return "package.json", t
	}
	if goModule.MatchString(t) {
		host, rest, _ := strings.Cut(t, "/")
		if !slices.Contains(goHosts, host) {
			return "", ""
		}
		// A module on a code host is host/owner/repo at least.
		if (host == "github.com" || host == "gitlab.com" || host == "bitbucket.org") && strings.Count(rest, "/") < 1 {
			return "", ""
		}
		return "go.mod", t
	}
	return "", ""
}

// codeIdentifier: `TierHaiku`, `MAX_TOKENS`, `use_v2`: an identifier that
// reads as code (an inner capital, an underscore or a digit), not a word.
func codeIdentifier(s string) bool {
	if !identifier.MatchString(s) {
		return false
	}
	for i, r := range s {
		if (i > 0 && unicode.IsUpper(r)) || r == '_' || unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

// sourcePath is a file source's repository path, or "".
func sourcePath(ref string) string {
	p := lineSuffix.ReplaceAllString(strings.TrimSpace(ref), "")
	return asPath(candidate{text: p, ticked: true})
}

// Derive reads a version's conditions off its words, sources and scope,
// most telling first, at most MaxDerived: the paths the statement names,
// a backticked identifier in the one file the memory names (the words'
// one file, else its sources' one; file_contains, as a whole word), the dependencies it names (Go module paths and scoped
// npm names, by presence), the code files its sources cite, and its scope's
// globs. Every one is OriginDerived with its From; ids are Renew's.
func Derive(in Input) []Condition {
	var out []Condition
	add := func(c Condition) {
		if len(out) == MaxDerived {
			return
		}
		c.Origin = OriginDerived
		if slices.ContainsFunc(out, func(o Condition) bool { return Same(o, c) }) {
			return
		}
		if Validate([]Condition{withID(c)}) != nil {
			return
		}
		out = append(out, c)
	}

	toks := tokens(in.Statement)
	var named, idents []string
	type dep struct{ manifest, name string }
	var deps []dep
	for _, t := range toks {
		if m, n := asDependency(t); n != "" {
			if !slices.ContainsFunc(deps, func(d dep) bool { return d.name == n }) {
				deps = append(deps, dep{m, n})
			}
			continue
		}
		if p := asPath(t); p != "" {
			if !slices.Contains(named, p) {
				named = append(named, p)
			}
			continue
		}
		if t.ticked && codeIdentifier(t.text) && !slices.Contains(idents, t.text) {
			idents = append(idents, t.text)
		}
	}
	var cited []string
	for _, f := range in.Files {
		if p := sourcePath(f); p != "" && !slices.Contains(cited, p) && !slices.Contains(named, p) {
			cited = append(cited, p)
		}
	}

	for _, p := range named {
		add(Condition{Kind: PathExists, Path: p, From: FromStatement})
	}
	// The one file the words name; failing that, the one its sources cite.
	onlyFiles := func(ps []string) []string {
		var out []string
		for _, p := range ps {
			if hasExtension(p) {
				out = append(out, p)
			}
		}
		return out
	}
	files := onlyFiles(named)
	if len(files) == 0 {
		files = onlyFiles(cited)
	}
	if len(files) == 1 && len(idents) > 0 {
		for _, id := range idents {
			if len(secrets.DetectCredentials(id)) > 0 {
				continue
			}
			add(Condition{Kind: FileContains, Path: files[0], Text: id, Word: true, From: FromStatement})
		}
	}
	for _, d := range deps {
		add(Condition{Kind: DepPresent, Manifest: d.manifest, Name: d.name, From: FromStatement})
	}
	for _, p := range cited {
		add(Condition{Kind: PathExists, Path: p, From: FromSource})
	}
	for _, g := range in.ScopePaths {
		g = strings.TrimPrefix(strings.TrimSpace(g), "./")
		if ValidGlob(g) && !Excluded(g) {
			add(Condition{Kind: PathExists, Glob: g, From: FromScope})
		}
	}
	return out
}

// withID gives c a placeholder id, to validate it on its own.
func withID(c Condition) Condition {
	c.ID = "c1"
	return c
}
