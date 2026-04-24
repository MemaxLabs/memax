import type { Hub, HubWithRole } from "memax-sdk";
import { getClient } from "./client.js";

export const PERSONAL_HUB_ALIAS = "personal";

function normalizeHubRef(value: string): string {
  return value.trim().toLowerCase();
}

export function getHubReference(hub: Hub): string {
  return hub.hub_type === "personal" ? PERSONAL_HUB_ALIAS : hub.slug;
}

export function findHubMatch(
  hubs: HubWithRole[],
  ref: string,
): HubWithRole | undefined {
  const normalized = normalizeHubRef(ref);
  if (normalized === PERSONAL_HUB_ALIAS) {
    return hubs.find(({ hub }) => hub.hub_type === "personal");
  }
  return hubs.find(({ hub }) => {
    return (
      normalizeHubRef(hub.id) === normalized ||
      normalizeHubRef(hub.slug) === normalized
    );
  });
}

export async function resolveHubID(
  ref: string | undefined,
): Promise<string | undefined> {
  if (!ref) return undefined;
  const hubs = await getClient().hubs.list();
  return findHubMatch(hubs, ref)?.hub.id;
}

export async function requireHub(ref: string): Promise<HubWithRole> {
  const hubs = await getClient().hubs.list();
  const match = findHubMatch(hubs, ref);
  if (!match) {
    throw new Error(
      "Hub not found or not accessible. Run `memax hub list` to see available hubs.",
    );
  }
  return match;
}
