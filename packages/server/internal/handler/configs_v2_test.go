package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/spacemode"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
)

// Two-way config sync stops for the files of a space on V2 (plan 25 §10,
// D10): Memax compiles them there. V1's sync answers them "unchanged"
// with reason space_on_v2 and no version (nothing is acknowledged), never
// pull, push, conflict or delete, and warns. A space back on V1 syncs
// again; files of spaces still on V1 sync as always.
func TestConfigSyncIsOffForSpacesOnV2(t *testing.T) {
	t.Parallel()
	st, pool := testdb.Acquire(t)
	ctx := context.Background()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	user := uuid.New()
	exec(`INSERT INTO users (id, email, name) VALUES ($1, $2, 'zz')`, user, user.String()[:8]+"@sync.test")
	personal, project := uuid.New(), uuid.New()
	exec(`INSERT INTO hubs (id, name, slug, hub_type, owner_id) VALUES ($1, 'Personal', $2, 'personal', $3)`, personal, personal.String(), user)
	exec(`INSERT INTO hubs (id, name, slug, hub_type, owner_id, space_kind, repository) VALUES ($1, 'web', $2, 'team', $3, 'project', 'acme/web')`,
		project, project.String(), user)
	now := time.Now()
	for _, c := range []model.AgentConfig{
		{ID: uuid.NewString(), OwnerID: user.String(), Agent: "claude-code", FilePath: "CLAUDE.md", Scope: "global", Content: "cloud", ContentHash: "cloud-1", Version: 1},
		{ID: uuid.NewString(), OwnerID: user.String(), Agent: "claude-code", FilePath: "CLAUDE.md", Scope: "project:https://github.com/acme/web", Content: "cloud", ContentHash: "cloud-2", Version: 1},
		{ID: uuid.NewString(), OwnerID: user.String(), Agent: "claude-code", FilePath: "CLAUDE.md", Scope: "project:https://github.com/acme/api", Content: "cloud", ContentHash: "cloud-3", Version: 1},
	} {
		c.CreatedAt, c.UpdatedAt = now, now
		if err := st.UpsertAgentConfig(&c); err != nil {
			t.Fatal(err)
		}
	}
	h := NewConfigsHandler(st, nil)
	h.SetSpaceModes(spacemode.New(pool))
	// The device has none of them: V1 would pull all three.
	sync := func() (map[string]map[string]any, string) {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/v1/configs/sync", bytes.NewBufferString(`{"device_id":"laptop","configs":[]}`))
		req = req.WithContext(context.WithValue(req.Context(), userIDKey, user.String()))
		rec := httptest.NewRecorder()
		h.Sync(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("sync: %d %s", rec.Code, rec.Body.String())
		}
		var resp struct {
			Data struct {
				Actions []map[string]any `json:"actions"`
			} `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		out := map[string]map[string]any{}
		for _, a := range resp.Data.Actions {
			out[a["scope"].(string)] = a
		}
		return out, rec.Header().Get("X-Memax-Warning")
	}
	got, warning := sync()
	for scope, a := range got {
		if a["action"] != "pull" {
			t.Errorf("on V1, %s: %v", scope, a)
		}
	}
	if warning != "" {
		t.Errorf("on V1, warning %q", warning)
	}

	modes := spacemode.New(pool)
	for _, id := range []uuid.UUID{personal, project} {
		if err := modes.Enable(ctx, id, now); err != nil {
			t.Fatal(err)
		}
	}
	got, warning = sync()
	for _, scope := range []string{"global", "project:https://github.com/acme/web"} {
		a := got[scope]
		if a["action"] != "unchanged" || a["reason"] != "space_on_v2" || a["version"] != nil {
			t.Errorf("on V2, %s: %v", scope, a)
		}
	}
	if a := got["project:https://github.com/acme/api"]; a["action"] != "pull" {
		t.Errorf("a repository with no space on V2 still syncs: %v", a)
	}
	if warning != "space_on_v2" {
		t.Errorf("warning %q", warning)
	}

	// Back on V1: it syncs again.
	if err := modes.Disable(ctx, project); err != nil {
		t.Fatal(err)
	}
	got, _ = sync()
	if a := got["project:https://github.com/acme/web"]; a["action"] != "pull" {
		t.Errorf("back on V1: %v", a)
	}
	if a := got["global"]; a["action"] != "unchanged" {
		t.Errorf("the personal space is still on V2: %v", a)
	}
}
