package handler

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"testing"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/plans"
	"github.com/MemaxLabs/memax/packages/server/internal/v2plans"
)

// TestConsentPastThePlansKinds: with plan limits enforced, an agent of a
// kind past the person's plan (D9: Free connects 3) isn't connected, so its
// grant would read nothing: consent answers 403 with the plan's refusal,
// takes the grant back and keeps the request open. Without them the same
// consent connects as before.
func TestConsentPastThePlansKinds(t *testing.T) {
	for _, enforce := range []bool{true, false} {
		f := newOAuthFlow(t)
		ctx := context.Background()
		// Free connects no kind of agent here, so the first meets the limit.
		if _, err := f.authH.pool.Exec(ctx, `UPDATE plans SET agent_kind_limit = 0 WHERE id = 'personal_free'`); err != nil {
			t.Fatal(err)
		}
		reg := plans.NewRegistry(f.authH.store)
		if err := reg.Load(ctx); err != nil {
			t.Fatal(err)
		}
		f.h.ledger = ledger.New(f.authH.pool, ledger.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))),
			ledger.WithPlans(v2plans.New(reg, enforce)))
		project := f.project(f.user, "memax-v2", 0)
		token := f.webSession(f.user)
		request, _ := f.start(authorizeQuery(cimdClientID, cimdRedirect, "memax:read memax:write"))
		f.open(request, token)
		rec := f.call(http.MethodPost, "/oauth/authorize/requests/"+request+"/decision", token,
			map[string]string{"decision": "approve", "space_id": project})
		var grants, connections int
		_ = f.authH.pool.QueryRow(ctx, `SELECT count(*) FROM oauth_grants WHERE user_id = $1 AND revoked_at IS NULL`, f.user).Scan(&grants)
		_ = f.authH.pool.QueryRow(ctx, `SELECT count(*) FROM v2.agent_connections WHERE person_id = $1`, f.user).Scan(&connections)
		if !enforce {
			if rec.Code != http.StatusOK || grants != 1 || connections != 1 {
				t.Errorf("not enforced: %d, %d grants, %d connections: %s", rec.Code, grants, connections, rec.Body)
			}
			continue
		}
		var body struct {
			Error struct {
				Code    string `json:"code"`
				Details struct {
					Policy policy.Decision `json:"policy"`
				} `json:"details"`
			} `json:"error"`
		}
		if rec.Code != http.StatusForbidden || json.Unmarshal(rec.Body.Bytes(), &body) != nil || body.Error.Code != "refused" ||
			body.Error.Details.Policy.Code != policy.CodeAgentKindLimit || body.Error.Details.Policy.Limit == nil ||
			body.Error.Details.Policy.Limit.Upgrade != policy.TierPro {
			t.Fatalf("enforced: %d %s", rec.Code, rec.Body)
		}
		if grants != 0 || connections != 0 {
			t.Errorf("enforced: %d live grants and %d connections, want none", grants, connections)
		}
		// The request is still open: the person can choose again after
		// disconnecting an agent, or moving to Pro.
		if status, _ := f.open(request, token); status != http.StatusOK {
			t.Errorf("the request closed: %d", status)
		}
	}
}
