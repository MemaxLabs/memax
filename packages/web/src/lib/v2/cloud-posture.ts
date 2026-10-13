import type { V2 } from "memax-sdk";
import raw from "./cloud-posture.json";
import type { SecurityView } from "./data/settings";
import { securityOf } from "./data/settings-sdk";

/**
 * Memax cloud's posture, for the public Security page (/security): where
 * data lives, who else sees a memory's words and on which hosts, and how
 * long backups keep what Forget removed.
 *
 * The page is static and open to anyone, so it can't read GET
 * /v2/security (a person's own). cloud-posture.json is what the server's
 * internal/trust says with its defaults and every key set, written by its
 * TestCloudPostureMatchesThePublicPage (-update), so a change to a tier,
 * a host list or a floor fails the server's tests until this file and the
 * page are reviewed with it. It goes through the same mapping as
 * /v2/security's answer, and the same words as Settings › Security.
 */
export type CloudPosture = Pick<
  SecurityView,
  "residency" | "processors" | "backupDays"
>;

type WirePosture = Pick<
  V2.Security,
  "residency" | "processors" | "backup_days"
>;

function postureOf(wire: WirePosture): CloudPosture {
  // Who is asking never matters for the cloud's posture: the session's
  // fields are filled in only to reuse /v2/security's mapping.
  const { residency, processors, backupDays } = securityOf({
    ...wire,
    assurance: "client_attested",
    passkey_check: false,
  });
  return { residency, processors, backupDays };
}

export const CLOUD_POSTURE: CloudPosture = postureOf(raw as WirePosture);
