package v2api

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/handler"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// Machines (personal-layer design §5): the computers a person's Personal
// space is written to, each with its own home targets. Every command goes
// to the ledger, which decides it (policy.DecideMachine); the reads are
// decided here by the same rule, since a machine's name is personal data.

type machineList struct {
	Items []ledger.Machine `json:"items"`
}

type machineResult struct {
	Outcome  ledger.Outcome   `json:"outcome"`
	Policy   policy.Decision  `json:"policy"`
	Machine  *ledger.Machine  `json:"machine"`
	Targets  []ledger.Target  `json:"targets"`
	Receipts []ledger.Receipt `json:"receipts"`
}

type machineFileInput struct {
	Kind      ledger.TargetKind `json:"kind"`
	Path      string            `json:"path"`
	UserOwned *bool             `json:"user_owned"`
	ReadBy    []string          `json:"read_by"`
}

type linkMachineRequest struct {
	DeviceID uuid.UUID          `json:"device_id"`
	Name     string             `json:"name"`
	Platform string             `json:"platform"`
	Files    []machineFileInput `json:"files"`
	commandFields
}

type renameMachineRequest struct {
	Name string `json:"name"`
	commandFields
}

// machineReader decides a read of a space's machines (and of one
// machine's targets): the space's owner, as a person on a session.
func (h *Handler) machineReader(w http.ResponseWriter, r *http.Request, p *principal, g ledger.SpaceGrant) bool {
	actor := policy.Actor{Kind: p.actor.Kind, Credential: p.actor.Credential, Via: p.via, Role: g.Role}
	if d := policy.DecideMachine(actor, policy.MachineList, policy.Space{Name: g.Name, Kind: g.Kind}); d.Effect == policy.EffectRefuse {
		h.writeRefusal(w, r, d)
		return false
	}
	return true
}

// GET /v2/spaces/{space}/machines
func (h *Handler) listMachines(w http.ResponseWriter, r *http.Request) {
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
	if !h.machineReader(w, r, p, sp) {
		return
	}
	machines, err := h.ledger.ListMachines(r.Context(), p.scope, sp.SpaceID)
	if err != nil {
		writeError(w, h.fromLedger(r, err))
		return
	}
	writeData(w, http.StatusOK, machineList{Items: nonNil(machines)})
}

// POST /v2/spaces/{space}/machines
func (h *Handler) linkMachine(w http.ResponseWriter, r *http.Request) {
	p, key, e := h.commandStart(r)
	if e != nil {
		writeError(w, e)
		return
	}
	var req linkMachineRequest
	if e := decodeBody(w, r, &req, true); e != nil {
		writeError(w, e)
		return
	}
	sp, e := h.space(r, p, r.PathValue("space"))
	if e != nil {
		writeError(w, e)
		return
	}
	cmd := &ledger.LinkMachine{Meta: p.meta(p.scope.Narrow(sp.SpaceID), key, req.commandFields), SpaceID: sp.SpaceID,
		DeviceKey: req.DeviceID, MachineName: req.Name, Platform: req.Platform}
	// The session comes from the token, never from the body.
	cmd.SessionID, _ = uuid.Parse(handler.GetGrant(r).SessionID)
	kinds := make([]ledger.TargetKind, 0, len(req.Files))
	for _, f := range req.Files {
		cmd.Files = append(cmd.Files, ledger.MachineFile{Kind: f.Kind, Path: f.Path, UserOwned: f.UserOwned, ReadBy: f.ReadBy})
		kinds = append(kinds, f.Kind)
	}
	if e := h.adaptersFor(r, kinds...); e != nil {
		writeError(w, e)
		return
	}
	res, err := h.ledger.Apply(r.Context(), cmd)
	if !h.commandOK(w, r, res, err) {
		return
	}
	status := http.StatusCreated
	if res.Unchanged {
		status = http.StatusOK
	}
	h.writeMachine(w, status, res)
}

// PATCH /v2/machines/{machine}
func (h *Handler) renameMachine(w http.ResponseWriter, r *http.Request) {
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
	id, e := machineID(r)
	if e != nil {
		writeError(w, e)
		return
	}
	var req renameMachineRequest
	if e := decodeBody(w, r, &req, true); e != nil {
		writeError(w, e)
		return
	}
	res, err := h.ledger.Apply(r.Context(), &ledger.RenameMachine{Meta: p.meta(p.scope, key, req.commandFields),
		Machine: id, ExpectedVersion: version, NewName: req.Name})
	if !h.commandOK(w, r, res, err) {
		return
	}
	h.writeMachine(w, http.StatusOK, res)
}

// POST /v2/machines/{machine}:unlink
func (h *Handler) unlinkMachine(w http.ResponseWriter, r *http.Request) {
	p, key, e := h.commandStart(r)
	if e != nil {
		writeError(w, e)
		return
	}
	id, e := machineID(r)
	if e != nil {
		writeError(w, e)
		return
	}
	var req reviewRequest
	if e := decodeBody(w, r, &req, false); e != nil {
		writeError(w, e)
		return
	}
	res, err := h.ledger.Apply(r.Context(), &ledger.UnlinkMachine{Meta: p.meta(p.scope, key, req.commandFields), Machine: id})
	if !h.commandOK(w, r, res, err) {
		return
	}
	h.writeMachine(w, http.StatusOK, res)
}

func (h *Handler) writeMachine(w http.ResponseWriter, status int, res ledger.Result) {
	setVersionETag(w, res.Machine.Version)
	writeData(w, status, machineResult{Outcome: res.Outcome, Policy: res.Policy, Machine: res.Machine,
		Targets: nonNil(res.Targets), Receipts: nonNil(res.Receipts)})
}

// machineID parses {machine}. A machine is addressed by its id only.
func machineID(r *http.Request) (uuid.UUID, *apiError) {
	id, err := uuid.Parse(r.PathValue("machine"))
	if err != nil {
		return uuid.Nil, invalidRequest("machine", "Address a machine by its id.")
	}
	return id, nil
}

// adaptersFor refuses kinds the compile service doesn't compile: the
// contract stays at version 1 as adapters are added, so a kind the service
// doesn't list would only ever fail to compile.
func (h *Handler) adaptersFor(r *http.Request, kinds ...ledger.TargetKind) *apiError {
	if h.compile == nil {
		return nil
	}
	missing := h.compile.MissingAdapters(r.Context(), kinds...)
	if len(missing) == 0 {
		return nil
	}
	names := make([]string, len(missing))
	for i, k := range missing {
		names[i] = string(k)
	}
	return invalidRequest("kind", fmt.Sprintf(
		"The compile service here doesn't have the %s adapter yet. Update it, then try again.", strings.Join(names, " and ")))
}
