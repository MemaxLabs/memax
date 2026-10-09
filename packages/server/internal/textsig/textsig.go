// Package textsig fingerprints short statements for the judge's no-LLM
// stage (plan 25 §5.8 step 1): an exact content hash, MinHash signatures
// with locality-sensitive hashing (LSH) bands for near-verbatim repeats,
// and the guard that keeps "pnpm 9" from being a duplicate of "pnpm 10".
//
// The ledger stores ContentSHA256 and Bands on every memory as derived
// index data (migration 035), and the judge compares a proposal against
// them. Both sides must compute them the same way, so everything here is
// pure and deterministic: no I/O, no clock, no randomness.
//
// # Changing the parameters
//
// Stored band keys depend on Shingle, the hash seeds, NumHashes and the
// band layout. Changing any of them makes old memories' bands stop
// matching new ones: near-duplicate recall drops until the bands are
// recomputed, and precision is unaffected, because every LSH hit is
// verified with an exact Jaccard similarity before anything is folded.
// Bump Version when you change them, and backfill.
package textsig

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Version names the fingerprint parameters below.
const Version = 1

// The MinHash and LSH parameters. With 16 bands of 8 rows, two statements
// with Jaccard similarity 0.9 share a band with probability above 0.999,
// and two at 0.5 with probability about 0.06; every hit is then checked
// exactly (NearDuplicate).
const (
	// Shingle is the length, in characters, of the shingles compared.
	Shingle   = 5
	NumHashes = 128
	Bands     = 16
	Rows      = NumHashes / Bands
)

// NearThreshold is the Jaccard similarity at or above which two
// statements are near-verbatim repeats (Graphiti's dedup helpers use the
// same bar).
const NearThreshold = 0.9

// Normalize is the canonical form two statements are compared in: lower
// case, typographic quotes and dashes made plain, whitespace collapsed,
// and trailing sentence punctuation dropped. It keeps every word,
// number and inner punctuation, so "pnpm 9" and "pnpm 10" stay apart.
func Normalize(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	space := false
	for _, r := range s {
		switch r {
		case '‘', '’', '‚', '′':
			r = '\''
		case '“', '”', '„', '″':
			r = '"'
		case '–', '—', '−':
			r = '-'
		}
		if isFormat(r) {
			continue // zero-width and bidi controls: drop them, don't split on them
		}
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			space = b.Len() > 0
			continue
		}
		if space {
			b.WriteByte(' ')
			space = false
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return strings.TrimRight(b.String(), ".!;: ")
}

// isFormat reports Unicode format characters (zero-width spaces, joiners,
// bidi controls), which are invisible and must not make two statements
// differ.
func isFormat(r rune) bool { return unicode.Is(unicode.Cf, r) }

// ContentSHA256 is the hex sha256 of the normalised statement: equal for
// statements that differ only in case, spacing or a final full stop.
func ContentSHA256(s string) string {
	sum := sha256.Sum256([]byte(Normalize(s)))
	return hex.EncodeToString(sum[:])
}

// shingleText is the text shingles are cut from: the statement's words in
// order, without stopwords and punctuation, one space apart, so "pnpm,
// not npm" and "pnpm not npm" shingle the same, and so do "A and B" and
// "A; B". Order still counts: "River, not Temporal" and "Temporal, not
// River" share few shingles. Dropping stopwords is safe only because
// NearDuplicate also requires SameSalient, which allows no other
// difference.
func shingleText(s string) []rune {
	var out []rune
	for _, w := range Words(s) {
		if stopwords[w] {
			continue
		}
		start := len(out)
		for _, r := range w {
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				if len(out) == start && start > 0 {
					out = append(out, ' ')
				}
				out = append(out, r)
			}
		}
	}
	return out
}

// shingles returns the hashed character shingles of s. A statement
// shorter than one shingle is one shingle.
func shingles(s string) map[uint64]struct{} {
	rs := shingleText(s)
	set := map[uint64]struct{}{}
	if len(rs) == 0 {
		return set
	}
	if len(rs) <= Shingle {
		set[fnv64(string(rs))] = struct{}{}
		return set
	}
	for i := 0; i+Shingle <= len(rs); i++ {
		set[fnv64(string(rs[i:i+Shingle]))] = struct{}{}
	}
	return set
}

// Jaccard is the exact Jaccard similarity of two statements' shingle
// sets, in [0, 1]. Two empty statements are identical.
func Jaccard(a, b string) float64 {
	sa, sb := shingles(a), shingles(b)
	if len(sa) == 0 && len(sb) == 0 {
		return 1
	}
	inter := 0
	for h := range sa {
		if _, ok := sb[h]; ok {
			inter++
		}
	}
	union := len(sa) + len(sb) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

// Signature is a statement's MinHash signature.
type Signature [NumHashes]uint64

// MinHash computes the signature: for each of NumHashes seeded hash
// functions, the smallest hash over the statement's shingles.
func MinHash(s string) Signature {
	var sig Signature
	for i := range sig {
		sig[i] = ^uint64(0)
	}
	for h := range shingles(s) {
		for i := range sig {
			if v := splitmix64(h ^ seeds[i]); v < sig[i] {
				sig[i] = v
			}
		}
	}
	return sig
}

// BandKeys are the LSH keys of a signature: one per band, each a hash of
// the band's index and its rows. Two statements are near-duplicate
// candidates when they share a key. They are int64 so Postgres can store
// them as bigint[].
func (sig Signature) BandKeys() []int64 {
	keys := make([]int64, Bands)
	buf := make([]byte, 8*(Rows+1))
	for b := 0; b < Bands; b++ {
		binary.LittleEndian.PutUint64(buf, uint64(b))
		for r := 0; r < Rows; r++ {
			binary.LittleEndian.PutUint64(buf[8*(r+1):], sig[b*Rows+r])
		}
		keys[b] = int64(fnv64(string(buf)))
	}
	return keys
}

// BandsOf is MinHash(s).BandKeys().
func BandsOf(s string) []int64 { return MinHash(s).BandKeys() }

// NearDuplicate reports whether b repeats a nearly verbatim: Jaccard at
// or above NearThreshold, and no salient difference (SameSalient). Both
// halves are needed: a long statement with one number changed can clear
// 0.9, and two statements with the same words in another order
// ("pnpm, not npm" and "npm, not pnpm") pass the word check.
func NearDuplicate(a, b string) bool {
	return SameSalient(a, b) && Jaccard(a, b) >= NearThreshold
}

// Stopwords are words whose presence never changes what a convention or
// decision says. Negations, quantifiers and qualifiers (not, never,
// always, only, before, after, must, in, on, for…) are deliberately absent:
// they are exactly the "key qualifiers" two statements must not differ in
// to be duplicates.
var stopwords = map[string]bool{
	"a": true, "an": true, "the": true, "this": true, "that": true, "these": true, "those": true,
	"we": true, "our": true, "us": true, "you": true, "your": true, "it": true, "its": true,
	"they": true, "their": true, "i": true, "my": true,
	"is": true, "are": true, "was": true, "were": true, "be": true, "been": true,
	"to": true, "of": true, "and": true, "please": true, "also": true, "just": true,
	"here": true, "there": true, "so": true, "then": true,
}

// negations are words that flip what a statement says.
var negations = map[string]bool{
	"not": true, "no": true, "never": true, "none": true, "nothing": true, "without": true,
	"avoid": true, "cannot": true, "can't": true, "cant": true, "don't": true, "dont": true,
	"doesn't": true, "doesnt": true, "isn't": true, "isnt": true, "aren't": true, "arent": true,
	"won't": true, "wont": true, "shouldn't": true, "shouldnt": true, "mustn't": true, "mustnt": true,
	"stop": true, "stopped": true, "drop": true, "dropped": true, "instead": true,
}

// Words splits a statement into lower-case word tokens. A token keeps
// inner dots, hyphens, slashes, plus signs and apostrophes between letters
// or digits, so versions ("1.2.3", "v4.1"), packages ("@base-ui/react")
// and contractions ("don't") stay whole.
func Words(s string) []string {
	n := Normalize(s)
	var out []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	rs := []rune(n)
	for i, r := range rs {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			cur.WriteRune(r)
		case r == '@' && cur.Len() == 0 && i+1 < len(rs) && unicode.IsLetter(rs[i+1]):
			cur.WriteRune(r) // a scoped package: @base-ui/react
		case (r == '.' || r == '-' || r == '/' || r == '\'' || r == '+' || r == '_' || r == '@') &&
			cur.Len() > 0 && i+1 < len(rs) && (unicode.IsLetter(rs[i+1]) || unicode.IsDigit(rs[i+1])):
			cur.WriteRune(r)
		default:
			flush()
		}
	}
	flush()
	return out
}

func hasDigit(w string) bool { return strings.IndexFunc(w, unicode.IsDigit) >= 0 }

// Numbers lists the tokens of s that carry a digit (counts, versions,
// dates, ports), in order.
func Numbers(s string) []string {
	var out []string
	for _, w := range Words(s) {
		if hasDigit(w) {
			out = append(out, w)
		}
	}
	return out
}

// SameSalient reports whether two statements say the same thing word for
// word, up to stopwords and punctuation: the same numbers, the same
// negations, and no other word in one that isn't in the other. It is the
// deterministic half of Graphiti's rule (never call two facts duplicates
// when numbers, dates or key qualifiers differ).
func SameSalient(a, b string) bool {
	wa, wb := Words(a), Words(b)
	na, nb := filter(wa, hasDigit), filter(wb, hasDigit)
	slices.Sort(na)
	slices.Sort(nb)
	if !slices.Equal(na, nb) {
		return false
	}
	ga, gb := filter(wa, func(w string) bool { return negations[w] }), filter(wb, func(w string) bool { return negations[w] })
	slices.Sort(ga)
	slices.Sort(gb)
	if !slices.Equal(ga, gb) {
		return false
	}
	sa, sb := set(wa), set(wb)
	for w := range sa {
		if !sb[w] && !stopwords[w] {
			return false
		}
	}
	for w := range sb {
		if !sa[w] && !stopwords[w] {
			return false
		}
	}
	return true
}

// ContentWords is the set of a statement's words that carry meaning:
// stopwords dropped, dotted and hyphenated tokens also split into their
// parts ("fly.io" → fly.io, fly, io), and a light English stem applied.
// It is for overlap tests (does a write touch a decision?), not for
// equality.
func ContentWords(s string) map[string]bool {
	out := map[string]bool{}
	add := func(w string) {
		if w == "" || stopwords[w] || utf8.RuneCountInString(w) < 2 {
			return
		}
		out[Stem(w)] = true
	}
	for _, w := range Words(s) {
		add(w)
		if strings.ContainsAny(w, ".-/_@+") {
			for _, part := range strings.FieldsFunc(w, func(r rune) bool { return strings.ContainsRune(".-/_@+", r) }) {
				add(part)
			}
		}
	}
	return out
}

// Stem strips a few English suffixes so "deploys", "deployed" and
// "deploying" meet "deploy". Tokens with digits are left alone.
func Stem(w string) string {
	if hasDigit(w) || utf8.RuneCountInString(w) <= 4 {
		return w
	}
	for _, suf := range []string{"ing", "ed", "es", "s"} {
		if strings.HasSuffix(w, suf) && utf8.RuneCountInString(w)-len(suf) >= 3 {
			stem := strings.TrimSuffix(w, suf)
			if suf == "es" && !strings.HasSuffix(stem, "s") && !strings.HasSuffix(stem, "x") &&
				!strings.HasSuffix(stem, "ch") && !strings.HasSuffix(stem, "sh") {
				stem = strings.TrimSuffix(w, "s") // "sources" → "source", not "sourc"
			}
			return stem
		}
	}
	return w
}

// Overlap counts the content words two statements share, and the overlap
// coefficient: shared / the smaller set's size.
func Overlap(a, b string) (shared int, coefficient float64) {
	ca, cb := ContentWords(a), ContentWords(b)
	for w := range ca {
		if cb[w] {
			shared++
		}
	}
	small := min(len(ca), len(cb))
	if small == 0 {
		return shared, 0
	}
	return shared, float64(shared) / float64(small)
}

// Mentions reports whether a statement names a key (a decision's area,
// such as "deploy-target" or "package manager"): every content word of
// the key appears among the statement's content words.
func Mentions(statement, key string) bool {
	kw := ContentWords(strings.NewReplacer("-", " ", "_", " ", "/", " ").Replace(key))
	if len(kw) == 0 {
		return false
	}
	sw := ContentWords(statement)
	for w := range kw {
		if !sw[w] {
			return false
		}
	}
	return true
}

// NormalizeKey is a decision area in the form keys are compared in.
func NormalizeKey(key string) string {
	return strings.Join(strings.Fields(strings.NewReplacer("-", " ", "_", " ", "/", " ").Replace(Normalize(key))), " ")
}

func filter(ws []string, keep func(string) bool) []string {
	out := []string{}
	for _, w := range ws {
		if keep(w) {
			out = append(out, w)
		}
	}
	return out
}

func set(ws []string) map[string]bool {
	out := make(map[string]bool, len(ws))
	for _, w := range ws {
		out[w] = true
	}
	return out
}

// fnv64 is FNV-1a, 64 bits.
func fnv64(s string) uint64 {
	h := uint64(14695981039346656037)
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= 1099511628211
	}
	return h
}

// splitmix64 is the SplitMix64 finaliser: a fast, well-mixed hash of x.
func splitmix64(x uint64) uint64 {
	x += 0x9e3779b97f4a7c15
	x = (x ^ (x >> 30)) * 0xbf58476d1ce4e5b9
	x = (x ^ (x >> 27)) * 0x94d049bb133111eb
	return x ^ (x >> 31)
}

// seeds are the NumHashes MinHash seeds, fixed forever (see the package
// note): SplitMix64's sequence from a fixed start.
var seeds = func() [NumHashes]uint64 {
	var s [NumHashes]uint64
	x := uint64(0x6d656d6178763200) // "memaxv2\x00"
	for i := range s {
		x += 0x9e3779b97f4a7c15
		s[i] = splitmix64(x)
	}
	return s
}()
