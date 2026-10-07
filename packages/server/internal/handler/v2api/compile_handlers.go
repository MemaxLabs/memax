package v2api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/compile"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// The Brief and its targets (plan 25 §5.7). Commands go to the ledger;
// the preview, hand-edit reports and the drift view go to the compile
// coordinator, which reads and writes the record only through the ledger
// and adds what the ledger doesn't hold: artifacts and parse-back.

// ---------------------------------------------------------------------
// Wire shapes
// ---------------------------------------------------------------------

type briefVersionPage struct {
	Items      []ledger.Brief `json:"items"`
	HasMore    bool           `json:"has_more"`
	NextCursor string         `json:"next_cursor,omitempty"`
}

type briefResult struct {
	Outcome  ledger.Outcome   `json:"outcome"`
	Policy   policy.Decision  `json:"policy"`
	Brief    *ledger.Brief    `json:"brief"`
	Receipts []ledger.Receipt `json:"receipts"`
}

type targetList struct {
	Items []ledger.Target `json:"items"`
}

type targetResult struct {
	Outcome  ledger.Outcome   `json:"outcome"`
	Policy   policy.Decision  `json:"policy"`
	Target   *ledger.Target   `json:"target"`
	Receipts []ledger.Receipt `json:"receipts"`
}

type compileRunPage struct {
	Items      []ledger.CompileRun `json:"items"`
	HasMore    bool                `json:"has_more"`
	NextCursor string              `json:"next_cursor,omitempty"`
}

type targetPreview struct {
	Target  *ledger.Target           `json:"target"`
	Compile *ledger.CompileRun       `json:"compile,omitempty"`
	Reads   string                   `json:"reads,omitempty"`
	Files   []compile.ArtifactOutput `json:"files"`
	Copies  []compile.ArtifactOutput `json:"copies"`
}

type observationResult struct {
	Drifted     bool                `json:"drifted"`
	Target      *ledger.Target      `json:"target"`
	Observation *ledger.Observation `json:"observation,omitempty"`
	Receipts    []ledger.Receipt    `json:"receipts"`
}

type deliveryResult struct {
	Target   *ledger.Target     `json:"target"`
	Compile  *ledger.CompileRun `json:"compile"`
	Receipts []ledger.Receipt   `json:"receipts"`
}

type driftItem struct {
	Observation ledger.Observation `json:"observation"`
	Compiled    string             `json:"compiled"`
	BaseCompile *ledger.CompileRun `json:"base_compile,omitempty"`
	Observed    string             `json:"observed"`
}

type driftView struct {
	Target *ledger.Target `json:"target"`
	Items  []driftItem    `json:"items"`
}

type driftResolution struct {
	Outcome      ledger.Outcome       `json:"outcome"`
	Policy       policy.Decision      `json:"policy"`
	Target       *ledger.Target       `json:"target"`
	Observations []ledger.Observation `json:"observations"`
	Proposals    []ledger.Memory      `json:"proposals"`
	Receipts     []ledger.Receipt     `json:"receipts"`
}

type briefItemInput struct {
	Ref   string   `json:"ref"`
	Text  string   `json:"text"`
	Cites []string `json:"cites"`
}

type briefSectionInput struct {
	Key     string           `json:"key"`
	Heading string           `json:"heading"`
	Items   []briefItemInput `json:"items"`
}

type reviseBriefRequest struct {
	Title    string              `json:"title"`
	Summary  string              `json:"summary"`
	Sections []briefSectionInput `json:"sections"`
	commandFields
}

type createTargetRequest struct {
	Kind     ledger.TargetKind           `json:"kind"`
	Path     *string                     `json:"path"`
	Settings *ledger.TargetSettingsInput `json:"settings"`
	Delivery *ledger.Delivery            `json:"delivery"`
	commandFields
}

type configureTargetRequest struct {
	Path     *string                     `json:"path"`
	Settings *ledger.TargetSettingsInput `json:"settings"`
	Delivery *ledger.Delivery            `json:"delivery"`
	Enabled  *bool                       `json:"enabled"`
	commandFields
}

type observationRequest struct {
	Path     string `json:"path"`
	Content  string `json:"content"`
	DeviceID string `json:"device_id"`
	Commit   string `json:"commit"`
	commandFields
}

type deliveryRequest struct {
	Compile string `json:"compile"`
	SHA256  string `json:"sha256"`
	commandFields
}

type resolveDriftRequest struct {
	Observation *uuid.UUID `json:"observation"`
	commandFields
}

// ---------------------------------------------------------------------
// The Brief
// ---------------------------------------------------------------------

// GET /v2/spaces/{space}/brief
func (h *Handler) getBrief(w http.ResponseWriter, r *http.Request) {
	p, e := h.principalFor(r)
	if e != nil {
		writeError(w, e)
		return
	}
	sp, e := h.space(r, p, r.PathValue("space"))
	if e != nil {
		writeError(w, e)
		return
	}
	b, err := h.ledger.GetBrief(r.Context(), p.scope, sp.SpaceID)
	if err != nil {
		writeError(w, h.fromLedger(r, err))
		return
	}
	setVersionETag(w, b.Version)
	writeData(w, http.StatusOK, b)
}

// POST /v2/spaces/{space}/brief
func (h *Handler) reviseBrief(w http.ResponseWriter, r *http.Request) {
	p, key, e := h.commandStart(r)
	if e != nil {
		writeError(w, e)
		return
	}
	version, hasIfMatch, e := ifMatchVersion(r)
	if e != nil {
		writeError(w, e)
		return
	}
	var req reviseBriefRequest
	if e := decodeBody(w, r, &req, true); e != nil {
		writeError(w, e)
		return
	}
	sp, e := h.space(r, p, r.PathValue("space"))
	if e != nil {
		writeError(w, e)
		return
	}
	if !hasIfMatch {
		// Once there is a Brief, a revision must say which version it
		// started from, so it never overwrites words nobody saw.
		if _, err := h.ledger.GetBrief(r.Context(), p.scope, sp.SpaceID); err == nil {
			writeError(w, &apiError{status: http.StatusPreconditionRequired, code: codePreconditionRequired,
				message: `Send If-Match with the Brief version you started from (its ETag, such as "3").`})
			return
		}
	}
	cmd := &ledger.ReviseBrief{Meta: p.meta(p.scope.Narrow(sp.SpaceID), key, req.commandFields), SpaceID: sp.SpaceID,
		ExpectedVersion: version, Title: req.Title, Summary: req.Summary}
	for _, s := range req.Sections {
		sec := ledger.BriefSection{Key: s.Key, Heading: s.Heading, Items: []ledger.BriefItem{}}
		for _, it := range s.Items {
			sec.Items = append(sec.Items, ledger.BriefItem{Ref: it.Ref, Text: it.Text, Cites: it.Cites})
		}
		cmd.Sections = append(cmd.Sections, sec)
	}
	res, err := h.ledger.Apply(r.Context(), cmd)
	if !h.commandOK(w, r, res, err) {
		return
	}
	setVersionETag(w, res.Brief.Version)
	writeData(w, http.StatusCreated, briefResult{Outcome: res.Outcome, Policy: res.Policy, Brief: res.Brief, Receipts: nonNil(res.Receipts)})
}

// GET /v2/spaces/{space}/brief/versions
func (h *Handler) listBriefVersions(w http.ResponseWriter, r *http.Request) {
	p, sp, cursor, limit, e := h.spaceList(r)
	if e != nil {
		writeError(w, e)
		return
	}
	page, err := h.ledger.ListBriefVersions(r.Context(), p.scope, ledger.BriefVersionQuery{SpaceID: sp.SpaceID, Cursor: cursor, Limit: limit})
	if err != nil {
		writeError(w, h.fromLedger(r, err))
		return
	}
	writeData(w, http.StatusOK, briefVersionPage{Items: nonNil(page.Versions), HasMore: page.HasMore, NextCursor: page.NextCursor})
}

// ---------------------------------------------------------------------
// Targets
// ---------------------------------------------------------------------

// GET /v2/spaces/{space}/targets
func (h *Handler) listTargets(w http.ResponseWriter, r *http.Request) {
	p, e := h.principalFor(r)
	if e != nil {
		writeError(w, e)
		return
	}
	sp, e := h.space(r, p, r.PathValue("space"))
	if e != nil {
		writeError(w, e)
		return
	}
	targets, err := h.ledger.ListTargets(r.Context(), p.scope, sp.SpaceID)
	if err != nil {
		writeError(w, h.fromLedger(r, err))
		return
	}
	writeData(w, http.StatusOK, targetList{Items: nonNil(targets)})
}

// POST /v2/spaces/{space}/targets
func (h *Handler) createTarget(w http.ResponseWriter, r *http.Request) {
	p, key, e := h.commandStart(r)
	if e != nil {
		writeError(w, e)
		return
	}
	var req createTargetRequest
	if e := decodeBody(w, r, &req, true); e != nil {
		writeError(w, e)
		return
	}
	sp, e := h.space(r, p, r.PathValue("space"))
	if e != nil {
		writeError(w, e)
		return
	}
	res, err := h.ledger.Apply(r.Context(), &ledger.ConfigureTarget{
		Meta: p.meta(p.scope.Narrow(sp.SpaceID), key, req.commandFields), SpaceID: sp.SpaceID, Kind: req.Kind,
		Path: req.Path, Settings: req.Settings, Delivery: req.Delivery,
	})
	if !h.commandOK(w, r, res, err) {
		return
	}
	w.Header().Set("Location", "/v2/targets/"+res.Target.ID.String())
	h.writeTarget(w, http.StatusCreated, res)
}

// PATCH /v2/targets/{target}
func (h *Handler) configureTarget(w http.ResponseWriter, r *http.Request) {
	p, key, e := h.commandStart(r)
	if e != nil {
		writeError(w, e)
		return
	}
	version, _, e := ifMatchVersion(r)
	if e != nil {
		writeError(w, e)
		return
	}
	id, e := targetID(r)
	if e != nil {
		writeError(w, e)
		return
	}
	var req configureTargetRequest
	if e := decodeBody(w, r, &req, true); e != nil {
		writeError(w, e)
		return
	}
	res, err := h.ledger.Apply(r.Context(), &ledger.ConfigureTarget{
		Meta: p.meta(p.scope, key, req.commandFields), Target: id, ExpectedVersion: version,
		Path: req.Path, Settings: req.Settings, Delivery: req.Delivery, Enabled: req.Enabled,
	})
	if !h.commandOK(w, r, res, err) {
		return
	}
	h.writeTarget(w, http.StatusOK, res)
}

// POST /v2/targets/{target}:compile
func (h *Handler) compileTarget(w http.ResponseWriter, r *http.Request) {
	p, key, e := h.commandStart(r)
	if e != nil {
		writeError(w, e)
		return
	}
	id, e := targetID(r)
	if e != nil {
		writeError(w, e)
		return
	}
	var req reviewRequest
	if e := decodeBody(w, r, &req, false); e != nil {
		writeError(w, e)
		return
	}
	res, err := h.ledger.Apply(r.Context(), &ledger.RequestCompile{Meta: p.meta(p.scope, key, req.commandFields), Target: id})
	if !h.commandOK(w, r, res, err) {
		return
	}
	h.writeTarget(w, http.StatusAccepted, res)
}

// GET /v2/targets/{target}/preview
func (h *Handler) getTargetPreview(w http.ResponseWriter, r *http.Request) {
	p, id, e := h.targetRead(r)
	if e != nil {
		writeError(w, e)
		return
	}
	if e := h.compileConfigured(); e != nil {
		writeError(w, e)
		return
	}
	pv, err := h.compile.Preview(r.Context(), p.scope, id)
	if err != nil {
		writeError(w, h.fromLedger(r, err))
		return
	}
	writeData(w, http.StatusOK, targetPreview{Target: pv.Target, Compile: pv.Compile, Reads: pv.Reads,
		Files: nonNil(pv.Files), Copies: nonNil(pv.Copies)})
}

// GET /v2/targets/{target}/runs
func (h *Handler) listCompileRuns(w http.ResponseWriter, r *http.Request) {
	p, id, e := h.targetRead(r)
	if e != nil {
		writeError(w, e)
		return
	}
	cursor, limit, e := page(r)
	if e != nil {
		writeError(w, e)
		return
	}
	res, err := h.ledger.ListCompileRuns(r.Context(), p.scope, ledger.CompileRunQuery{TargetID: id, Cursor: cursor, Limit: limit})
	if err != nil {
		writeError(w, h.fromLedger(r, err))
		return
	}
	writeData(w, http.StatusOK, compileRunPage{Items: nonNil(res.Runs), HasMore: res.HasMore, NextCursor: res.NextCursor})
}

// POST /v2/targets/{target}/observations
func (h *Handler) recordObservation(w http.ResponseWriter, r *http.Request) {
	p, key, e := h.commandStart(r)
	if e != nil {
		writeError(w, e)
		return
	}
	id, e := targetID(r)
	if e != nil {
		writeError(w, e)
		return
	}
	var req observationRequest
	if e := decodeBody(w, r, &req, true); e != nil {
		writeError(w, e)
		return
	}
	if e := h.compileConfigured(); e != nil {
		writeError(w, e)
		return
	}
	device := strings.TrimSpace(req.DeviceID)
	if device == "" {
		// Unnamed devices are told apart by who reported them.
		device = string(p.actor.Kind) + ":" + p.actor.ID.String()
	}
	res, err := h.compile.Observe(r.Context(), p.meta(p.scope, key, req.commandFields), id, compile.ObserveInput{
		Path: req.Path, Content: req.Content, ObserverKind: ledger.ObserverDevice, ObserverID: device, Commit: req.Commit,
	})
	if !h.commandOK(w, r, res, err) {
		return
	}
	out := observationResult{Target: res.Target, Receipts: nonNil(res.Receipts)}
	status := http.StatusOK
	if !res.Unchanged && len(res.Observations) > 0 {
		out.Drifted, out.Observation, status = true, &res.Observations[0], http.StatusCreated
	}
	writeData(w, status, out)
}

// POST /v2/targets/{target}/deliveries
func (h *Handler) recordDelivery(w http.ResponseWriter, r *http.Request) {
	p, key, e := h.commandStart(r)
	if e != nil {
		writeError(w, e)
		return
	}
	id, e := targetID(r)
	if e != nil {
		writeError(w, e)
		return
	}
	var req deliveryRequest
	if e := decodeBody(w, r, &req, true); e != nil {
		writeError(w, e)
		return
	}
	res, err := h.ledger.Apply(r.Context(), &ledger.RecordDelivery{
		Meta: p.meta(p.scope, key, req.commandFields), Target: id, Compile: req.Compile, SHA256: req.SHA256,
	})
	if !h.commandOK(w, r, res, err) {
		return
	}
	writeData(w, http.StatusOK, deliveryResult{Target: res.Target, Compile: res.Compile, Receipts: nonNil(res.Receipts)})
}

// GET /v2/targets/{target}/drift
func (h *Handler) getDrift(w http.ResponseWriter, r *http.Request) {
	p, id, e := h.targetRead(r)
	if e != nil {
		writeError(w, e)
		return
	}
	if e := h.compileConfigured(); e != nil {
		writeError(w, e)
		return
	}
	d, err := h.compile.Drift(r.Context(), p.scope, id)
	if err != nil {
		writeError(w, h.fromLedger(r, err))
		return
	}
	out := driftView{Target: d.Target, Items: []driftItem{}}
	for _, it := range d.Items {
		out.Items = append(out.Items, driftItem{Observation: it.Observation, Compiled: it.Compiled, BaseCompile: it.BaseCompile, Observed: it.Observed})
	}
	writeData(w, http.StatusOK, out)
}

// POST /v2/targets/{target}/drift:pull
func (h *Handler) pullDrift(w http.ResponseWriter, r *http.Request) {
	h.resolveDrift(w, r, ledger.DriftPull)
}

// POST /v2/targets/{target}/drift:overwrite
func (h *Handler) overwriteDrift(w http.ResponseWriter, r *http.Request) {
	h.resolveDrift(w, r, ledger.DriftOverwrite)
}

// POST /v2/targets/{target}/drift:stop
func (h *Handler) stopDrift(w http.ResponseWriter, r *http.Request) {
	h.resolveDrift(w, r, ledger.DriftStop)
}

func (h *Handler) resolveDrift(w http.ResponseWriter, r *http.Request, mode ledger.DriftMode) {
	p, key, e := h.commandStart(r)
	if e != nil {
		writeError(w, e)
		return
	}
	id, e := targetID(r)
	if e != nil {
		writeError(w, e)
		return
	}
	var req resolveDriftRequest
	if e := decodeBody(w, r, &req, false); e != nil {
		writeError(w, e)
		return
	}
	cmd := &ledger.ResolveDrift{Meta: p.meta(p.scope, key, req.commandFields), Target: id, Mode: mode}
	if req.Observation != nil {
		cmd.Observation = *req.Observation
	}
	res, err := h.ledger.Apply(r.Context(), cmd)
	if !h.commandOK(w, r, res, err) {
		return
	}
	writeData(w, http.StatusOK, driftResolution{Outcome: res.Outcome, Policy: res.Policy, Target: res.Target,
		Observations: nonNil(res.Observations), Proposals: nonNil(res.Proposals), Receipts: nonNil(res.Receipts)})
}

// ---------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------

// commandOK writes the error or the refusal of a command and reports
// whether the result is there to answer with. A replay is marked.
func (h *Handler) commandOK(w http.ResponseWriter, r *http.Request, res ledger.Result, err error) bool {
	switch {
	case err != nil:
		writeError(w, h.fromLedger(r, err))
		return false
	case res.Outcome == ledger.OutcomeRefused:
		h.writeRefusal(w, r, res.Policy)
		return false
	}
	if res.Replayed {
		w.Header().Set("Idempotent-Replayed", "true")
	}
	return true
}

func (h *Handler) writeTarget(w http.ResponseWriter, status int, res ledger.Result) {
	setVersionETag(w, res.Target.Version)
	writeData(w, status, targetResult{Outcome: res.Outcome, Policy: res.Policy, Target: res.Target, Receipts: nonNil(res.Receipts)})
}

// targetRead is the start of the target reads.
func (h *Handler) targetRead(r *http.Request) (*principal, uuid.UUID, *apiError) {
	p, e := h.principalFor(r)
	if e != nil {
		return nil, uuid.Nil, e
	}
	id, e := targetID(r)
	return p, id, e
}

// targetID parses {target}. A target is addressed by its id only.
func targetID(r *http.Request) (uuid.UUID, *apiError) {
	id, err := uuid.Parse(r.PathValue("target"))
	if err != nil {
		return uuid.Nil, invalidRequest("target", "Address a target by its id.")
	}
	return id, nil
}

func (h *Handler) compileConfigured() *apiError {
	if h.compile == nil {
		return &apiError{status: http.StatusServiceUnavailable, code: codeUnavailable,
			message: "This server has no compile service. Set COMPILE_SERVICE_URL and object storage."}
	}
	return nil
}

// ifMatchVersion reads If-Match as a Brief's or a target's version.
func ifMatchVersion(r *http.Request) (int, bool, *apiError) {
	n, ok, e := ifMatch(r)
	if e != nil {
		return 0, false, invalidRequest("If-Match", `If-Match must be the version's ETag, such as "3".`)
	}
	return n, ok, nil
}

func setVersionETag(w http.ResponseWriter, version int) {
	w.Header().Set("ETag", strconv.Quote(strconv.Itoa(version)))
}
