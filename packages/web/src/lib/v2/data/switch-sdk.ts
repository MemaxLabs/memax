import type { V2 } from "memax-sdk";
import { agentKey, type V2Client } from "./sdk-records";
import type { SwitchSource, SwitchView } from "./switch";

/**
 * Switch to V2 through memax.v2.spaces (`GET /v2/spaces/{space}/switch`,
 * `POST /v2/spaces/{space}:switch`). The server says what moves, runs the
 * steps (in the background when there's an import to judge) and resumes
 * a failed switch when asked again.
 */

export function switchViewOf(s: V2.SpaceSwitch): SwitchView {
  const pv = s.preview;
  const n = pv.notes;
  return {
    state: s.state,
    step: s.step,
    preview: {
      kind: pv.kind,
      kinds: pv.kinds,
      repository: pv.repository ?? null,
      suggestedRepository: pv.suggested_repository ?? null,
      members: pv.members.map((m) => ({
        name: m.name,
        v1Role: m.v1_role,
        role: m.role,
        canForget: m.can_forget,
      })),
      notes: {
        total: n.total,
        candidates: n.candidates,
        fold: n.fold,
        kept: n.kept,
        archived: n.archived,
        secret: n.secret,
      },
      personas: pv.personas,
      configs: pv.configs.length,
      targets: pv.targets,
      agents: pv.agents.map((a) => ({
        name: a.name,
        agent: agentKey(a.agent) ?? a.agent,
        autonomy: a.autonomy,
        connected: a.connected,
      })),
      gates: pv.gates,
      dreamRuns: pv.dream_runs,
      plan: pv.plan ?? null,
    },
    progress: {
      notes: s.progress.notes + s.progress.personas + s.progress.configs,
      proposed: s.progress.proposed,
      targets: s.progress.targets,
      connected: s.progress.connected,
      notified: s.progress.notified,
    },
    importId: s.import_id ?? null,
    error: s.error ?? null,
    switchedAt: s.switched_at ?? null,
  };
}

export function createSdkSwitch(client: V2Client): SwitchSource {
  return {
    async status({ space, signal }) {
      return switchViewOf(
        await client.v2.spaces.switchStatus(space.slug, { signal }),
      );
    },
    async toV2({ space, kind, idempotencyKey }) {
      return switchViewOf(
        await client.v2.spaces.switchToV2(space.slug, {
          idempotencyKey,
          ...(kind ? { kind } : {}),
        }),
      );
    },
    async toV1({ space, idempotencyKey }) {
      return switchViewOf(
        await client.v2.spaces.switchToV1(space.slug, { idempotencyKey }),
      );
    },
  };
}
