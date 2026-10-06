// Package sealer runs the receipt hash chain (plan 25 §5.3, epic 1.1) in
// the worker, off the write path:
//
//   - seal_sweep, every SEALER_INTERVAL (10–60 s, default 15 s), moves a
//     cursor over receipts whose transactions have ended and queues a
//     seal_space job for each space it passes (ledger.SealSweep);
//   - seal_space, unique per space, chains that space's new receipts into
//     a checkpoint signed with RECEIPT_SIGNING_KEY (ledger.SealSpace), then
//     uploads the checkpoint to object storage, best-effort;
//   - receipts_verify_sweep, nightly, queues receipts_verify for every
//     space with receipts, which recomputes the chain from genesis and
//     checks every checkpoint, root and signature (ledger.VerifySpace),
//     logs and meters any mismatch loudly, and retries pending uploads.
//
// The database row of a checkpoint is the source of truth; the copy in
// object storage is a second, independent one. In production the bucket
// holds them under an object lock (write once, read many), so rewriting the
// database can't rewrite the copies, and the signing key lives in a KMS
// (receiptchain.Signer is the seam).
package sealer

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/objectstore"
	"github.com/MemaxLabs/memax/packages/server/internal/receiptchain"
)

// The cadence and sizes.
const (
	DefaultInterval = 15 * time.Second
	MinInterval     = 10 * time.Second
	MaxInterval     = 60 * time.Second
	// DefaultBatch is the most receipts one checkpoint covers.
	DefaultBatch = 2000
	// SweepBatch is how many receipts one sweep moves its cursor over.
	SweepBatch = 10000
	// maxBatchesPerJob bounds one seal_space job; more is a snooze.
	maxBatchesPerJob = 25
	// UploadAttempts is how often a seal job retries a checkpoint's
	// upload; the nightly verifier retries every pending one regardless.
	UploadAttempts = 5
	// LagWarning: receipts waiting longer than this to be sealed are
	// logged as the sealer falling behind.
	LagWarning = 10 * time.Minute
	// EnvInterval sets the sweep's cadence ("15s").
	EnvInterval = "SEALER_INTERVAL"
)

// Config configures the sealer.
type Config struct {
	Interval time.Duration
	Batch    int
	// Signer signs checkpoints; nil leaves them unsigned (and says so).
	Signer receiptchain.Signer
	// Keys verify checkpoints: the signer's key and retired ones.
	Keys receiptchain.Keyring
	// Store holds checkpoint copies; nil keeps them in the database only.
	Store objectstore.Store
	Log   *slog.Logger
}

// ConfigFromEnv reads SEALER_INTERVAL, RECEIPT_SIGNING_KEY and
// RECEIPT_VERIFY_KEYS.
func ConfigFromEnv(getenv func(string) string) (Config, error) {
	cfg := Config{Interval: DefaultInterval, Batch: DefaultBatch}
	if raw := strings.TrimSpace(getenv(EnvInterval)); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil {
			return cfg, fmt.Errorf("%s: %w", EnvInterval, err)
		}
		cfg.Interval = d
	}
	cfg.Interval = min(max(cfg.Interval, MinInterval), MaxInterval)
	signer, keys, err := receiptchain.FromEnv(getenv)
	if err != nil {
		return cfg, err
	}
	if signer != nil {
		cfg.Signer = signer
	}
	cfg.Keys = keys
	return cfg, nil
}

// Sealer seals and verifies spaces' receipt chains.
type Sealer struct {
	ledger *ledger.Ledger
	cfg    Config
	log    *slog.Logger
	m      meters

	// Counters, for logs and tests.
	Sealed, Checkpoints, Uploaded, UploadFailures, Problems atomic.Int64
}

// New returns a sealer on the ledger, or nil without one.
func New(l *ledger.Ledger, cfg Config) *Sealer {
	if l == nil {
		return nil
	}
	if cfg.Interval <= 0 {
		cfg.Interval = DefaultInterval
	}
	if cfg.Batch <= 0 {
		cfg.Batch = DefaultBatch
	}
	if cfg.Keys == nil {
		cfg.Keys = receiptchain.Keyring{}
	}
	if s, ok := cfg.Signer.(*receiptchain.Ed25519Signer); ok && s == nil {
		cfg.Signer = nil
	}
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
	return &Sealer{ledger: l, cfg: cfg, log: cfg.Log.With("component", "sealer"), m: newMeters()}
}

// Signed reports whether checkpoints are signed.
func (s *Sealer) Signed() bool { return s != nil && s.cfg.Signer != nil }

// Interval is the sweep's cadence.
func (s *Sealer) Interval() time.Duration { return s.cfg.Interval }

// ErrMore: a space had more new receipts than one job seals; snooze.
var ErrMore = errors.New("sealer: more receipts to seal")

// SealSpace seals the space's new receipts (up to maxBatchesPerJob
// checkpoints) and uploads what's pending. It returns ErrMore when
// receipts remain.
func (s *Sealer) SealSpace(ctx context.Context, space uuid.UUID) (int, error) {
	total := 0
	for range maxBatchesPerJob {
		res, err := s.ledger.SealSpace(ctx, space, s.cfg.Signer, s.cfg.Batch)
		if err != nil {
			return total, err
		}
		if res.Sealed > 0 {
			total += res.Sealed
			s.Sealed.Add(int64(res.Sealed))
			s.Checkpoints.Add(1)
			attrs := metric.WithAttributes(attribute.Bool("signed", res.Checkpoint.Signed))
			s.m.sealed.Add(ctx, int64(res.Sealed), attrs)
			s.m.checkpoints.Add(ctx, 1, attrs)
			s.m.lag.Record(ctx, res.Lag.Seconds())
			s.log.InfoContext(ctx, "sealer: sealed", "metric", "receipts_sealed", "space_id", space.String(),
				"checkpoint", res.Checkpoint.Number, "receipts", res.Sealed, "position", res.Checkpoint.PositionTo,
				"signed", res.Checkpoint.Signed, "lag_ms", res.Lag.Milliseconds())
			if res.Lag > LagWarning {
				s.log.WarnContext(ctx, "sealer: receipts waited long to be sealed", "metric", "receipts_seal_lag",
					"space_id", space.String(), "lag", res.Lag.String())
			}
		}
		if !res.More {
			s.Upload(ctx, space, UploadAttempts)
			return total, nil
		}
	}
	s.Upload(ctx, space, UploadAttempts)
	return total, ErrMore
}

// Upload copies the space's pending checkpoints to object storage, best
// effort: a failure is counted on the checkpoint and retried later.
func (s *Sealer) Upload(ctx context.Context, space uuid.UUID, maxAttempts int) {
	if s.cfg.Store == nil {
		return
	}
	pending, err := s.ledger.PendingUploads(ctx, space, 100, maxAttempts)
	if err != nil {
		s.log.WarnContext(ctx, "sealer: can't list checkpoints to upload", "space_id", space.String(), "error", err)
		return
	}
	for _, c := range pending {
		key := ledger.CheckpointKey(space, c.Number)
		raw, err := Document(c)
		if err == nil {
			err = s.cfg.Store.Put(ctx, objectstore.PutInput{Key: key, Body: bytes.NewReader(raw),
				ContentType: "application/json", SizeBytes: int64(len(raw))})
		}
		if err != nil {
			s.UploadFailures.Add(1)
			s.m.uploads.Add(ctx, 1, metric.WithAttributes(attribute.String("result", "failed")))
			s.log.WarnContext(ctx, "sealer: checkpoint upload failed; it stays in the database and is retried",
				"metric", "receipts_checkpoint_upload_failed", "space_id", space.String(), "checkpoint", c.Number, "error", err)
			if err := s.ledger.MarkUploaded(ctx, space, c.ID, "", err); err != nil {
				s.log.WarnContext(ctx, "sealer: can't count an upload failure", "error", err)
			}
			continue
		}
		if err := s.ledger.MarkUploaded(ctx, space, c.ID, key, nil); err != nil {
			s.log.WarnContext(ctx, "sealer: uploaded a checkpoint but can't record it", "checkpoint", c.Number, "error", err)
			continue
		}
		s.Uploaded.Add(1)
		s.m.uploads.Add(ctx, 1, metric.WithAttributes(attribute.String("result", "stored")))
	}
}

// Document is a checkpoint's copy in object storage: every field the
// signature covers, the signed statement itself (base64), and the format.
func Document(c ledger.Checkpoint) ([]byte, error) {
	return json.MarshalIndent(struct {
		Format    string `json:"format"`
		Statement string `json:"statement"`
		ledger.Checkpoint
	}{"memax.checkpoint.v1", base64.StdEncoding.EncodeToString(c.Chain().Statement()), c}, "", "  ")
}

// Verify recomputes the space's chain from genesis, checks every
// checkpoint, records the outcome on the head, and retries pending
// uploads. Mismatches are logged at error level and metered.
func (s *Sealer) Verify(ctx context.Context, space uuid.UUID) (*ledger.Verification, error) {
	v, err := s.ledger.VerifySpace(ctx, space, s.cfg.Keys)
	if err != nil {
		return nil, err
	}
	if err := s.ledger.RecordVerification(ctx, space, v); err != nil {
		return v, err
	}
	for _, p := range v.Problems {
		s.Problems.Add(1)
		s.m.problems.Add(ctx, 1, metric.WithAttributes(attribute.String("kind", string(p.Kind))))
	}
	attrs := []any{"space_id", space.String(), "receipts", v.Receipts, "checkpoints", v.Checkpoints,
		"signed", v.Signed, "unsigned", v.Unsigned, "unsealed", v.Unsealed}
	if !v.OK() {
		problems := make([]string, 0, min(len(v.Problems), 10))
		for _, p := range v.Problems[:min(len(v.Problems), 10)] {
			problems = append(problems, p.String())
		}
		s.log.ErrorContext(ctx, "sealer: RECEIPT CHAIN MISMATCH: the record doesn't match its signed checkpoints",
			append(attrs, "metric", "receipt_chain_mismatch", "problems", len(v.Problems), "first", problems)...)
	} else {
		s.log.InfoContext(ctx, "sealer: verified", append(attrs, "metric", "receipt_chain_verified")...)
	}
	if v.OldestUnsealed != nil && time.Since(*v.OldestUnsealed) > LagWarning {
		s.log.WarnContext(ctx, "sealer: receipts are waiting to be sealed", "metric", "receipts_seal_lag",
			"space_id", space.String(), "unsealed", v.Unsealed, "oldest", v.OldestUnsealed.UTC().Format(time.RFC3339))
	}
	s.Upload(ctx, space, 0)
	return v, nil
}

type meters struct {
	sealed, checkpoints, uploads, problems metric.Int64Counter
	lag                                    metric.Float64Histogram
}

func newMeters() meters {
	m := otel.Meter("memax.receipts")
	var out meters
	out.sealed, _ = m.Int64Counter("memax.receipts.sealed", metric.WithDescription("Receipts chained into checkpoints"))
	out.checkpoints, _ = m.Int64Counter("memax.receipts.checkpoints", metric.WithDescription("Checkpoints written"))
	out.uploads, _ = m.Int64Counter("memax.receipts.checkpoint.uploads",
		metric.WithDescription("Checkpoint copies to object storage, by result: stored or failed"))
	out.problems, _ = m.Int64Counter("memax.receipts.verify.problems",
		metric.WithDescription("Mismatches the verifier found, by kind; anything above 0 is an incident"))
	out.lag, _ = m.Float64Histogram("memax.receipts.seal.lag",
		metric.WithDescription("How long a receipt waited to be sealed"), metric.WithUnit("s"))
	return out
}
