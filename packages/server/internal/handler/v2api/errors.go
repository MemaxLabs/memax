package v2api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/MemaxLabs/memax/packages/server/internal/handler"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// Error codes. They are the ErrorCode enum of v2.yaml (TestErrorCodesMatchSpec).
const (
	codeInvalidRequest         = "invalid_request"
	codeIdempotencyKeyRequired = "idempotency_key_required"
	codeSpaceRequired          = "space_required"
	codeAmbiguousRef           = "ambiguous_ref"
	codeUnauthorized           = "unauthorized"
	codeRefused                = "refused"
	codePermissionDenied       = "permission_denied"
	codeSurfaceUnverified      = "surface_unverified"
	codeImpersonation          = "impersonation_read_only"
	codeNotFound               = "not_found"
	codeMethodNotAllowed       = "method_not_allowed"
	codeInvalidTransition      = "invalid_transition"
	codeEditClash              = "edit_clash"
	codeKeyReused              = "idempotency_key_reused"
	codePreconditionRequired   = "precondition_required"
	codeInternal               = "internal_error"
	codeBusy                   = "busy"
	codeUnavailable            = "unavailable"
)

// apiError is an error response: a status, an ErrorCode and a sentence
// that says what to do.
type apiError struct {
	status     int
	code       string
	message    string
	details    *errorDetails
	retryAfter int // seconds; sets Retry-After
}

// errorDetails is the ErrorDetails schema (the keys /v2 itself writes).
type errorDetails struct {
	Field           string           `json:"field,omitempty"`
	Policy          *policy.Decision `json:"policy,omitempty"`
	Ref             string           `json:"ref,omitempty"`
	ExpectedVersion int              `json:"expected_version,omitempty"`
	CurrentVersion  int              `json:"current_version,omitempty"`
	RetryAfter      int              `json:"retry_after,omitempty"`
}

func writeError(w http.ResponseWriter, e *apiError) {
	if e.retryAfter > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(e.retryAfter))
	}
	body := &model.Error{Code: e.code, Message: e.message}
	if e.details != nil {
		body.Details = e.details
	}
	handler.WriteJSON(w, e.status, model.ApiResponse{Error: body})
}

func invalidRequest(field, message string) *apiError {
	return &apiError{status: http.StatusBadRequest, code: codeInvalidRequest, message: message,
		details: &errorDetails{Field: field}}
}

var notFound = &apiError{status: http.StatusNotFound, code: codeNotFound,
	message: "Nothing by that reference in your spaces. Check the ID and the space."}

// refusal is a policy refusal: 403 with the decision, so clients can
// localise by details.policy.code.
func refusal(d policy.Decision) *apiError {
	return &apiError{status: http.StatusForbidden, code: codeRefused, message: d.Message,
		details: &errorDetails{Policy: &d}}
}

// fromLedger maps a ledger error onto a response. Anything it doesn't
// recognise is a 500, logged without request content.
func (h *Handler) fromLedger(r *http.Request, err error) *apiError {
	var ve *ledger.ValidationError
	var clash *ledger.EditClashError
	var te *ledger.TransitionError
	var ce *ledger.ConnectionStateError
	switch {
	case errors.As(err, &ce):
		return &apiError{status: http.StatusConflict, code: codeInvalidTransition, message: ce.Error()}
	case errors.As(err, &ve):
		return invalidRequest(ve.Field, ve.Error())
	case errors.Is(err, ledger.ErrNotFound):
		return notFound
	case errors.Is(err, ledger.ErrAmbiguousRef):
		return &apiError{status: http.StatusBadRequest, code: codeAmbiguousRef,
			message: "That display ID exists in more than one of your spaces. Add ?space= with the space's id or slug."}
	case errors.As(err, &clash):
		return &apiError{status: http.StatusPreconditionFailed, code: codeEditClash, message: clash.Error(),
			details: &errorDetails{Ref: clash.Ref, ExpectedVersion: clash.Expected, CurrentVersion: clash.Current}}
	case errors.Is(err, ledger.ErrEditClash):
		return &apiError{status: http.StatusPreconditionFailed, code: codeEditClash,
			message: "Another change landed first. Reload the memory and try again."}
	case errors.As(err, &te):
		return &apiError{status: http.StatusConflict, code: codeInvalidTransition, message: te.Error(),
			details: &errorDetails{Ref: te.Ref}}
	case errors.Is(err, ledger.ErrInvalidTransition):
		return &apiError{status: http.StatusConflict, code: codeInvalidTransition,
			message: "The memory's state doesn't allow that. Reload it and try again."}
	case errors.Is(err, ledger.ErrIdempotencyKeyReused):
		return &apiError{status: http.StatusUnprocessableEntity, code: codeKeyReused,
			message: "That Idempotency-Key was already used for a different command. Send a new key for a new command."}
	case errors.Is(err, ledger.ErrBusy):
		return &apiError{status: http.StatusServiceUnavailable, code: codeBusy, retryAfter: 1,
			message: "Another change is holding this memory. Try again in a moment.", details: &errorDetails{RetryAfter: 1}}
	case errors.Is(err, ledger.ErrDisabled):
		return &apiError{status: http.StatusServiceUnavailable, code: codeUnavailable,
			message: "The V2 record isn't configured on this server."}
	}
	h.log.ErrorContext(r.Context(), "v2: request failed", "method", r.Method, "path", r.URL.Path, "error", err)
	return &apiError{status: http.StatusInternalServerError, code: codeInternal,
		message: "Something went wrong on our side. Try again; if it keeps happening, contact support@memax.app."}
}
