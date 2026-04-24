import { readFileSync, writeFileSync, mkdirSync, existsSync } from "node:fs";
import { join } from "node:path";
import { homedir } from "node:os";
import { randomUUID, createHash } from "node:crypto";

const CONFIG_DIR = join(homedir(), ".memax");
const CONFIG_FILE = join(CONFIG_DIR, "config.json");

export interface MemaxConfig {
  api_url: string;
  default_hub: string;
  active_hub_id?: string;
  default_boundary: string;
  auto_categorize: boolean;
  device_id?: string;
  sync_sources?: SyncSourceState[];
  agent_session_roots?: AgentSessionRoot[];
}

export interface AgentSessionRoot {
  id?: string;
  agent: string;
  root_path: string;
  scope: string;
  session_type?: string;
  include_extensions?: string[];
}

export interface SyncSourceState {
  id: string;
  root_path: string;
  kind: "directory";
  ignore_patterns: string[];
  default_boundary?: string;
  deletion_policy: "retain";
  last_sync_at?: string;
  last_mode?: "manual" | "watch";
  last_scan_count?: number;
  last_pushed?: number;
  last_skipped?: number;
  last_errors?: number;
}

// Published CLI ships pointed at prod. Local development overrides
// with `MEMAX_API_URL=http://localhost:8080` (or any other host).
const DEFAULT_CONFIG: MemaxConfig = {
  api_url: "https://api.memax.app",
  default_hub: "",
  default_boundary: "private",
  auto_categorize: true,
};

export function getConfigDir(): string {
  return CONFIG_DIR;
}

export function loadConfig(): MemaxConfig {
  // Env var overrides everything
  const envUrl = process.env.MEMAX_API_URL;

  if (!existsSync(CONFIG_FILE)) {
    return envUrl ? { ...DEFAULT_CONFIG, api_url: envUrl } : DEFAULT_CONFIG;
  }

  try {
    const raw = readFileSync(CONFIG_FILE, "utf-8");
    const config = { ...DEFAULT_CONFIG, ...JSON.parse(raw) };
    if (!config.active_hub_id && config.default_hub) {
      config.active_hub_id = config.default_hub;
    }
    if (envUrl) config.api_url = envUrl;
    return config;
  } catch {
    return envUrl ? { ...DEFAULT_CONFIG, api_url: envUrl } : DEFAULT_CONFIG;
  }
}

export function saveConfig(config: Partial<MemaxConfig>): void {
  mkdirSync(CONFIG_DIR, { recursive: true });

  let existing = DEFAULT_CONFIG;
  if (existsSync(CONFIG_FILE)) {
    try {
      existing = {
        ...DEFAULT_CONFIG,
        ...JSON.parse(readFileSync(CONFIG_FILE, "utf-8")),
      };
    } catch {
      // ignore parse errors
    }
  }

  const merged = { ...existing, ...config };
  writeFileSync(CONFIG_FILE, JSON.stringify(merged, null, 2) + "\n");
}

export function getActiveHubID(): string {
  const config = loadConfig();
  return config.active_hub_id?.trim() || config.default_hub?.trim() || "";
}

export function setActiveHubID(hubID: string): void {
  saveConfig({ active_hub_id: hubID });
}

export function getOrCreateDeviceID(): string {
  const config = loadConfig();
  if (config.device_id && config.device_id.trim()) {
    return config.device_id;
  }
  const deviceID = randomUUID();
  saveConfig({ device_id: deviceID });
  return deviceID;
}

export function makeSyncSourceID(rootPath: string): string {
  return createHash("sha256").update(rootPath).digest("hex").slice(0, 12);
}

export function listSyncSources(): SyncSourceState[] {
  return [...(loadConfig().sync_sources ?? [])].sort((a, b) =>
    a.root_path.localeCompare(b.root_path),
  );
}

export function upsertSyncSource(
  source: Omit<SyncSourceState, "id"> & { id?: string },
): SyncSourceState {
  const config = loadConfig();
  const syncSources = [...(config.sync_sources ?? [])];
  const id = source.id ?? makeSyncSourceID(source.root_path);
  const next: SyncSourceState = {
    id,
    root_path: source.root_path,
    kind: source.kind,
    ignore_patterns: [...source.ignore_patterns],
    default_boundary: source.default_boundary,
    deletion_policy: source.deletion_policy,
    last_sync_at: source.last_sync_at,
    last_mode: source.last_mode,
    last_scan_count: source.last_scan_count,
    last_pushed: source.last_pushed,
    last_skipped: source.last_skipped,
    last_errors: source.last_errors,
  };

  const existingIndex = syncSources.findIndex((item) => item.id === id);
  if (existingIndex >= 0) {
    syncSources[existingIndex] = next;
  } else {
    syncSources.push(next);
  }

  saveConfig({ sync_sources: syncSources });
  return next;
}

export function updateSyncSourceRun(
  rootPath: string,
  update: {
    ignorePatterns: string[];
    defaultBoundary?: string;
    mode: "manual" | "watch";
    scanCount: number;
    pushed: number;
    skipped: number;
    errors: number;
  },
): SyncSourceState {
  const existing = listSyncSources().find(
    (item) => item.root_path === rootPath,
  );
  return upsertSyncSource({
    id: existing?.id,
    root_path: rootPath,
    kind: "directory",
    ignore_patterns: update.ignorePatterns,
    default_boundary: update.defaultBoundary,
    deletion_policy: "retain",
    last_sync_at: new Date().toISOString(),
    last_mode: update.mode,
    last_scan_count: update.scanCount,
    last_pushed: update.pushed,
    last_skipped: update.skipped,
    last_errors: update.errors,
  });
}
