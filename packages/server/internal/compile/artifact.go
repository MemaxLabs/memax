package compile

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/objectstore"
)

// Artifacts are what a compile produced and what a device observed, in
// object storage (R2 in production). Postgres holds their keys and
// hashes; the content is served only through the API, under scope.
//
// Keys:
//
//	v2/spaces/<space>/targets/<target>/compiles/<C-ref>.json   a compile's outputs
//	v2/spaces/<space>/targets/<target>/observed/<sha256>       an observed file
//
// Observed content is keyed by its hash, so reporting the same edit twice
// stores it once. Forget will have to reach both kinds (plan 25 §5.13):
// their keys are on compile_runs and target_observations.

// maxArtifact bounds what is read back.
const maxArtifact = 16 << 20

// Artifact is a compile run's stored outputs.
type Artifact struct {
	Version int    `json:"version"`
	Compile string `json:"compile"`
	Kind    string `json:"kind"`
	// Reads is the canonical file the tool also reads ("AGENTS.md" for
	// Cursor), when it has one.
	Reads    string                  `json:"reads,omitempty"`
	Files    []ArtifactOutput        `json:"files"`
	Copies   []ArtifactOutput        `json:"copies"`
	Warnings []ledger.CompileWarning `json:"warnings"`
}

// ArtifactOutput is one output with its content.
type ArtifactOutput struct {
	ledger.CompiledOutput
	Content string `json:"content"`
}

func compileKey(spaceID, targetID uuid.UUID, ref string) string {
	return fmt.Sprintf("v2/spaces/%s/targets/%s/compiles/%s.json", spaceID, targetID, ref)
}

func observedKey(spaceID, targetID uuid.UUID, sha string) string {
	return fmt.Sprintf("v2/spaces/%s/targets/%s/observed/%s", spaceID, targetID, sha)
}

func newArtifact(ref string, res *Result, warns []ledger.CompileWarning) *Artifact {
	a := &Artifact{Version: 1, Compile: ref, Files: []ArtifactOutput{}, Copies: []ArtifactOutput{}, Warnings: warns}
	for _, t := range res.Targets {
		a.Kind = t.Kind
		if t.Reads != nil {
			a.Reads = *t.Reads
		}
	}
	outs := outputs(res)
	for i, f := range res.Files {
		a.Files = append(a.Files, ArtifactOutput{CompiledOutput: outs[i], Content: f.Content})
	}
	for i, c := range res.Copies {
		a.Copies = append(a.Copies, ArtifactOutput{CompiledOutput: outs[len(res.Files)+i], Content: c.Content})
	}
	return a
}

func putJSON(ctx context.Context, store objectstore.Store, key string, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return store.Put(ctx, objectstore.PutInput{
		Key: key, Body: bytes.NewReader(raw), ContentType: "application/json", SizeBytes: int64(len(raw)),
	})
}

func putText(ctx context.Context, store objectstore.Store, key, text string) error {
	return store.Put(ctx, objectstore.PutInput{
		Key: key, Body: bytes.NewReader([]byte(text)), ContentType: "text/markdown; charset=utf-8", SizeBytes: int64(len(text)),
	})
}

func get(ctx context.Context, store objectstore.Store, key string) ([]byte, error) {
	res, err := store.Get(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("compile: read artifact: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(res.Body, maxArtifact+1))
	if err != nil {
		return nil, fmt.Errorf("compile: read artifact: %w", err)
	}
	if len(raw) > maxArtifact {
		return nil, fmt.Errorf("compile: artifact %s is over %d bytes", key, maxArtifact)
	}
	return raw, nil
}

func loadArtifact(ctx context.Context, store objectstore.Store, key string) (*Artifact, error) {
	raw, err := get(ctx, store, key)
	if err != nil {
		return nil, err
	}
	var a Artifact
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, fmt.Errorf("compile: artifact %s: %w", key, err)
	}
	return &a, nil
}

// file is one path's content in an artifact.
func (a *Artifact) file(path string) (ArtifactOutput, bool) {
	for _, f := range a.Files {
		if f.Path == path {
			return f, true
		}
	}
	return ArtifactOutput{}, false
}
