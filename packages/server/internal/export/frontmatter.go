package export

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// The frontmatter is YAML, written in a small, fixed subset so every
// parser reads it the same way (YAML 1.2 is a superset of JSON):
//
//	key: <json>            a scalar, or a flow array or object
//	key:                   a block list, one flow value per item
//	  - <json>
//	key:                   a block object one level deep
//	  name: <json>
//
// Keys are lower-case identifiers in a fixed order per file kind; every
// value is JSON, with a space after each comma and colon so YAML 1.1
// parsers read the flow collections too. An empty list or object is
// written flow ([] or {}), never as an empty block. The SDK's
// parseFrontmatter reads exactly this subset; a standard YAML parser
// (go.yaml.in/yaml/v3 in the tests, js-yaml, PyYAML) reads it as the same
// data.

// obj is a JSON object with its keys in order.
type obj []kv

type kv struct {
	k string
	v any
}

// timeLayout is how every time is written: UTC, to the microsecond (what
// Postgres keeps, and what the receipt chain encodes).
const timeLayout = "2006-01-02T15:04:05.000000Z07:00"

func stamp(t time.Time) string { return t.UTC().Truncate(time.Microsecond).Format(timeLayout) }

// optTime is a time, or null.
func optTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return stamp(*t)
}

// optString is a string, or null when empty.
func optString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// jsonString is s as a JSON string, with <, > and & as themselves.
func jsonString(s string) string {
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	_ = e.Encode(s) // a string always encodes
	return strings.TrimSuffix(b.String(), "\n")
}

// flow writes v as JSON with ", " and ": " between items.
func flow(v any) string {
	var b strings.Builder
	writeFlow(&b, v)
	return b.String()
}

func writeFlow(b *strings.Builder, v any) {
	switch x := v.(type) {
	case nil:
		b.WriteString("null")
	case string:
		b.WriteString(jsonString(x))
	case bool:
		b.WriteString(strconv.FormatBool(x))
	case int:
		b.WriteString(strconv.Itoa(x))
	case int64:
		b.WriteString(strconv.FormatInt(x, 10))
	case json.Number:
		b.WriteString(x.String())
	case float64:
		b.WriteString(strconv.FormatFloat(x, 'g', -1, 64))
	case time.Time:
		b.WriteString(jsonString(stamp(x)))
	case uuid.UUID:
		b.WriteString(jsonString(x.String()))
	case []string:
		items := make([]any, len(x))
		for i, s := range x {
			items[i] = s
		}
		writeFlow(b, items)
	case []any:
		b.WriteByte('[')
		for i, item := range x {
			if i > 0 {
				b.WriteString(", ")
			}
			writeFlow(b, item)
		}
		b.WriteByte(']')
	case obj:
		b.WriteByte('{')
		for i, f := range x {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(jsonString(f.k))
			b.WriteString(": ")
			writeFlow(b, f.v)
		}
		b.WriteByte('}')
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		o := make(obj, len(keys))
		for i, k := range keys {
			o[i] = kv{k, x[k]}
		}
		writeFlow(b, o)
	case json.RawMessage:
		writeFlow(b, rawValue(x))
	default:
		panic(fmt.Sprintf("export: no flow encoding for %T", v))
	}
}

// rawValue decodes stored JSON (a locator, conditions, scope) for flow,
// keeping numbers as written. Invalid or empty JSON is null.
func rawValue(raw json.RawMessage) any {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var v any
	if err := d.Decode(&v); err != nil {
		return nil
	}
	return normalize(v)
}

// normalize turns decoded JSON into flow's types.
func normalize(v any) any {
	switch x := v.(type) {
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = normalize(item)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, item := range x {
			out[k] = normalize(item)
		}
		return out
	}
	return v
}

// frontmatter writes a file's frontmatter, field by field, in order.
type frontmatter struct{ b strings.Builder }

func (f *frontmatter) field(key string, v any) {
	f.b.WriteString(key)
	f.b.WriteString(": ")
	writeFlow(&f.b, v)
	f.b.WriteByte('\n')
}

// list writes a block list, or [] when it is empty.
func (f *frontmatter) list(key string, items []any) {
	if len(items) == 0 {
		f.field(key, []any{})
		return
	}
	f.b.WriteString(key)
	f.b.WriteString(":\n")
	for _, item := range items {
		f.b.WriteString("  - ")
		writeFlow(&f.b, item)
		f.b.WriteByte('\n')
	}
}

// object writes a block object one level deep, or null when o is nil.
func (f *frontmatter) object(key string, o obj) {
	if o == nil {
		f.field(key, nil)
		return
	}
	if len(o) == 0 {
		f.field(key, obj{})
		return
	}
	f.b.WriteString(key)
	f.b.WriteString(":\n")
	for _, x := range o {
		f.b.WriteString("  ")
		f.b.WriteString(x.k)
		f.b.WriteString(": ")
		writeFlow(&f.b, x.v)
		f.b.WriteByte('\n')
	}
}

// document is the frontmatter between --- fences, a blank line and the
// body.
func (f *frontmatter) document(body string) []byte {
	var b bytes.Buffer
	b.WriteString("---\n")
	b.WriteString(f.b.String())
	b.WriteString("---\n\n")
	b.WriteString(body)
	if !strings.HasSuffix(body, "\n") {
		b.WriteByte('\n')
	}
	return b.Bytes()
}

// splitDocument splits a Markdown file into its frontmatter (without the
// fences) and its body.
func splitDocument(raw []byte) (front, body string, err error) {
	s := string(raw)
	if !strings.HasPrefix(s, "---\n") {
		return "", "", fmt.Errorf("it doesn't start with a --- frontmatter fence")
	}
	rest := s[len("---\n"):]
	end := strings.Index(rest, "\n---\n")
	if end < 0 {
		return "", "", fmt.Errorf("its frontmatter has no closing --- fence")
	}
	front, body = rest[:end+1], rest[end+len("\n---\n"):]
	if !strings.HasPrefix(body, "\n") {
		return "", "", fmt.Errorf("a blank line must follow the frontmatter")
	}
	return front, body[1:], nil
}
