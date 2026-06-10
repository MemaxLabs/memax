package handler

import (
	"context"
	"errors"
	"html/template"
	"log/slog"
	"net/http"
	"strings"

	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// UnsubscribeHandler serves /v1/unsubscribe as a public endpoint. The
// token in the query string IS the auth — it's random 32-byte entropy
// per user, rotates on use. Rate-limited at the IP layer to blunt
// brute-force enumeration.
//
// Two surfaces:
//   - GET: HTML confirmation page for humans clicking through. Tells
//     them what will happen, includes a POST form to confirm.
//   - POST: one-click opt-out (RFC 8058). Called by mail clients and
//     by the confirmation form.
type UnsubscribeHandler struct {
	store store.Store
}

func NewUnsubscribeHandler(s store.Store) *UnsubscribeHandler {
	return &UnsubscribeHandler{store: s}
}

// productName resolves the brand-configured product name at request
// time so admin edits to the brand settings surface immediately on
// the confirmation pages. Falls back to "memax" on any lookup error
// or when the admin has not customized the field.
func (h *UnsubscribeHandler) productName(ctx context.Context) string {
	const fallback = "memax"
	if h.store == nil {
		return fallback
	}
	brand, err := h.store.GetEmailBrandSettings(ctx)
	if err != nil || brand == nil || strings.TrimSpace(brand.ProductName) == "" {
		return fallback
	}
	return brand.ProductName
}

// Receive dispatches GET and POST. GET shows a confirmation page;
// POST (either from the form or from a mail client's one-click
// action) performs the opt-out and renders a success page.
func (h *UnsubscribeHandler) Receive(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" && r.Method == http.MethodPost {
		// Also accept token in form body (one-click POST sometimes
		// ships the query string in the form, depending on client).
		if err := r.ParseForm(); err == nil {
			token = r.Form.Get("token")
		}
	}
	if token == "" {
		h.renderError(w, r, http.StatusBadRequest, "Missing unsubscribe token.")
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.renderConfirmPage(w, r, token)
	case http.MethodPost:
		h.performUnsubscribe(w, r, token)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *UnsubscribeHandler) performUnsubscribe(w http.ResponseWriter, r *http.Request, token string) {
	userID, err := h.store.UnsubscribeByToken(r.Context(), token)
	if err != nil {
		if errors.Is(err, store.ErrUnsubscribeTokenNotFound) {
			// Always render a success-shaped page for unknown tokens
			// to avoid leaking token validity via response differences.
			// Operators see the real outcome in the log.
			slog.Info("unsubscribe: unknown token (likely already-unsubscribed + rotated)")
			h.renderSuccessPage(w, r)
			return
		}
		slog.Error("unsubscribe failed", "error", err)
		h.renderError(w, r, http.StatusInternalServerError,
			"We couldn't record your unsubscribe. Please try again or email support.")
		return
	}
	slog.Info("unsubscribe succeeded", "user_id", userID)
	h.renderSuccessPage(w, r)
}

// renderConfirmPage shows a small HTML form the recipient can POST
// back to confirm. Exists so users clicking a link aren't silently
// unsubscribed via browser prefetchers or link-preview bots. Mail
// clients that honor List-Unsubscribe-Post=One-Click skip this page
// and POST directly.
func (h *UnsubscribeHandler) renderConfirmPage(w http.ResponseWriter, r *http.Request, token string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	data := unsubPageData{ProductName: h.productName(r.Context()), Token: token}
	if err := confirmTmpl.Execute(w, data); err != nil {
		slog.Error("render unsubscribe confirm page", "error", err)
	}
}

func (h *UnsubscribeHandler) renderSuccessPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	data := unsubPageData{ProductName: h.productName(r.Context())}
	if err := successTmpl.Execute(w, data); err != nil {
		slog.Error("render unsubscribe success page", "error", err)
	}
}

func (h *UnsubscribeHandler) renderError(w http.ResponseWriter, r *http.Request, status int, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = errorTmpl.Execute(w, unsubPageData{ProductName: h.productName(r.Context()), Error: msg})
}

type unsubPageData struct {
	ProductName string
	Token       string
	Error       string
}

// Minimal inline-styled confirmation pages. Intentionally plain —
// this endpoint is hit rarely and shouldn't require a full Next.js
// render cycle. Styling matches the base email layout's palette so
// users don't feel like they landed somewhere unrelated.
var confirmTmpl = template.Must(template.New("confirm").Parse(`<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Unsubscribe from {{.ProductName}}</title>
<style>
body { margin:0; padding:0; background:#fafafa; font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,sans-serif; color:#444; }
.box { max-width:480px; margin:64px auto; padding:40px 32px; background:#fff; border:1px solid #e5e5e5; border-radius:12px; text-align:center; }
h1 { font-size:22px; color:#111; margin:0 0 12px; }
p { font-size:15px; line-height:1.6; margin:0 0 20px; }
button { font:inherit; cursor:pointer; padding:12px 28px; background:#111; color:#fff; border:0; border-radius:8px; font-weight:500; }
</style>
</head>
<body>
<div class="box">
<h1>Unsubscribe from {{.ProductName}} marketing emails?</h1>
<p>You'll still get transactional emails you asked for (hub invites, account verification).</p>
<form method="post">
<input type="hidden" name="token" value="{{.Token}}">
<button type="submit">Unsubscribe</button>
</form>
</div>
</body>
</html>`))

var successTmpl = template.Must(template.New("success").Parse(`<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Unsubscribed</title>
<style>
body { margin:0; padding:0; background:#fafafa; font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,sans-serif; color:#444; }
.box { max-width:480px; margin:64px auto; padding:40px 32px; background:#fff; border:1px solid #e5e5e5; border-radius:12px; text-align:center; }
h1 { font-size:22px; color:#111; margin:0 0 12px; }
p { font-size:15px; line-height:1.6; margin:0; }
</style>
</head>
<body>
<div class="box">
<h1>You're unsubscribed</h1>
<p>You won't receive marketing emails from {{.ProductName}} anymore. Transactional emails (like hub invites) still work.</p>
</div>
</body>
</html>`))

var errorTmpl = template.Must(template.New("err").Parse(`<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Unsubscribe error</title>
<style>
body { margin:0; padding:0; background:#fafafa; font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,sans-serif; color:#444; }
.box { max-width:480px; margin:64px auto; padding:40px 32px; background:#fff; border:1px solid #e5e5e5; border-radius:12px; text-align:center; }
h1 { font-size:22px; color:#111; margin:0 0 12px; }
p { font-size:15px; line-height:1.6; margin:0; }
</style>
</head>
<body>
<div class="box">
<h1>Something went wrong</h1>
<p>{{.Error}}</p>
</div>
</body>
</html>`))
