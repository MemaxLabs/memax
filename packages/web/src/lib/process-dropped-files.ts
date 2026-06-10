/**
 * Shared file-drop processor — used by the global bar drop handler and
 * (plan 20 P2-4) by future compose-modal drag-drop integration.
 *
 * Behavior:
 *   - Folders are filtered out at the FileList level (size > 0 AND
 *     either a MIME type OR a name with an extension). Showing folders
 *     in the staged list would be a footgun because their contents
 *     don't enumerate via the File API.
 *   - Binary file extensions (PDF, image formats) are read as base64
 *     DataURLs (the raw `data:...` prefix is stripped). Everything
 *     else is read as plain text.
 *   - Per-file failures (read error, empty content) are silently
 *     dropped from the result rather than rejecting the whole batch —
 *     mirrors the bar's prior behavior.
 *
 * Returns a Promise so callers can `await` and then dispatch into
 * their own state (bar staging, compose draft hand-off, etc).
 */

const BINARY_EXT = [".pdf", ".png", ".jpg", ".jpeg", ".webp", ".gif", ".bmp"];

/**
 * `true` when the filename's extension is a known binary type that the
 * drop pipeline should base64-encode (PDF + raster image formats).
 * Exported so callers that bypass `processDroppedFiles` (e.g. compose-
 * modal staging an attachment from a File handle directly) can apply
 * the same binary/text classification.
 */
export function isBinaryFileName(name: string): boolean {
  return BINARY_EXT.some((ext) => name.toLowerCase().endsWith(ext));
}

const isBinaryExt = isBinaryFileName;

export interface DroppedFileItem {
  name: string;
  content: string;
  binary: boolean;
  contentType: string;
}

export interface ProcessDroppedFilesResult {
  /**
   * Files successfully read and ready to stage. Empty when the drop
   * was all folders / all read errors.
   */
  items: DroppedFileItem[];
  /**
   * `true` when the drop contained no real files — every entry was
   * a folder or a zero-byte placeholder. Callers typically surface a
   * "drop files, not folders" error when this fires.
   */
  rejectedAsFolders: boolean;
}

/**
 * Filter folders from a FileList — folders surface as zero-byte
 * entries with no MIME type and no extension. Real files have at
 * least one of: nonzero size, a MIME type, or a `.ext` in the name.
 */
function filterRealFiles(files: FileList | File[]): File[] {
  return Array.from(files).filter(
    (f) => f.size > 0 && (f.type !== "" || f.name.includes(".")),
  );
}

function readOne(file: File): Promise<DroppedFileItem | null> {
  return new Promise((resolve) => {
    const binary = isBinaryExt(file.name);
    const reader = new FileReader();
    reader.onload = () => {
      const raw = reader.result as string;
      // For binary reads, FileReader returns `data:<mime>;base64,<data>`.
      // Strip the prefix so callers receive just the base64 payload.
      const content = binary ? raw.split(",")[1] || raw : raw;
      if (!content.trim()) {
        resolve(null);
        return;
      }
      resolve({
        name: file.name,
        content,
        binary,
        contentType: file.type || "application/octet-stream",
      });
    };
    reader.onerror = () => resolve(null);
    if (binary) {
      reader.readAsDataURL(file);
    } else {
      reader.readAsText(file);
    }
  });
}

/**
 * Read every droppable file in `files`, in parallel, and resolve to
 * the array of successfully-read entries plus a `rejectedAsFolders`
 * flag if no real files survived the filter. Pure — does not touch
 * any UI state. Callers compose this with their own staging logic.
 *
 * `options.skipBinary` (default false): when true, binary files are
 * filtered out BEFORE the read step so callers that handle binaries
 * separately (e.g. compose-modal staging an attachment from the File
 * directly) don't pay the cost of `readAsDataURL` + base64 encoding
 * a multi-MB image they never use. Codex 5.5 caught the duplicate-read.
 */
export async function processDroppedFiles(
  files: FileList | File[],
  options: { skipBinary?: boolean } = {},
): Promise<ProcessDroppedFilesResult> {
  let real = filterRealFiles(files);
  if (options.skipBinary) {
    real = real.filter((f) => !isBinaryExt(f.name));
  }
  if (real.length === 0) {
    return { items: [], rejectedAsFolders: true };
  }
  const settled = await Promise.all(real.map(readOne));
  return {
    items: settled.filter((x): x is DroppedFileItem => x !== null),
    rejectedAsFolders: false,
  };
}
