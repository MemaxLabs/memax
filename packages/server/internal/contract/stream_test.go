package contract_test

import (
	"bufio"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/contract"
)

// streamSpec documents one SSE operation: tick events then a done event.
const streamSpec = `openapi: 3.1.1
info: {title: x, version: '1'}
tags: [{name: t}]
paths:
  /v2/ticks:
    post:
      operationId: ticks
      summary: Ticks
      tags: [t]
      x-memax-read: true
      responses:
        '200':
          description: ticks
          content:
            text/event-stream:
              schema: {$ref: '#/components/schemas/TickEvent'}
        '401': {$ref: '#/components/responses/Err'}
        '429': {$ref: '#/components/responses/Err'}
        '500': {$ref: '#/components/responses/Err'}
        '503': {$ref: '#/components/responses/Err'}
components:
  responses:
    Err:
      description: error
      content:
        application/json:
          schema: {$ref: '#/components/schemas/ErrorEnvelope'}
  schemas:
    ErrorEnvelope:
      type: object
      additionalProperties: false
      required: [error]
      properties:
        error: {type: string}
    TickEvent:
      oneOf:
        - $ref: '#/components/schemas/TickTick'
        - $ref: '#/components/schemas/TickDone'
    TickTick:
      type: object
      additionalProperties: false
      required: [event, data]
      properties:
        event: {const: tick}
        data: {$ref: '#/components/schemas/Tick'}
    TickDone:
      type: object
      additionalProperties: false
      required: [event, data]
      properties:
        event: {const: done}
        data: {$ref: '#/components/schemas/Done'}
    Tick:
      type: object
      additionalProperties: false
      required: [n]
      properties:
        n: {type: integer}
    Done:
      type: object
      additionalProperties: false
      required: [total]
      properties:
        total: {type: integer}
`

func TestParseEvents(t *testing.T) {
	t.Parallel()
	events, err := contract.ParseEvents([]byte("\ufeff: a comment\r\nevent: tick\r\ndata: {\"n\":1}\r\n\r\n" +
		"data:{\"a\":\ndata: 2}\nid: 7\n\nevent: empty\n\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Name != "tick" || events[0].Data != `{"n":1}` ||
		events[1].Name != "message" || events[1].Data != "{\"a\":\n2}" || events[1].ID != "7" {
		t.Errorf("events = %+v", events)
	}
	if _, err := contract.ParseEvents([]byte("event: tick\ndata: {}\n")); err == nil {
		t.Error("a stream ending inside an event parsed")
	}
}

func TestStreamValidation(t *testing.T) {
	t.Parallel()
	spec, err := contract.Load([]byte(streamSpec))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range spec.Lint() {
		t.Error(e)
	}
	op, _, err := spec.Find("POST", "/v2/ticks")
	if err != nil {
		t.Fatal(err)
	}
	sse := http.Header{"Content-Type": {"text/event-stream"}}
	if err := spec.ValidateResponse(op, 200, sse, []byte("event: tick\ndata: {\"n\":1}\n\nevent: done\ndata: {\"total\":1}\n\n")); err != nil {
		t.Fatalf("a valid stream was refused: %v", err)
	}
	cases := map[string]struct {
		header http.Header
		body   string
		want   string
	}{
		"undocumented event":  {sse, "event: tock\ndata: {\"n\":1}\n\n", "event 0 (tock)"},
		"undocumented field":  {sse, "event: tick\ndata: {\"n\":1,\"x\":2}\n\n", "additional"},
		"wrong data":          {sse, "event: done\ndata: {\"n\":1}\n\n", "event 0 (done)"},
		"data not json":       {sse, "event: tick\ndata: one\n\n", "not JSON"},
		"undocumented id":     {sse, "event: tick\nid: 3\ndata: {\"n\":1}\n\n", "event 0"},
		"cut off":             {sse, "event: tick\ndata: {\"n\":1}\n", "ends inside"},
		"empty":               {sse, "", "no events"},
		"wrong content type":  {http.Header{"Content-Type": {"application/json"}}, "event: tick\ndata: {\"n\":1}\n\n", "Content-Type"},
		"unnamed event (msg)": {sse, "data: {\"n\":1}\n\n", "event 0 (message)"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := spec.ValidateResponse(op, 200, c.header, []byte(c.body))
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("got %v, want an error mentioning %q", err, c.want)
			}
		})
	}
}

func TestLintStreams(t *testing.T) {
	t.Parallel()
	bad := strings.NewReplacer(
		"event: {const: tick}", "event: {type: string}",
		"data: {$ref: '#/components/schemas/Done'}", "data: {type: object, additionalProperties: true}",
	).Replace(streamSpec)
	spec, err := contract.Load([]byte(bad))
	if err != nil {
		t.Fatal(err)
	}
	var msgs []string
	for _, e := range spec.Lint() {
		msgs = append(msgs, e.Error())
	}
	joined := strings.Join(msgs, "\n")
	for _, want := range []string{"`event` must be a const", "`data` must be a named schema"} {
		if !strings.Contains(joined, want) {
			t.Errorf("Lint missed %q; got:\n%s", want, joined)
		}
	}
}

// Handler passes an event stream through as it is written, so a client
// reads each event when it is flushed, and still checks it whole.
func TestHandlerStreamsEvents(t *testing.T) {
	t.Parallel()
	spec, err := contract.Load([]byte(streamSpec))
	if err != nil {
		t.Fatal(err)
	}
	rep := &fakeReporter{}
	release := make(chan struct{})
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("event: tick\ndata: {\"n\":1}\n\n"))
		if err := http.NewResponseController(w).Flush(); err != nil {
			t.Error(err)
		}
		<-release
		_, _ = w.Write([]byte("event: tock\ndata: {\"n\":2}\n\n"))
	})
	srv := httptest.NewServer(spec.Handler(rep, h))
	defer srv.Close()
	res, err := http.Post(srv.URL+"/v2/ticks", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	first := make(chan string, 1)
	go func() {
		line, _ := bufio.NewReader(res.Body).ReadString('\n')
		first <- line
	}()
	select {
	case line := <-first:
		if line != "event: tick\n" {
			t.Errorf("first line %q", line)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the first event didn't arrive before the handler finished")
	}
	close(release)
	srv.Close() // waits for the handler, which then reports
	if len(rep.errors) != 1 || !strings.Contains(rep.errors[0], "tock") {
		t.Errorf("reported %v, want the undocumented tock event", rep.errors)
	}
}
