package observability

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/trace"
)

const (
	defaultLokiQueueSize       = 2048
	defaultLokiBatchMaxRecords = 256
	defaultLokiBatchMaxBytes   = 512 * 1024
	defaultLokiBatchInterval   = 2 * time.Second
	defaultLokiRequestTimeout  = 5 * time.Second
	defaultLokiMaxRetries      = 3
)

type LokiSetup struct {
	Enabled  bool
	Handler  slog.Handler
	Shutdown func(context.Context) error
}

type lokiConfig struct {
	endpoint        string
	username        string
	password        string
	serviceName     string
	environment     string
	hostName        string
	processPID      int
	flyRegion       string
	flyMachineID    string
	flyAppName      string
	queueSize       int
	batchMaxRecords int
	batchMaxBytes   int
	batchInterval   time.Duration
	requestTimeout  time.Duration
	maxRetries      int
}

type lokiRecord struct {
	labels map[string]string
	line   string
	ts     time.Time
}

type lokiPushRequest struct {
	Streams []lokiStream `json:"streams"`
}

type lokiStream struct {
	Stream map[string]string `json:"stream"`
	Values [][2]string       `json:"values"`
}

type lokiExporter struct {
	cfg      lokiConfig
	client   *http.Client
	records  chan lokiRecord
	done     chan struct{}
	wg       sync.WaitGroup
	dropped  atomic.Int64
	sent     atomic.Int64
	failed   atomic.Int64
	shutdown atomic.Bool
}

type lokiHandler struct {
	exporter *lokiExporter
	attrs    []slog.Attr
	groups   []string
}

func SetupLoki(defaultServiceName string) (LokiSetup, error) {
	cfg, ok, err := newLokiConfig(defaultServiceName)
	if err != nil {
		return LokiSetup{}, err
	}
	if !ok {
		return LokiSetup{
			Enabled: false,
			Shutdown: func(context.Context) error {
				return nil
			},
		}, nil
	}

	exporter := newLokiExporter(cfg)
	return LokiSetup{
		Enabled:  true,
		Handler:  &lokiHandler{exporter: exporter},
		Shutdown: exporter.Shutdown,
	}, nil
}

func newLokiConfig(defaultServiceName string) (lokiConfig, bool, error) {
	endpoint := strings.TrimSpace(os.Getenv("LOKI_URL"))
	username := strings.TrimSpace(os.Getenv("LOKI_USERNAME"))
	password := strings.TrimSpace(os.Getenv("LOKI_PASSWORD"))
	if endpoint == "" && username == "" && password == "" {
		return lokiConfig{}, false, nil
	}
	if endpoint == "" || username == "" || password == "" {
		return lokiConfig{}, false, fmt.Errorf("incomplete Loki configuration: LOKI_URL, LOKI_USERNAME, and LOKI_PASSWORD must all be set")
	}

	queueSize, err := intEnv("LOKI_QUEUE_SIZE", defaultLokiQueueSize)
	if err != nil {
		return lokiConfig{}, false, err
	}
	batchMaxRecords, err := intEnv("LOKI_BATCH_MAX_RECORDS", defaultLokiBatchMaxRecords)
	if err != nil {
		return lokiConfig{}, false, err
	}
	batchMaxBytes, err := intEnv("LOKI_BATCH_MAX_BYTES", defaultLokiBatchMaxBytes)
	if err != nil {
		return lokiConfig{}, false, err
	}
	maxRetries, err := intEnv("LOKI_MAX_RETRIES", defaultLokiMaxRetries)
	if err != nil {
		return lokiConfig{}, false, err
	}
	batchInterval, err := durationMsEnv("LOKI_BATCH_INTERVAL_MS", defaultLokiBatchInterval)
	if err != nil {
		return lokiConfig{}, false, err
	}
	requestTimeout, err := durationMsEnv("LOKI_REQUEST_TIMEOUT_MS", defaultLokiRequestTimeout)
	if err != nil {
		return lokiConfig{}, false, err
	}

	endpoint = strings.TrimRight(endpoint, "/")
	if !strings.HasSuffix(endpoint, "/loki/api/v1/push") {
		endpoint += "/loki/api/v1/push"
	}

	hostName, err := os.Hostname()
	if err != nil {
		hostName = ""
	}

	return lokiConfig{
		endpoint:        endpoint,
		username:        username,
		password:        password,
		serviceName:     defaultServiceName,
		environment:     environmentName(),
		hostName:        hostName,
		processPID:      os.Getpid(),
		flyRegion:       strings.TrimSpace(os.Getenv("FLY_REGION")),
		flyMachineID:    strings.TrimSpace(os.Getenv("FLY_MACHINE_ID")),
		flyAppName:      strings.TrimSpace(os.Getenv("FLY_APP_NAME")),
		queueSize:       queueSize,
		batchMaxRecords: batchMaxRecords,
		batchMaxBytes:   batchMaxBytes,
		batchInterval:   batchInterval,
		requestTimeout:  requestTimeout,
		maxRetries:      maxRetries,
	}, true, nil
}

func newLokiExporter(cfg lokiConfig) *lokiExporter {
	exporter := &lokiExporter{
		cfg: cfg,
		client: &http.Client{
			Timeout: cfg.requestTimeout,
		},
		records: make(chan lokiRecord, cfg.queueSize),
		done:    make(chan struct{}),
	}
	exporter.wg.Add(1)
	go exporter.run()
	return exporter
}

func (e *lokiExporter) Shutdown(ctx context.Context) error {
	if !e.shutdown.CompareAndSwap(false, true) {
		return nil
	}
	close(e.done)
	done := make(chan struct{})
	go func() {
		e.wg.Wait()
		close(done)
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		fmt.Fprintf(os.Stderr, "loki exporter stopped sent=%d failed=%d dropped=%d\n", e.sent.Load(), e.failed.Load(), e.dropped.Load())
		return nil
	}
}

func (e *lokiExporter) Enqueue(record lokiRecord) {
	if e.shutdown.Load() {
		e.dropped.Add(1)
		return
	}
	select {
	case e.records <- record:
	default:
		dropped := e.dropped.Add(1)
		if dropped == 1 || dropped%100 == 0 {
			fmt.Fprintf(os.Stderr, "loki exporter dropping logs dropped=%d\n", dropped)
		}
	}
}

func (e *lokiExporter) run() {
	defer e.wg.Done()
	ticker := time.NewTicker(e.cfg.batchInterval)
	defer ticker.Stop()

	var batch []lokiRecord
	var batchBytes int
	flush := func() {
		if len(batch) == 0 {
			return
		}
		if err := e.send(batch); err != nil {
			e.failed.Add(int64(len(batch)))
			fmt.Fprintf(os.Stderr, "loki exporter send failed error=%v batch=%d\n", err, len(batch))
		} else {
			e.sent.Add(int64(len(batch)))
		}
		batch = nil
		batchBytes = 0
	}

	for {
		select {
		case <-e.done:
			for {
				select {
				case record := <-e.records:
					batch = append(batch, record)
				default:
					flush()
					return
				}
			}
		case <-ticker.C:
			flush()
		case record := <-e.records:
			batch = append(batch, record)
			batchBytes += len(record.line)
			if len(batch) >= e.cfg.batchMaxRecords || batchBytes >= e.cfg.batchMaxBytes {
				flush()
			}
		}
	}
}

func (e *lokiExporter) send(batch []lokiRecord) error {
	streamMap := make(map[string]*lokiStream)
	for _, record := range batch {
		key := labelsKey(record.labels)
		stream := streamMap[key]
		if stream == nil {
			labelsCopy := make(map[string]string, len(record.labels))
			for k, v := range record.labels {
				labelsCopy[k] = v
			}
			stream = &lokiStream{Stream: labelsCopy}
			streamMap[key] = stream
		}
		stream.Values = append(stream.Values, [2]string{
			strconv.FormatInt(record.ts.UnixNano(), 10),
			record.line,
		})
	}

	body := lokiPushRequest{Streams: make([]lokiStream, 0, len(streamMap))}
	for _, stream := range streamMap {
		body.Streams = append(body.Streams, *stream)
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}

	authHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte(e.cfg.username+":"+e.cfg.password))
	var lastErr error
	for attempt := 0; attempt < e.cfg.maxRetries; attempt++ {
		req, err := http.NewRequest(http.MethodPost, e.cfg.endpoint, bytes.NewReader(payload))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", authHeader)

		resp, err := e.client.Do(req)
		if err != nil {
			lastErr = err
		} else {
			respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 8*1024))
			resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				return nil
			}
			lastErr = fmt.Errorf("status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(respBody)))
			if resp.StatusCode < 500 && resp.StatusCode != http.StatusTooManyRequests {
				return lastErr
			}
		}
		time.Sleep(time.Duration(attempt+1) * 200 * time.Millisecond)
	}
	return lastErr
}

func (h *lokiHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= slog.LevelDebug
}

func (h *lokiHandler) Handle(ctx context.Context, record slog.Record) error {
	fields := map[string]any{
		"time":        record.Time.UTC().Format(time.RFC3339Nano),
		"level":       record.Level.String(),
		"message":     record.Message,
		"service":     h.exporter.cfg.serviceName,
		"environment": h.exporter.cfg.environment,
	}
	if h.exporter.cfg.hostName != "" {
		fields["host.name"] = h.exporter.cfg.hostName
	}
	if h.exporter.cfg.processPID > 0 {
		fields["process.pid"] = h.exporter.cfg.processPID
	}
	if h.exporter.cfg.flyRegion != "" {
		fields["fly.region"] = h.exporter.cfg.flyRegion
	}
	if h.exporter.cfg.flyMachineID != "" {
		fields["fly.machine.id"] = h.exporter.cfg.flyMachineID
	}
	if h.exporter.cfg.flyAppName != "" {
		fields["fly.app.name"] = h.exporter.cfg.flyAppName
	}
	for _, attr := range h.attrs {
		addField(fields, h.groups, attr)
	}
	record.Attrs(func(attr slog.Attr) bool {
		addField(fields, h.groups, attr)
		return true
	})
	if spanCtx := trace.SpanContextFromContext(ctx); spanCtx.IsValid() {
		fields["trace_id"] = spanCtx.TraceID().String()
		fields["span_id"] = spanCtx.SpanID().String()
	}
	line, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	labels := map[string]string{
		"service":     h.exporter.cfg.serviceName,
		"environment": h.exporter.cfg.environment,
		"level":       normalizeLabelValue(record.Level.String()),
	}
	if h.exporter.cfg.flyRegion != "" {
		labels["fly_region"] = normalizeLabelValue(h.exporter.cfg.flyRegion)
	}
	h.exporter.Enqueue(lokiRecord{
		labels: labels,
		line:   string(line),
		ts:     record.Time,
	})
	return nil
}

func (h *lokiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	merged := make([]slog.Attr, 0, len(h.attrs)+len(attrs))
	merged = append(merged, h.attrs...)
	merged = append(merged, attrs...)
	return &lokiHandler{exporter: h.exporter, attrs: merged, groups: append([]string(nil), h.groups...)}
}

func (h *lokiHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	groups := append(append([]string(nil), h.groups...), name)
	return &lokiHandler{exporter: h.exporter, attrs: append([]slog.Attr(nil), h.attrs...), groups: groups}
}

func addField(fields map[string]any, groups []string, attr slog.Attr) {
	attr.Value = attr.Value.Resolve()
	if attr.Equal(slog.Attr{}) {
		return
	}
	key := attr.Key
	if len(groups) > 0 {
		key = strings.Join(append(append([]string(nil), groups...), key), ".")
	}
	addResolvedAttr(fields, key, attr.Value)
}

func addResolvedAttr(fields map[string]any, key string, value slog.Value) {
	switch value.Kind() {
	case slog.KindGroup:
		for _, child := range value.Group() {
			addResolvedAttr(fields, key+"."+child.Key, child.Value.Resolve())
		}
	case slog.KindString:
		fields[key] = value.String()
	case slog.KindInt64:
		fields[key] = value.Int64()
	case slog.KindUint64:
		fields[key] = value.Uint64()
	case slog.KindFloat64:
		fields[key] = value.Float64()
	case slog.KindBool:
		fields[key] = value.Bool()
	case slog.KindDuration:
		fields[key] = value.Duration().String()
	case slog.KindTime:
		fields[key] = value.Time().UTC().Format(time.RFC3339Nano)
	case slog.KindAny:
		fields[key] = value.Any()
	default:
		fields[key] = value.String()
	}
}

func labelsKey(labels map[string]string) string {
	return labels["service"] + "|" + labels["environment"] + "|" + labels["level"]
}

func normalizeLabelValue(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return "unknown"
	}
	return strings.NewReplacer(" ", "_", "-", "_").Replace(value)
}
