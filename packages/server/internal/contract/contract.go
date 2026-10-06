// Package contract checks HTTP exchanges against an OpenAPI 3.1 document,
// so the /v2 handlers can't drift from packages/server/openapi/v2.yaml.
//
// Load parses the document, validates it against the OpenAPI 3.1 schema and
// compiles every JSON Schema in it (JSON Schema 2020-12, the OpenAPI 3.1
// dialect, with formats asserted). ValidateRequest and ValidateResponse
// then check one exchange: the operation must be documented, the status
// code must be documented for it, and every body must match its schema.
// Because the spec closes every object (additionalProperties: false, see
// Lint), an undocumented field fails as surely as a missing one.
//
// Handler wraps an http.Handler for tests: every request and response that
// passes through it is checked, and violations are reported to the test.
//
// It is built on github.com/santhosh-tekuri/jsonschema/v6 and
// go.yaml.in/yaml/v3, both already in the module graph, rather than a full
// OpenAPI toolkit: the checks we need are small, and a complete 2020-12
// validator is what makes them trustworthy.
//
// The package is for tests. Nothing in a server binary imports it.
package contract

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"go.yaml.in/yaml/v3"
)

// oasSchema is the OpenAPI 3.1 document schema, published by the OpenAPI
// Initiative at https://spec.openapis.org/oas/3.1/schema/2022-10-07
// (Apache-2.0). It checks the document's structure; the JSON Schemas inside
// it are checked against the 2020-12 meta-schema separately.
//
//go:embed oas-3.1-schema-2022-10-07.json
var oasSchema []byte

const (
	oasSchemaURL = "https://spec.openapis.org/oas/3.1/schema/2022-10-07"
	metaSchema   = "https://json-schema.org/draft/2020-12/schema"
	// docURL is where the document lives for $ref resolution. Nothing is
	// fetched from it.
	docURL = "file:///openapi/spec.json"
)

// Spec is a loaded OpenAPI document.
type Spec struct {
	doc      map[string]any
	compiler *jsonschema.Compiler
	ops      []*Operation

	mu   sync.Mutex
	seen map[string]map[int]bool // operationId → statuses exercised
}

// Operation is one method on one path.
type Operation struct {
	ID     string
	Method string // upper case
	Path   string // the template, e.g. /v2/memories/{ref}:keep

	segments  []segment
	params    []*parameter
	body      *requestBody
	responses map[int]*response
	pointer   string // JSON pointer of the operation object
}

type segment struct {
	prefix, param, suffix string // param == "" means a literal segment (prefix)
}

type parameter struct {
	name, in string
	required bool
	array    bool
	schema   *jsonschema.Schema
}

type requestBody struct {
	required bool
	schema   *jsonschema.Schema
}

type response struct {
	schema  *jsonschema.Schema // nil: no body
	headers map[string]*header // canonical names
}

type header struct {
	name     string // as the spec writes it
	required bool
	schema   *jsonschema.Schema
}

// Load parses and checks an OpenAPI 3.1 document (YAML or JSON).
func Load(raw []byte) (*Spec, error) {
	doc, err := normalize(raw)
	if err != nil {
		return nil, err
	}
	if err := validateDocument(doc); err != nil {
		return nil, err
	}
	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft2020)
	c.AssertFormat()
	if err := c.AddResource(docURL, doc); err != nil {
		return nil, fmt.Errorf("contract: add document: %w", err)
	}
	s := &Spec{doc: doc, compiler: c, seen: map[string]map[int]bool{}}
	if err := s.checkSchemas(); err != nil {
		return nil, err
	}
	if err := s.indexOperations(); err != nil {
		return nil, err
	}
	return s, nil
}

// normalize turns YAML into the JSON data model (json.Number for numbers,
// string keys only), the way the schema validator expects it.
func normalize(raw []byte) (map[string]any, error) {
	var y any
	if err := yaml.Unmarshal(raw, &y); err != nil {
		return nil, fmt.Errorf("contract: parse document: %w", err)
	}
	js, err := json.Marshal(y)
	if err != nil {
		// yaml decodes a mapping with a non-string key (an unquoted status
		// code, say) as map[any]any, which JSON can't encode.
		return nil, fmt.Errorf("contract: document is not JSON-compatible (quote status codes and other non-string keys): %w", err)
	}
	v, err := jsonschema.UnmarshalJSON(bytes.NewReader(js))
	if err != nil {
		return nil, fmt.Errorf("contract: reparse document: %w", err)
	}
	doc, ok := v.(map[string]any)
	if !ok {
		return nil, errors.New("contract: document is not an object")
	}
	return doc, nil
}

func validateDocument(doc map[string]any) error {
	meta, err := jsonschema.UnmarshalJSON(bytes.NewReader(oasSchema))
	if err != nil {
		return fmt.Errorf("contract: read the OpenAPI 3.1 schema: %w", err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource(oasSchemaURL, meta); err != nil {
		return err
	}
	sch, err := c.Compile(oasSchemaURL)
	if err != nil {
		return fmt.Errorf("contract: compile the OpenAPI 3.1 schema: %w", err)
	}
	if err := sch.Validate(doc); err != nil {
		return fmt.Errorf("contract: not a valid OpenAPI 3.1 document: %w", err)
	}
	return nil
}

// checkSchemas validates every component schema against the JSON Schema
// 2020-12 meta-schema and compiles it, so a typo in a keyword's value fails
// here rather than silently weakening a check.
func (s *Spec) checkSchemas() error {
	mc := jsonschema.NewCompiler()
	meta, err := mc.Compile(metaSchema)
	if err != nil {
		return fmt.Errorf("contract: compile the 2020-12 meta-schema: %w", err)
	}
	schemas := mapAt(s.doc, "components", "schemas")
	names := sortedKeys(schemas)
	for _, name := range names {
		if err := meta.Validate(schemas[name]); err != nil {
			return fmt.Errorf("contract: schema %s is not valid JSON Schema: %w", name, err)
		}
		if _, err := s.compile("/components/schemas/" + escape(name)); err != nil {
			return err
		}
	}
	return nil
}

func (s *Spec) compile(ptr string) (*jsonschema.Schema, error) {
	sch, err := s.compiler.Compile(docURL + "#" + urlPointer(ptr))
	if err != nil {
		return nil, fmt.Errorf("contract: compile schema at %s: %w", ptr, err)
	}
	return sch, nil
}

var methods = []string{"get", "put", "post", "delete", "options", "head", "patch", "trace"}

func (s *Spec) indexOperations() error {
	paths := mapAt(s.doc, "paths")
	for _, path := range sortedKeys(paths) {
		item, _ := paths[path].(map[string]any)
		itemPtr := "/paths/" + escape(path)
		for _, m := range methods {
			raw, ok := item[m].(map[string]any)
			if !ok {
				continue
			}
			op := &Operation{
				Method: strings.ToUpper(m), Path: path, segments: parseTemplate(path),
				responses: map[int]*response{}, pointer: itemPtr + "/" + m,
			}
			op.ID, _ = raw["operationId"].(string)
			if op.ID == "" {
				return fmt.Errorf("contract: %s %s has no operationId", op.Method, path)
			}
			if err := s.indexParameters(op, item, itemPtr, raw); err != nil {
				return err
			}
			if err := s.indexBody(op, raw); err != nil {
				return err
			}
			if err := s.indexResponses(op, raw); err != nil {
				return err
			}
			s.ops = append(s.ops, op)
		}
	}
	return nil
}

// indexParameters merges path-item and operation parameters; an
// operation's parameter overrides the path item's of the same name and
// location.
func (s *Spec) indexParameters(op *Operation, item map[string]any, itemPtr string, raw map[string]any) error {
	type src struct {
		list []any
		ptr  string
	}
	for _, from := range []src{{asList(item["parameters"]), itemPtr + "/parameters"}, {asList(raw["parameters"]), op.pointer + "/parameters"}} {
		for i, p := range from.list {
			obj, ptr, err := s.deref(p, fmt.Sprintf("%s/%d", from.ptr, i))
			if err != nil {
				return err
			}
			name, _ := obj["name"].(string)
			in, _ := obj["in"].(string)
			sch, err := s.compile(ptr + "/schema")
			if err != nil {
				return err
			}
			required, _ := obj["required"].(bool)
			schemaObj, _ := obj["schema"].(map[string]any)
			array := false
			if schemaObj != nil {
				resolved, _, err := s.deref(schemaObj, ptr+"/schema")
				if err != nil {
					return err
				}
				array = resolved["type"] == "array"
			}
			np := &parameter{name: name, in: in, required: required || in == "path", array: array, schema: sch}
			op.params = slices.DeleteFunc(op.params, func(x *parameter) bool { return x.name == name && x.in == in })
			op.params = append(op.params, np)
		}
	}
	return nil
}

func (s *Spec) indexBody(op *Operation, raw map[string]any) error {
	rb, ok := raw["requestBody"]
	if !ok {
		return nil
	}
	obj, ptr, err := s.deref(rb, op.pointer+"/requestBody")
	if err != nil {
		return err
	}
	content := mapAt(obj, "content")
	if _, ok := content["application/json"]; !ok || len(content) != 1 {
		return fmt.Errorf("contract: %s: request bodies must be application/json only", op.ID)
	}
	sch, err := s.compile(ptr + "/content/application~1json/schema")
	if err != nil {
		return err
	}
	required, _ := obj["required"].(bool)
	op.body = &requestBody{required: required, schema: sch}
	return nil
}

func (s *Spec) indexResponses(op *Operation, raw map[string]any) error {
	responses := mapAt(raw, "responses")
	for _, code := range sortedKeys(responses) {
		var status int
		if _, err := fmt.Sscanf(code, "%d", &status); err != nil || status < 100 || status > 599 || fmt.Sprint(status) != code {
			return fmt.Errorf("contract: %s: response %q must be an explicit status code", op.ID, code)
		}
		obj, ptr, err := s.deref(responses[code], op.pointer+"/responses/"+code)
		if err != nil {
			return err
		}
		resp := &response{headers: map[string]*header{}}
		if content := mapAt(obj, "content"); len(content) > 0 {
			if _, ok := content["application/json"]; !ok || len(content) != 1 {
				return fmt.Errorf("contract: %s %s: responses must be application/json only", op.ID, code)
			}
			if resp.schema, err = s.compile(ptr + "/content/application~1json/schema"); err != nil {
				return err
			}
		}
		headers := mapAt(obj, "headers")
		for _, name := range sortedKeys(headers) {
			h, hptr, err := s.deref(headers[name], ptr+"/headers/"+escape(name))
			if err != nil {
				return err
			}
			sch, err := s.compile(hptr + "/schema")
			if err != nil {
				return err
			}
			required, _ := h["required"].(bool)
			resp.headers[http.CanonicalHeaderKey(name)] = &header{name: name, required: required, schema: sch}
		}
		op.responses[status] = resp
	}
	return nil
}

// deref follows a $ref to a component (parameter, response, header,
// request body or schema) and returns the object with its JSON pointer.
func (s *Spec) deref(v any, ptr string) (map[string]any, string, error) {
	for range 8 {
		obj, ok := v.(map[string]any)
		if !ok {
			return nil, "", fmt.Errorf("contract: %s is not an object", ptr)
		}
		ref, ok := obj["$ref"].(string)
		if !ok {
			return obj, ptr, nil
		}
		if !strings.HasPrefix(ref, "#/") {
			return nil, "", fmt.Errorf("contract: %s: only local $refs are supported, got %q", ptr, ref)
		}
		ptr = ref[1:]
		if v = s.at(ptr); v == nil {
			return nil, "", fmt.Errorf("contract: $ref %q does not resolve", ref)
		}
	}
	return nil, "", fmt.Errorf("contract: %s: $ref chain too long", ptr)
}

// at returns the value at a JSON pointer, or nil.
func (s *Spec) at(ptr string) any {
	var cur any = s.doc
	for _, tok := range strings.Split(strings.TrimPrefix(ptr, "/"), "/") {
		obj, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = obj[unescape(tok)]
	}
	return cur
}

// Operations lists every documented operation, sorted by path then method.
func (s *Spec) Operations() []*Operation { return slices.Clone(s.ops) }

// Doc returns the parsed document (JSON data model). Callers must not
// modify it.
func (s *Spec) Doc() map[string]any { return s.doc }

// Find returns the operation for a request line, or an error saying why
// none matches. Among templates that match, the most specific wins (the
// most literal characters), so /v2/memories/{ref}:keep beats
// /v2/memories/{ref} for "M-0001:keep".
func (s *Spec) Find(method, path string) (*Operation, map[string]string, error) {
	var best *Operation
	var bestParams map[string]string
	bestScore := -1
	pathMatched := false
	for _, op := range s.ops {
		params, score, ok := op.match(path)
		if !ok {
			continue
		}
		pathMatched = true
		if op.Method != strings.ToUpper(method) || score <= bestScore {
			continue
		}
		best, bestParams, bestScore = op, params, score
	}
	switch {
	case best != nil:
		return best, bestParams, nil
	case pathMatched:
		return nil, nil, fmt.Errorf("method %s is not documented for %s", method, path)
	}
	return nil, nil, fmt.Errorf("path %s is not documented", path)
}

func parseTemplate(path string) []segment {
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	out := make([]segment, len(parts))
	for i, p := range parts {
		open, close := strings.IndexByte(p, '{'), strings.IndexByte(p, '}')
		if open < 0 || close < open {
			out[i] = segment{prefix: p}
			continue
		}
		out[i] = segment{prefix: p[:open], param: p[open+1 : close], suffix: p[close+1:]}
	}
	return out
}

func (op *Operation) match(path string) (map[string]string, int, bool) {
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(parts) != len(op.segments) {
		return nil, 0, false
	}
	params := map[string]string{}
	score := 0
	for i, seg := range op.segments {
		p := parts[i]
		if seg.param == "" {
			if p != seg.prefix {
				return nil, 0, false
			}
			score += len(p)
			continue
		}
		if !strings.HasPrefix(p, seg.prefix) || !strings.HasSuffix(p, seg.suffix) || len(p) <= len(seg.prefix)+len(seg.suffix) {
			return nil, 0, false
		}
		params[seg.param] = p[len(seg.prefix) : len(p)-len(seg.suffix)]
		score += len(seg.prefix) + len(seg.suffix)
	}
	return params, score, true
}

// ValidateRequest checks a request (with its body, already read) and
// returns its operation. The operation is returned even when the request
// is invalid, so the response can still be checked.
func (s *Spec) ValidateRequest(r *http.Request, body []byte) (*Operation, error) {
	op, pathParams, err := s.Find(r.Method, r.URL.Path)
	if err != nil {
		return nil, err
	}
	var errs []error
	query := r.URL.Query()
	documented := map[string]bool{}
	for _, p := range op.params {
		switch p.in {
		case "path":
			errs = append(errs, check(p.schema, pathParams[p.name], "path parameter "+p.name))
		case "query":
			documented[p.name] = true
			vals, ok := query[p.name]
			switch {
			case !ok && p.required:
				errs = append(errs, fmt.Errorf("query parameter %s is required", p.name))
			case !ok:
			case p.array:
				items := make([]any, len(vals))
				for i, v := range vals {
					items[i] = v
				}
				errs = append(errs, check(p.schema, items, "query parameter "+p.name))
			case len(vals) > 1:
				errs = append(errs, fmt.Errorf("query parameter %s is not repeatable", p.name))
			default:
				errs = append(errs, check(p.schema, scalar(vals[0]), "query parameter "+p.name))
			}
		case "header":
			v := r.Header.Get(p.name)
			switch {
			case v == "" && p.required:
				errs = append(errs, fmt.Errorf("header %s is required", p.name))
			case v != "":
				errs = append(errs, check(p.schema, v, "header "+p.name))
			}
		}
	}
	for name := range query {
		if !documented[name] {
			errs = append(errs, fmt.Errorf("query parameter %s is not documented", name))
		}
	}
	switch {
	case op.body == nil && len(body) > 0:
		errs = append(errs, errors.New("the operation takes no request body"))
	case op.body != nil && len(body) == 0 && op.body.required:
		errs = append(errs, errors.New("the request body is required"))
	case op.body != nil && len(body) > 0:
		if ct := r.Header.Get("Content-Type"); !isJSON(ct) {
			errs = append(errs, fmt.Errorf("request Content-Type is %q, want application/json", ct))
		}
		errs = append(errs, checkJSON(op.body.schema, body, "request body"))
	}
	return op, errors.Join(errs...)
}

// ValidateResponse checks one response to op and records the status as
// exercised.
func (s *Spec) ValidateResponse(op *Operation, status int, h http.Header, body []byte) error {
	s.mu.Lock()
	if s.seen[op.ID] == nil {
		s.seen[op.ID] = map[int]bool{}
	}
	s.seen[op.ID][status] = true
	s.mu.Unlock()

	resp, ok := op.responses[status]
	if !ok {
		return fmt.Errorf("%s: status %d is not documented (documented: %v)", op.ID, status, op.statuses())
	}
	var errs []error
	for name, hd := range resp.headers {
		v := h.Get(name)
		switch {
		case v == "" && hd.required:
			errs = append(errs, fmt.Errorf("%s %d: header %s is required", op.ID, status, hd.name))
		case v != "":
			errs = append(errs, check(hd.schema, v, fmt.Sprintf("%s %d: header %s", op.ID, status, hd.name)))
		}
	}
	switch {
	case resp.schema == nil && len(bytes.TrimSpace(body)) > 0:
		errs = append(errs, fmt.Errorf("%s %d: documented without a body, got %d bytes", op.ID, status, len(body)))
	case resp.schema != nil:
		if ct := h.Get("Content-Type"); !isJSON(ct) {
			errs = append(errs, fmt.Errorf("%s %d: Content-Type is %q, want application/json", op.ID, status, ct))
		}
		errs = append(errs, checkJSON(resp.schema, body, fmt.Sprintf("%s %d: body", op.ID, status)))
	}
	return errors.Join(errs...)
}

func (op *Operation) statuses() []int {
	out := make([]int, 0, len(op.responses))
	for code := range op.responses {
		out = append(out, code)
	}
	sort.Ints(out)
	return out
}

// Exercised reports the statuses seen for each operation so far.
func (s *Spec) Exercised() map[string][]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string][]int{}
	for id, codes := range s.seen {
		for code := range codes {
			out[id] = append(out[id], code)
		}
		sort.Ints(out[id])
	}
	return out
}

// Reporter is the part of testing.TB that Handler uses.
type Reporter interface {
	Helper()
	Errorf(format string, args ...any)
}

type invalidKey struct{}

// ExpectInvalidRequest marks a request as deliberately off-contract (a
// missing Idempotency-Key, say), so Handler reports it only if it turns
// out to be valid. Its response is still checked.
func ExpectInvalidRequest(r *http.Request) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), invalidKey{}, true))
}

// Handler wraps next so every exchange through it is checked against the
// spec. Violations are reported with t.Errorf, so a test fails on any
// undocumented path, method, status, header or field.
func (s *Spec) Handler(t Reporter, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("contract: read request body: %v", err)
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		op, reqErr := s.ValidateRequest(r, body)
		expectInvalid := r.Context().Value(invalidKey{}) != nil
		switch {
		case reqErr != nil && !expectInvalid:
			t.Errorf("contract: request %s %s breaks the spec: %v", r.Method, r.URL, reqErr)
		case reqErr == nil && expectInvalid:
			t.Errorf("contract: request %s %s was marked invalid but matches the spec", r.Method, r.URL)
		}
		rec := &recorder{header: http.Header{}, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		if op != nil {
			if err := s.ValidateResponse(op, rec.status, rec.header, rec.body.Bytes()); err != nil {
				t.Errorf("contract: response to %s %s breaks the spec: %v\nbody: %s", r.Method, r.URL, err, rec.body.String())
			}
		}
		for k, v := range rec.header {
			w.Header()[k] = v
		}
		w.WriteHeader(rec.status)
		_, _ = w.Write(rec.body.Bytes())
	})
}

type recorder struct {
	header      http.Header
	body        bytes.Buffer
	status      int
	wroteHeader bool
}

func (r *recorder) Header() http.Header { return r.header }

func (r *recorder) WriteHeader(code int) {
	if !r.wroteHeader {
		r.status, r.wroteHeader = code, true
	}
}

func (r *recorder) Write(b []byte) (int, error) {
	r.WriteHeader(http.StatusOK)
	return r.body.Write(b)
}

func check(sch *jsonschema.Schema, v any, what string) error {
	if err := sch.Validate(v); err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	return nil
}

func checkJSON(sch *jsonschema.Schema, body []byte, what string) error {
	v, err := jsonschema.UnmarshalJSON(bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("%s is not JSON: %w", what, err)
	}
	return check(sch, v, what)
}

// scalar converts a query value to a JSON number when it looks like one,
// so integer parameters validate; anything else stays a string.
func scalar(v string) any {
	if n := json.Number(v); v != "" {
		if _, err := n.Int64(); err == nil {
			return n
		}
	}
	return v
}

func isJSON(contentType string) bool {
	mt, _, _ := strings.Cut(contentType, ";")
	return strings.TrimSpace(strings.ToLower(mt)) == "application/json"
}

func mapAt(v any, keys ...string) map[string]any {
	for _, k := range keys {
		obj, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = obj[k]
	}
	obj, _ := v.(map[string]any)
	return obj
}

func asList(v any) []any {
	l, _ := v.([]any)
	return l
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// escape and unescape are JSON pointer token escaping (RFC 6901).
func escape(tok string) string {
	return strings.ReplaceAll(strings.ReplaceAll(tok, "~", "~0"), "/", "~1")
}

func unescape(tok string) string {
	return strings.ReplaceAll(strings.ReplaceAll(tok, "~1", "/"), "~0", "~")
}

// urlPointer percent-encodes a JSON pointer for use as a URL fragment
// (path templates contain braces).
func urlPointer(ptr string) string {
	toks := strings.Split(ptr, "/")
	for i, t := range toks {
		toks[i] = url.PathEscape(t)
	}
	return strings.Join(toks, "/")
}
