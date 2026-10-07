import type { Memax } from "memax-sdk";
import {
  ConsentLoadError,
  consentEnding,
  consentRefusal,
  toConsentRequest,
  type ConsentSource,
} from "./consent";

/**
 * The consent request over the SDK (consent.ts), as the person signed in
 * here: the authed client goes through the web app's proxy, which attaches
 * the session's token and refuses requests other sites start.
 */
export function createSdkConsent(client: Pick<Memax, "auth">): ConsentSource {
  return {
    async load(requestId) {
      try {
        return toConsentRequest(await client.auth.openOAuthRequest(requestId));
      } catch (err) {
        throw new ConsentLoadError(consentEnding(err));
      }
    },
    async decide(requestId, d) {
      try {
        const res = await client.auth.decideOAuthRequest(
          requestId,
          d.decision === "approve"
            ? { decision: "approve", space_id: d.spaceId }
            : { decision: "deny" },
        );
        return res.redirect_to;
      } catch (err) {
        throw consentRefusal(err);
      }
    },
    async release(requestId) {
      try {
        await client.auth.releaseOAuthRequest(requestId);
      } catch (err) {
        throw consentRefusal(err);
      }
    },
  };
}
