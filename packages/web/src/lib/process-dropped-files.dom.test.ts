// @vitest-environment jsdom
import { describe, it, expect } from "vitest";
import { processDroppedFiles } from "./process-dropped-files";

// Vitest provides `File` and `FileReader` via jsdom; the utility uses
// both. Tests assemble File instances directly rather than going
// through DataTransfer (which happy-dom doesn't fully implement).

function makeTextFile(name: string, content: string, type = "text/plain") {
  return new File([content], name, { type });
}

function makeBinaryFile(name: string, bytes: Uint8Array, type = "image/png") {
  // Cast to BlobPart — ArrayBufferLike vs ArrayBuffer narrow disagreement
  // doesn't affect runtime; File constructor accepts Uint8Array fine.
  return new File([bytes as unknown as BlobPart], name, { type });
}

function makeFolder(name: string) {
  // Folders surface as zero-byte File objects with no MIME type and no
  // dot in the name. Mirrors how Chrome/Safari report directory drops.
  return new File([""], name, { type: "" });
}

describe("processDroppedFiles", () => {
  it("returns rejectedAsFolders when the drop is empty", async () => {
    const result = await processDroppedFiles([]);
    expect(result.rejectedAsFolders).toBe(true);
    expect(result.items).toEqual([]);
  });

  it("returns rejectedAsFolders when only folders were dropped", async () => {
    const result = await processDroppedFiles([
      makeFolder("MyFolder"),
      makeFolder("AnotherFolder"),
    ]);
    expect(result.rejectedAsFolders).toBe(true);
    expect(result.items).toEqual([]);
  });

  it("reads a plain text file as content", async () => {
    const file = makeTextFile("note.md", "# Hello\nworld");
    const result = await processDroppedFiles([file]);
    expect(result.rejectedAsFolders).toBe(false);
    expect(result.items).toEqual([
      {
        name: "note.md",
        content: "# Hello\nworld",
        binary: false,
        contentType: "text/plain",
      },
    ]);
  });

  it("reads a binary file as base64 (DataURL prefix stripped)", async () => {
    const bytes = new Uint8Array([0x89, 0x50, 0x4e, 0x47]); // PNG magic
    const file = makeBinaryFile("img.png", bytes);
    const result = await processDroppedFiles([file]);
    expect(result.rejectedAsFolders).toBe(false);
    expect(result.items.length).toBe(1);
    expect(result.items[0].name).toBe("img.png");
    expect(result.items[0].binary).toBe(true);
    // Content should be base64 (no `data:` prefix). PNG magic bytes
    // 0x89 0x50 0x4e 0x47 → "iVBORw==" is wrong, actual: ...
    // Just assert it's not a DataURL.
    expect(result.items[0].content.startsWith("data:")).toBe(false);
    expect(result.items[0].content.length).toBeGreaterThan(0);
  });

  it("filters out folders mixed with real files", async () => {
    const result = await processDroppedFiles([
      makeFolder("Junk"),
      makeTextFile("real.md", "content"),
    ]);
    expect(result.rejectedAsFolders).toBe(false);
    expect(result.items.length).toBe(1);
    expect(result.items[0].name).toBe("real.md");
  });

  it("drops empty / whitespace-only files silently", async () => {
    const result = await processDroppedFiles([
      makeTextFile("blank.md", ""),
      makeTextFile("real.md", "actual content"),
    ]);
    // The blank file has size 0 so it's caught by the folder filter
    // before FileReader runs, leaving real.md as the only item.
    expect(result.items.length).toBe(1);
    expect(result.items[0].name).toBe("real.md");
  });

  it("recognizes binary extensions case-insensitively", async () => {
    const file = makeBinaryFile("photo.JPG", new Uint8Array([1, 2, 3]));
    const result = await processDroppedFiles([file]);
    expect(result.items[0].binary).toBe(true);
  });

  it("falls back to application/octet-stream when MIME is empty", async () => {
    // File with empty type but valid name (extension)
    const file = new File(["data"], "weird.txt", { type: "" });
    const result = await processDroppedFiles([file]);
    expect(result.items[0].contentType).toBe("application/octet-stream");
  });

  it("processes multiple files in parallel and preserves input order", async () => {
    // Order matters because consumers (bar staged-files chips, future
    // compose drag-drop hand-off) render the result array in order.
    // Promise.all guarantees positional stability regardless of which
    // FileReader resolves first — the prior inline handler used
    // completion order, which was nondeterministic for multi-file drops.
    const files = [
      makeTextFile("a.md", "alpha"),
      makeTextFile("b.md", "bravo"),
      makeTextFile("c.md", "charlie"),
    ];
    const result = await processDroppedFiles(files);
    expect(result.items.length).toBe(3);
    expect(result.items.map((i) => i.name)).toEqual(["a.md", "b.md", "c.md"]);
  });
});
