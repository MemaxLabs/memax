package v2api

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// Cloud agents and their start keys (cloud warm-start design §4.3, §4.5).
// The handler makes the secret, gives the ledger only its SHA-256, and
// answers the secret once: a retry under the same key answers without it,
// since nothing kept it.

type createCloudAgentRequest struct {
	Agent    ledger.AgentKind `json:"agent"`
	Space    string           `json:"space"`
	Autonomy policy.Autonomy  `json:"autonomy"`
	Reason   string           `json:"reason"`
}

type cloudAgentResult struct {
	Outcome  ledger.Outcome     `json:"outcome"`
	Policy   policy.Decision    `json:"policy"`
	Agent    *ledger.Connection `json:"agent"`
	Key      *startKeyWire      `json:"key,omitempty"`
	Receipts []ledger.Receipt   `json:"receipts"`
}

type startKeyWire struct {
	Secret    string    `json:"secret"`
	Prefix    string    `json:"prefix"`
	ExpiresAt time.Time `json:"expires_at"`
}

// newStartKey makes a start key's secret, in V1's format (mxk_ and 64 hex
// characters), and what the ledger stores of it.
func (h *Handler) newStartKey() (string, ledger.StartKey, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", ledger.StartKey{}, err
	}
	secret := "mxk_" + hex.EncodeToString(raw)
	sum := sha256.Sum256([]byte(secret))
	return secret, ledger.StartKey{ID: uuid.Must(uuid.NewV7()), SHA256: hex.EncodeToString(sum[:]), Prefix: secret[:8],
		ExpiresAt: h.now().Add(ledger.StartKeyLifetime).UTC().Truncate(time.Microsecond)}, nil
}

// POST /v2/cloud-agents
func (h *Handler) createCloudAgent(w http.ResponseWriter, r *http.Request) {
	p, key, e := h.commandStart(r)
	if e != nil {
		writeError(w, e)
		return
	}
	var req createCloudAgentRequest
	if e := decodeBody(w, r, &req, true); e != nil {
		writeError(w, e)
		return
	}
	sp, e := h.space(r, p, req.Space)
	if e != nil {
		writeError(w, e)
		return
	}
	secret, sk, err := h.newStartKey()
	if err != nil {
		writeError(w, h.fromLedger(r, err))
		return
	}
	m := p.meta(p.scope, key, commandFields{Reason: req.Reason})
	res, err := h.ledger.Apply(r.Context(), &ledger.ConnectCloudAgent{Meta: m, Agent: req.Agent, SpaceID: sp.SpaceID,
		Autonomy: req.Autonomy, Key: sk})
	h.writeCloudAgentResult(w, r, http.StatusCreated, res, err, secret, sk)
}

// POST /v2/agents/{agent}:rekey
func (h *Handler) rekeyAgent(w http.ResponseWriter, r *http.Request) {
	p, key, e := h.commandStart(r)
	if e != nil {
		writeError(w, e)
		return
	}
	id, e := agentID(r)
	if e != nil {
		writeError(w, e)
		return
	}
	var req agentCommandRequest
	if e := decodeBody(w, r, &req, false); e != nil {
		writeError(w, e)
		return
	}
	secret, sk, err := h.newStartKey()
	if err != nil {
		writeError(w, h.fromLedger(r, err))
		return
	}
	m := p.meta(p.scope, key, commandFields{Reason: req.Reason, OccurredAt: req.OccurredAt})
	res, err := h.ledger.Apply(r.Context(), &ledger.RekeyAgent{Meta: m, Connection: id, Key: sk})
	h.writeCloudAgentResult(w, r, http.StatusOK, res, err, secret, sk)
}

// writeCloudAgentResult answers with the secret, unless the key was used
// before (the secret it made then is gone).
func (h *Handler) writeCloudAgentResult(w http.ResponseWriter, r *http.Request, status int, res ledger.Result, err error,
	secret string, sk ledger.StartKey) {
	if err != nil {
		writeError(w, h.fromLedger(r, err))
		return
	}
	if res.Outcome == ledger.OutcomeRefused {
		h.writeRefusal(w, r, res.Policy)
		return
	}
	out := cloudAgentResult{Outcome: res.Outcome, Policy: res.Policy, Agent: res.Connection, Receipts: nonNil(res.Receipts)}
	if res.Replayed {
		w.Header().Set("Idempotent-Replayed", "true")
	} else {
		out.Key = &startKeyWire{Secret: secret, Prefix: sk.Prefix, ExpiresAt: sk.ExpiresAt}
	}
	writeData(w, status, out)
}
