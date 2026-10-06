package v2api_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/handler/v2api"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/receiptchain"
)

// Activity's "sealed through receipt N": the checkpoints, the head and the
// keys, in scope; another person's space is not found.
func TestCheckpointEndpoint(t *testing.T) {
	t.Parallel()
	signer, err := receiptchain.NewEd25519Signer(bytes.Repeat([]byte{3}, 32))
	if err != nil {
		t.Fatal(err)
	}
	keys := receiptchain.Keyring{}
	keys.Add(signer.Public())
	e := newEnv(t, v2api.WithReceiptKeys(keys))
	zz, jy := e.user("zz"), e.user("jy")
	sp := e.space(zz, policy.SpaceProject, "memax-v2")
	foreign := e.space(jy, policy.SpaceProject, "foreign")
	tok := e.session(zz)
	path := "/v2/spaces/" + sp.slug + "/checkpoints"

	type checkpointsWire struct {
		Items []struct {
			Number      int64     `json:"number"`
			From        int64     `json:"position_from"`
			To          int64     `json:"position_to"`
			Receipts    int       `json:"receipts"`
			Last        uuid.UUID `json:"last_receipt_id"`
			ChainSHA256 string    `json:"chain_sha256"`
			Signed      bool      `json:"signed"`
			KeyID       string    `json:"key_id"`
			Signature   string    `json:"signature"`
		} `json:"items"`
		HasMore bool   `json:"has_more"`
		Next    string `json:"next_cursor"`
		Seal    struct {
			SealedReceipts int64      `json:"sealed_receipts"`
			ThroughSeq     *int64     `json:"sealed_through_seq"`
			ThroughReceipt *uuid.UUID `json:"sealed_through_receipt_id"`
			Head           string     `json:"head_sha256"`
			Checkpoints    int64      `json:"checkpoints"`
			Unsealed       int        `json:"unsealed"`
		} `json:"seal"`
		Keys []struct {
			KeyID     string `json:"key_id"`
			Algorithm string `json:"algorithm"`
			PublicKey string `json:"public_key"`
		} `json:"keys"`
	}

	// Before anything is sealed: nothing, and what waits.
	e.remember(tok, sp, "Background jobs run on River.")
	var got checkpointsWire
	e.do(call{method: "GET", path: path, token: tok}).ok(http.StatusOK, &got)
	if len(got.Items) != 0 || got.Seal.SealedReceipts != 0 || got.Seal.Unsealed != 1 || got.Seal.ThroughSeq != nil || len(got.Keys) != 1 {
		t.Fatalf("before sealing = %+v", got)
	}

	ctx := context.Background()
	e.remember(tok, sp, "pnpm workspaces only.")
	deadline := time.Now().Add(90 * time.Second) // the watermark is the cluster's
	for {
		res, err := e.ledger.SealSpace(ctx, sp.id, signer, 1)
		if err != nil {
			t.Fatal(err)
		}
		if n := e.count(`SELECT position FROM v2.receipt_chain_heads WHERE space_id = $1`, sp.id); n == 2 && !res.More {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("sealing never caught up")
		}
		time.Sleep(10 * time.Millisecond)
	}
	e.do(call{method: "GET", path: path + "?limit=1", token: tok}).ok(http.StatusOK, &got)
	if len(got.Items) != 1 || !got.HasMore || got.Items[0].Number != 2 || got.Items[0].From != 2 || !got.Items[0].Signed ||
		got.Items[0].KeyID != signer.KeyID() || got.Seal.SealedReceipts != 2 || got.Seal.Checkpoints != 2 ||
		got.Seal.ThroughReceipt == nil || *got.Seal.ThroughReceipt != got.Items[0].Last || got.Seal.Head != got.Items[0].ChainSHA256 ||
		got.Seal.Unsealed != 0 {
		t.Errorf("checkpoints = %+v", got)
	}
	if got.Keys[0].KeyID != signer.KeyID() || got.Keys[0].Algorithm != "ed25519" ||
		got.Keys[0].PublicKey != base64.StdEncoding.EncodeToString(signer.Public()) {
		t.Errorf("keys = %+v", got.Keys)
	}
	if sig, err := base64.StdEncoding.DecodeString(got.Items[0].Signature); err != nil || len(sig) != 64 {
		t.Errorf("signature %q: %v", got.Items[0].Signature, err)
	}
	e.do(call{method: "GET", path: path + "?cursor=" + got.Next, token: tok}).ok(http.StatusOK, &got)
	if len(got.Items) != 1 || got.Items[0].Number != 1 || got.HasMore {
		t.Errorf("page 2 = %+v", got.Items)
	}
	e.do(call{method: "GET", path: "/v2/spaces/" + foreign.id.String() + "/checkpoints", token: tok}).fails(http.StatusNotFound, "not_found")
}
