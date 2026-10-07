package handler_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/handler"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/spacemode"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
)

// An old CLI's push into a space on V2 still saves (V1's API is frozen,
// not broken): the memory becomes a note, which Dream folds into proposals
// for Review, and the answer says so (X-Memax-Warning: space_on_v2). Back
// on V1, the same push answers as V1 always did.
func TestMemoriesCreate_IntoASpaceOnV2Warns(t *testing.T) {
	t.Parallel()
	s, pool := testdb.Acquire(t)
	ctx := context.Background()
	ownerID := uuid.NewString()
	if _, err := pool.Exec(ctx, `INSERT INTO users (id, email, name, personal_plan_id) VALUES ($1::uuid, $2, 'C', 'personal_early_access')`,
		ownerID, ownerID[:8]+"@v2"); err != nil {
		t.Fatal(err)
	}
	hubID := uuid.NewString()
	if err := s.CreateHub(&model.Hub{ID: hubID, Name: "P", Slug: "v2-" + uuid.NewString()[:6], HubType: "personal", OwnerID: ownerID}); err != nil {
		t.Fatal(err)
	}
	if err := s.AddHubMember(hubID, ownerID, "owner"); err != nil {
		t.Fatal(err)
	}
	modes := spacemode.New(pool)
	h := handler.NewMemoriesHandler(s, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	h.SetEnqueue(func(string, string, model.PushRequest) {})
	h.SetSpaceModes(modes)

	rec := postCreate(t, h, ownerID, hubID, map[string]any{"title": "On V1", "content": "A push while the space is on V1."})
	if rec.Code != http.StatusCreated || rec.Header().Get("X-Memax-Warning") != "" {
		t.Fatalf("on V1: %d %q %s", rec.Code, rec.Header().Get("X-Memax-Warning"), rec.Body.String())
	}
	if err := modes.Enable(ctx, uuid.MustParse(hubID), time.Now()); err != nil {
		t.Fatal(err)
	}
	rec = postCreate(t, h, ownerID, hubID, map[string]any{"title": "On V2", "content": "A push from an old CLI after the switch."})
	if rec.Code != http.StatusCreated || rec.Header().Get("X-Memax-Warning") != "space_on_v2" {
		t.Fatalf("on V2: %d %q %s", rec.Code, rec.Header().Get("X-Memax-Warning"), rec.Body.String())
	}
	if err := modes.Disable(ctx, uuid.MustParse(hubID)); err != nil {
		t.Fatal(err)
	}
	rec = postCreate(t, h, ownerID, hubID, map[string]any{"title": "Back", "content": "A push after switching back."})
	if rec.Code != http.StatusCreated || rec.Header().Get("X-Memax-Warning") != "" {
		t.Fatalf("back on V1: %d %q", rec.Code, rec.Header().Get("X-Memax-Warning"))
	}
}
