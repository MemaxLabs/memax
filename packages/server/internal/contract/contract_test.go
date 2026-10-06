package contract_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MemaxLabs/memax/packages/server/internal/contract"
	"github.com/MemaxLabs/memax/packages/server/openapi"
)

func loadV2(t *testing.T) *contract.Spec {
	t.Helper()
	spec, err := contract.Load(openapi.V2)
	if err != nil {
		t.Fatalf("v2.yaml does not load: %v", err)
	}
	return spec
}

// TestV2SpecIsValid is the spec's own check: it parses, validates against
// the OpenAPI 3.1 schema, every JSON Schema in it compiles against
// 2020-12, and it keeps the /v2 conventions.
func TestV2SpecIsValid(t *testing.T) {
	t.Parallel()
	spec := loadV2(t)
	for _, err := range spec.Lint() {
		t.Error(err)
	}
	if n := len(spec.Operations()); n < 9 {
		t.Errorf("found %d operations, want at least 9: did the index break?", n)
	}
}

func TestLoadRejectsBrokenDocuments(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"not openapi 3.1": "openapi: 3.0.3\ninfo: {title: x, version: '1'}\npaths: {}\n",
		"default response": `openapi: 3.1.1
info: {title: x, version: '1'}
paths:
  /v2/x:
    get:
      operationId: x
      responses:
        default: {description: anything}
`,
		"invalid schema keyword value": `openapi: 3.1.1
info: {title: x, version: '1'}
paths: {}
components:
  schemas:
    Bad: {type: object, minProperties: many}
`,
		"dangling ref": `openapi: 3.1.1
info: {title: x, version: '1'}
paths:
  /v2/x:
    get:
      operationId: x
      responses:
        '200':
          $ref: '#/components/responses/Missing'
`,
	}
	for name, doc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := contract.Load([]byte(doc)); err == nil {
				t.Fatal("Load accepted it")
			}
		})
	}
}

func TestLintCatchesOpenObjects(t *testing.T) {
	t.Parallel()
	spec, err := contract.Load([]byte(`openapi: 3.1.1
info: {title: x, version: '1'}
tags: [{name: t}]
paths:
  /v2/x:
    post:
      operationId: x
      summary: x
      tags: [t]
      responses:
        '200':
          description: ok
          content:
            application/json:
              schema: {type: object, properties: {data: {type: string}}}
components:
  schemas:
    Open:
      type: object
      properties: {a: {type: string, nullable: true}}
    Loose: {type: object}
`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got := map[string]bool{}
	for _, err := range spec.Lint() {
		got[err.Error()] = true
	}
	for _, want := range []string{
		"Idempotency-Key", "document 401", "named schema", "additionalProperties: false",
		"additionalProperties: true", "nullable", "never used",
	} {
		found := false
		for msg := range got {
			found = found || strings.Contains(msg, want)
		}
		if !found {
			t.Errorf("Lint missed %q; got %v", want, got)
		}
	}
}

// TestValidatorCatchesDrift proves the checks the handler tests rely on
// actually fire: each case breaks the contract in one way.
func TestValidatorCatchesDrift(t *testing.T) {
	t.Parallel()
	spec := loadV2(t)
	space := `{"id":"0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b","slug":"memax-v2","name":"memax-v2","kind":"project","role":"owner","can_forget":true}`
	jsonHeader := http.Header{"Content-Type": {"application/json"}}

	t.Run("valid exchange", func(t *testing.T) {
		t.Parallel()
		op := find(t, spec, "GET", "/v2/spaces")
		if err := spec.ValidateResponse(op, 200, jsonHeader, []byte(`{"data":{"items":[`+space+`]}}`)); err != nil {
			t.Fatalf("valid response rejected: %v", err)
		}
	})
	responseCases := map[string]struct {
		status int
		header http.Header
		body   string
		want   string
	}{
		"undocumented field": {200, jsonHeader, `{"data":{"items":[` + strings.Replace(space, `"role"`, `"secret":1,"role"`, 1) + `]}}`, "additional"},
		"missing field":      {200, jsonHeader, `{"data":{"items":[{"id":"0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b"}]}}`, "missing"},
		"no envelope":        {200, jsonHeader, `{"items":[]}`, "data"},
		"undocumented code":  {418, jsonHeader, `{"error":{"code":"internal_error","message":"x"}}`, "not documented"},
		"unknown error code": {500, jsonHeader, `{"error":{"code":"oops","message":"x"}}`, "code"},
		"wrong content type": {200, http.Header{"Content-Type": {"text/plain"}}, `{"data":{"items":[]}}`, "Content-Type"},
		"bad enum":           {200, jsonHeader, `{"data":{"items":[` + strings.Replace(space, `"owner"`, `"admin"`, 1) + `]}}`, "role"},
	}
	for name, c := range responseCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			op := find(t, spec, "GET", "/v2/spaces")
			err := spec.ValidateResponse(op, c.status, c.header, []byte(c.body))
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("got %v, want an error mentioning %q", err, c.want)
			}
		})
	}

	t.Run("missing required response header", func(t *testing.T) {
		t.Parallel()
		op := find(t, spec, "GET", "/v2/memories/M-0001")
		err := spec.ValidateResponse(op, 200, jsonHeader, []byte(`{"data":{}}`))
		if err == nil || !strings.Contains(err.Error(), "ETag") {
			t.Fatalf("got %v, want a missing ETag", err)
		}
	})

	requestCases := map[string]struct {
		method, target, body string
		header               http.Header
		want                 string
	}{
		"missing idempotency key": {"POST", "/v2/memories/M-0001:keep?space=memax-v2", "", http.Header{}, "Idempotency-Key"},
		"missing if-match":        {"POST", "/v2/memories/M-0001:edit?space=x", `{"statement":"x"}`, http.Header{"Idempotency-Key": {"k"}, "Content-Type": {"application/json"}}, "If-Match"},
		"undocumented query":      {"GET", "/v2/spaces?hub_id=x", "", http.Header{}, "hub_id"},
		"undocumented field":      {"POST", "/v2/spaces/memax-v2/memories", `{"statement":"x","section":"decisions","color":"red"}`, http.Header{"Idempotency-Key": {"k"}, "Content-Type": {"application/json"}}, "additional"},
		"bad query enum":          {"GET", "/v2/spaces/x/memories?state=archived", "", http.Header{}, "state"},
		"bad limit":               {"GET", "/v2/spaces/x/memories?limit=0", "", http.Header{}, "limit"},
		"undocumented path":       {"GET", "/v2/nowhere", "", http.Header{}, "not documented"},
		"undocumented method":     {"DELETE", "/v2/spaces", "", http.Header{}, "not documented"},
		"bad ref":                 {"GET", "/v2/memories/memory-one", "", http.Header{}, "ref"},
	}
	for name, c := range requestCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := httptest.NewRequest(c.method, c.target, strings.NewReader(c.body))
			r.Header = c.header
			_, err := spec.ValidateRequest(r, []byte(c.body))
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("got %v, want an error mentioning %q", err, c.want)
			}
		})
	}
}

func TestFindPrefersTheMostSpecificTemplate(t *testing.T) {
	t.Parallel()
	spec := loadV2(t)
	for target, want := range map[string]string{
		"/v2/memories/M-0001:keep":   "keepMemory",
		"/v2/memories/M-0001:edit":   "editMemory",
		"/v2/memories/M-0001:reject": "rejectMemory",
	} {
		op, params, err := spec.Find("POST", target)
		if err != nil || op.ID != want || params["ref"] != "M-0001" {
			t.Errorf("Find(POST %s) = %v %v %v, want %s with ref M-0001", target, op, params, err, want)
		}
	}
	if op, _, err := spec.Find("GET", "/v2/memories/M-0001"); err != nil || op.ID != "getMemory" {
		t.Errorf("Find(GET /v2/memories/M-0001) = %v, %v", op, err)
	}
}

// TestHandlerReportsViolations runs a deliberately wrong handler through
// Handler and checks the test double sees the errors.
func TestHandlerReportsViolations(t *testing.T) {
	t.Parallel()
	spec := loadV2(t)
	rep := &fakeReporter{}
	bad := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte(`{}`))
	})
	srv := httptest.NewServer(spec.Handler(rep, bad))
	defer srv.Close()
	res, err := http.Get(srv.URL + "/v2/spaces")
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusTeapot {
		t.Errorf("status passed through as %d", res.StatusCode)
	}
	if len(rep.errors) != 1 || !strings.Contains(rep.errors[0], "418 is not documented") {
		t.Errorf("reported %v", rep.errors)
	}
	if got := spec.Exercised()["listSpaces"]; len(got) != 1 || got[0] != 418 {
		t.Errorf("Exercised = %v", got)
	}

	// A request marked invalid that is in fact valid is reported too, so
	// the marker can't go stale.
	rep2 := &fakeReporter{}
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"items":[]}}`))
	})
	r := contract.ExpectInvalidRequest(httptest.NewRequest("GET", "/v2/spaces", nil))
	spec.Handler(rep2, ok).ServeHTTP(httptest.NewRecorder(), r)
	if len(rep2.errors) != 1 || !strings.Contains(rep2.errors[0], "marked invalid") {
		t.Errorf("reported %v", rep2.errors)
	}
}

type fakeReporter struct{ errors []string }

func (f *fakeReporter) Helper() {}
func (f *fakeReporter) Errorf(format string, args ...any) {
	f.errors = append(f.errors, fmt.Sprintf(format, args...))
}

func find(t *testing.T, spec *contract.Spec, method, path string) *contract.Operation {
	t.Helper()
	op, _, err := spec.Find(method, path)
	if err != nil {
		t.Fatalf("Find(%s %s): %v", method, path, err)
	}
	return op
}
