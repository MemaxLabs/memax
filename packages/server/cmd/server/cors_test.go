package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MemaxLabs/memax/packages/server/internal/contract"
	"github.com/MemaxLabs/memax/packages/server/openapi"
)

// TestCORSCoversTheV2Contract: the web app calls the API from another
// origin, so every request header the /v2 spec defines must be allowed by
// the preflight, and every response header it documents must be exposed,
// or browsers drop them (a command without Idempotency-Key, an edit that
// can't read the ETag).
func TestCORSCoversTheV2Contract(t *testing.T) {
	t.Parallel()
	spec, err := contract.Load(openapi.V2)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	corsMiddleware(http.NotFoundHandler()).ServeHTTP(rec, httptest.NewRequest(http.MethodOptions, "/v2/spaces", nil))
	allowed := headerSet(rec.Header().Get("Access-Control-Allow-Headers"))
	exposed := headerSet(rec.Header().Get("Access-Control-Expose-Headers"))

	requestHeaders, responseHeaders := v2Headers(spec.Doc())
	if len(requestHeaders) == 0 || len(responseHeaders) == 0 {
		t.Fatalf("found no headers in v2.yaml (request %v, response %v)", requestHeaders, responseHeaders)
	}
	for _, h := range requestHeaders {
		if !allowed[strings.ToLower(h)] {
			t.Errorf("request header %s is in v2.yaml but not in Access-Control-Allow-Headers", h)
		}
	}
	for _, h := range responseHeaders {
		if !exposed[strings.ToLower(h)] {
			t.Errorf("response header %s is in v2.yaml but not in Access-Control-Expose-Headers", h)
		}
	}
}

func headerSet(list string) map[string]bool {
	out := map[string]bool{}
	for _, h := range strings.Split(list, ",") {
		out[strings.ToLower(strings.TrimSpace(h))] = true
	}
	return out
}

// v2Headers lists the header parameters and the documented response
// header names of the spec.
func v2Headers(doc map[string]any) (request, response []string) {
	obj := func(v any) map[string]any { m, _ := v.(map[string]any); return m }
	components := obj(doc["components"])
	addParam := func(p map[string]any) {
		if p["in"] == "header" {
			request = append(request, p["name"].(string))
		}
	}
	for _, p := range obj(components["parameters"]) {
		addParam(obj(p))
	}
	addResponse := func(r map[string]any) {
		for name := range obj(r["headers"]) {
			response = append(response, name)
		}
	}
	for _, r := range obj(components["responses"]) {
		addResponse(obj(r))
	}
	for _, item := range obj(doc["paths"]) {
		for key, op := range obj(item) {
			if key == "parameters" {
				for _, p := range op.([]any) {
					addParam(obj(p))
				}
				continue
			}
			for _, p := range asSlice(obj(op)["parameters"]) {
				addParam(obj(p))
			}
			for _, r := range obj(obj(op)["responses"]) {
				addResponse(obj(r))
			}
		}
	}
	return request, response
}

func asSlice(v any) []any { s, _ := v.([]any); return s }
