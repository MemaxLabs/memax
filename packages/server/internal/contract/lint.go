package contract

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// Lint checks the conventions the /v2 contract keeps on top of OpenAPI
// itself (they are listed at the top of v2.yaml). Each rule exists to make
// a class of drift impossible:
//
//   - Every object schema is closed. A schema with properties sets
//     additionalProperties: false, and a free-form object says
//     additionalProperties: true out loud. Without this an undocumented
//     field would validate, and the server could grow fields the SDK
//     doesn't know about.
//   - Every response body is a named schema, so the SDK gets a named type.
//     2xx bodies are envelopes with exactly `data`; 4xx and 5xx bodies are
//     ErrorEnvelope. That is the model.ApiResponse contract in AGENTS.md.
//   - Every POST and PATCH requires Idempotency-Key (commands are retried),
//     except a POST marked x-memax-read: true, which only reads and is a
//     POST to keep its input out of URLs (the near-duplicate check).
//   - Every operation documents 401, 429, 500 and 503, which the middleware
//     chain can answer for any route.
//   - Operations have unique ids, a summary and a declared tag; path
//     templates and path parameters agree; every component schema is used.
//   - No OpenAPI 3.0 leftovers (`nullable`), which 3.1 silently ignores.
func (s *Spec) Lint() []error {
	var errs []error
	add := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }

	if v, _ := s.doc["openapi"].(string); !strings.HasPrefix(v, "3.1.") {
		add("openapi is %q, want 3.1.x", v)
	}
	tags := map[string]bool{}
	for _, t := range asList(s.doc["tags"]) {
		if name, ok := mapAt(t)["name"].(string); ok {
			tags[name] = true
		}
	}

	ids := map[string]bool{}
	for _, op := range s.ops {
		raw := mapAt(s.at(op.pointer))
		where := op.Method + " " + op.Path
		if !strings.HasPrefix(op.Path, "/v2/") {
			add("%s: every path is under /v2/", where)
		}
		if ids[op.ID] {
			add("%s: operationId %s is used twice", where, op.ID)
		}
		ids[op.ID] = true
		if s, _ := raw["summary"].(string); s == "" {
			add("%s: needs a summary", where)
		}
		opTags := asList(raw["tags"])
		if len(opTags) == 0 {
			add("%s: needs a tag", where)
		}
		for _, t := range opTags {
			if name, _ := t.(string); !tags[name] {
				add("%s: tag %v is not declared at the top level", where, t)
			}
		}
		errs = append(errs, s.lintPathParams(op)...)
		readOnly, _ := raw["x-memax-read"].(bool)
		switch {
		case readOnly && op.Method != "POST":
			add("%s: x-memax-read marks a POST that only reads; drop it from %s", where, op.Method)
		case readOnly && op.hasRequiredHeader("Idempotency-Key"):
			add("%s: a read (x-memax-read) takes no Idempotency-Key", where)
		case (op.Method == "POST" || op.Method == "PATCH") && !readOnly && !op.hasRequiredHeader("Idempotency-Key"):
			add("%s: commands require the Idempotency-Key header", where)
		}
		for _, code := range []int{401, 429, 500, 503} {
			if _, ok := op.responses[code]; !ok {
				add("%s: document %d", where, code)
			}
		}
		errs = append(errs, s.lintResponses(op, raw)...)
	}

	schemas := mapAt(s.doc, "components", "schemas")
	for _, name := range sortedKeys(schemas) {
		errs = append(errs, lintSchema("#/components/schemas/"+name, schemas[name])...)
	}
	errs = append(errs, s.lintUnused()...)
	return errs
}

func (op *Operation) hasRequiredHeader(name string) bool {
	return slices.ContainsFunc(op.params, func(p *parameter) bool {
		return p.in == "header" && strings.EqualFold(p.name, name) && p.required
	})
}

func (s *Spec) lintPathParams(op *Operation) []error {
	var errs []error
	inTemplate := map[string]bool{}
	for _, seg := range op.segments {
		if seg.param != "" {
			inTemplate[seg.param] = true
		}
	}
	declared := map[string]bool{}
	for _, p := range op.params {
		if p.in == "path" {
			declared[p.name] = true
			if !inTemplate[p.name] {
				errs = append(errs, fmt.Errorf("%s %s: path parameter %s is not in the template", op.Method, op.Path, p.name))
			}
		}
	}
	for name := range inTemplate {
		if !declared[name] {
			errs = append(errs, fmt.Errorf("%s %s: template parameter {%s} is not declared", op.Method, op.Path, name))
		}
	}
	return errs
}

var componentSchemaRef = regexp.MustCompile(`^#/components/schemas/([A-Za-z0-9]+)$`)

func (s *Spec) lintResponses(op *Operation, raw map[string]any) []error {
	var errs []error
	responses := mapAt(raw, "responses")
	for code, r := range responses {
		obj, _, err := s.deref(r, "")
		if err != nil {
			errs = append(errs, err)
			continue
		}
		media := mapAt(obj, "content", "application/json")
		if media == nil {
			if code != "204" {
				errs = append(errs, fmt.Errorf("%s %s: response %s needs an application/json body", op.Method, op.Path, code))
			}
			continue
		}
		ref, _ := mapAt(media, "schema")["$ref"].(string)
		m := componentSchemaRef.FindStringSubmatch(ref)
		if m == nil {
			errs = append(errs, fmt.Errorf("%s %s: response %s must use a named schema from #/components/schemas", op.Method, op.Path, code))
			continue
		}
		name := m[1]
		if strings.HasPrefix(code, "2") {
			env := mapAt(s.doc, "components", "schemas", name)
			props := mapAt(env, "properties")
			req := asList(env["required"])
			if len(props) != 1 || props["data"] == nil || len(req) != 1 || req[0] != "data" {
				errs = append(errs, fmt.Errorf("%s %s: response %s schema %s must be an envelope with exactly a required `data`", op.Method, op.Path, code, name))
			}
		} else if name != "ErrorEnvelope" {
			errs = append(errs, fmt.Errorf("%s %s: error response %s must use ErrorEnvelope, not %s", op.Method, op.Path, code, name))
		}
	}
	return errs
}

// lintSchema walks one schema (and its subschemas) for the closed-object
// and 3.1 rules.
func lintSchema(where string, v any) []error {
	obj, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	var errs []error
	if _, ok := obj["nullable"]; ok {
		errs = append(errs, fmt.Errorf("%s: `nullable` is OpenAPI 3.0; use type: [T, \"null\"]", where))
	}
	props := mapAt(obj, "properties")
	ap, hasAP := obj["additionalProperties"]
	isObject := obj["type"] == "object" || props != nil
	switch {
	case isObject && props != nil && ap != false:
		errs = append(errs, fmt.Errorf("%s: an object with properties must set additionalProperties: false", where))
	case isObject && props == nil && !hasAP:
		errs = append(errs, fmt.Errorf("%s: a free-form object must say additionalProperties: true", where))
	}
	for _, r := range asList(obj["required"]) {
		if name, _ := r.(string); props[name] == nil {
			errs = append(errs, fmt.Errorf("%s: required property %v is not defined", where, r))
		}
	}
	for _, name := range sortedKeys(props) {
		errs = append(errs, lintSchema(where+"/properties/"+name, props[name])...)
	}
	for _, key := range []string{"items", "additionalProperties", "not"} {
		errs = append(errs, lintSchema(where+"/"+key, obj[key])...)
	}
	for _, key := range []string{"allOf", "anyOf", "oneOf", "prefixItems"} {
		for i, sub := range asList(obj[key]) {
			errs = append(errs, lintSchema(fmt.Sprintf("%s/%s/%d", where, key, i), sub)...)
		}
	}
	return errs
}

// lintUnused reports component schemas nothing refers to: a schema the
// handlers no longer use is drift too.
func (s *Spec) lintUnused() []error {
	used := map[string]bool{}
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if ref, ok := x["$ref"].(string); ok {
				if m := componentSchemaRef.FindStringSubmatch(ref); m != nil {
					used[m[1]] = true
				}
			}
			for _, child := range x {
				walk(child)
			}
		case []any:
			for _, child := range x {
				walk(child)
			}
		}
	}
	walk(s.doc)
	var errs []error
	for _, name := range sortedKeys(mapAt(s.doc, "components", "schemas")) {
		if !used[name] {
			errs = append(errs, fmt.Errorf("#/components/schemas/%s is never used", name))
		}
	}
	return errs
}
