// Admin client for the per-person V2 UI flag (plan 25 E1, the server's
// internal/v2ui): GET and PUT /v1/admin/users/{id}/v2-ui. Web-only, like
// every admin call; never in the public SDK.

import { adminReq } from "./transport";

/** An operator's choice for one person: the rules, on, or off (wins). */
export type AdminV2UISetting = "default" | "on" | "off";

/** Which rule decided. */
export type AdminV2UIReason =
  | "operator_off"
  | "operator_on"
  | "v2_space"
  | "signed_up_since"
  | "none";

/** A person's V2 UI flag, why, and the operator's choice. */
export interface AdminV2UI {
  user_id: string;
  ui: "v1" | "v2";
  reason: AdminV2UIReason;
  setting: AdminV2UISetting;
  /** V2_UI_SINCE (accounts created at or after it get V2), or null. */
  since: string | null;
}

function path(userId: string): string {
  return `/v1/admin/users/${encodeURIComponent(userId)}/v2-ui`;
}

export const adminV2UIClient = {
  get(userId: string): Promise<AdminV2UI> {
    return adminReq<AdminV2UI>("GET", path(userId));
  },
  set(userId: string, setting: AdminV2UISetting): Promise<AdminV2UI> {
    return adminReq<AdminV2UI>("PUT", path(userId), { setting });
  },
};
