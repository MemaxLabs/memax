import type { Memax } from "memax-sdk";
import {
  ConsentLoadError,
  consentEnding,
  toConsentRequest,
  type ConsentSource,
} from "./consent";

/**
 * The consent request over the SDK's public client (consent.ts). It needs
 * no session: the request's consent token is what lets the page read it,
 * and the person it is signed in as may not be this browser's.
 */
export function createSdkConsent(client: Pick<Memax, "auth">): ConsentSource {
  return {
    async load({ requestId, token }) {
      try {
        return toConsentRequest(
          await client.auth.getOAuthConsentRequest(requestId, token),
        );
      } catch (err) {
        throw new ConsentLoadError(consentEnding(err));
      }
    },
  };
}
