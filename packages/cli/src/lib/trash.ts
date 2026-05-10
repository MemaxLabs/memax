import {
  copyFileSync,
  existsSync,
  mkdirSync,
  renameSync,
  rmSync,
} from "node:fs";
import { dirname, join } from "node:path";
import { getConfigDir } from "./config.js";

export interface TrashMoveResult {
  moved: boolean;
  trashPath?: string;
}

function makeTrashPath(kind: string, sourcePath: string): string {
  const timestamp = new Date().toISOString().replace(/[:.]/g, "-");
  const normalized = sourcePath.replace(/^\/+/, "");
  return join(getConfigDir(), "trash", kind, timestamp, normalized);
}

export function moveFileToTrash(
  sourcePath: string,
  kind: "agent-configs",
): TrashMoveResult {
  if (!existsSync(sourcePath)) {
    return { moved: false };
  }
  const trashPath = makeTrashPath(kind, sourcePath);
  mkdirSync(dirname(trashPath), { recursive: true });
  try {
    renameSync(sourcePath, trashPath);
  } catch (err) {
    const error = err as NodeJS.ErrnoException;
    if (error.code !== "EXDEV") {
      throw err;
    }
    copyFileSync(sourcePath, trashPath);
    rmSync(sourcePath);
  }
  return { moved: true, trashPath };
}
