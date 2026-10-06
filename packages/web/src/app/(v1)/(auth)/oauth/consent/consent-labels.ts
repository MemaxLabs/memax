import type {
  OAuthConsentHub,
  OAuthConsentPermission,
  OAuthConsentRequest,
} from "memax-sdk";

import type { Translations } from "@/i18n/locales/en";

export type ConsentLoadState =
  | { status: "loading" }
  | { status: "ready"; data: OAuthConsentRequest }
  | { status: "error"; message?: string };

export function toggleValue(values: string[], value: string, checked: boolean) {
  if (checked) {
    return values.includes(value) ? values : [...values, value];
  }
  return values.filter((item) => item !== value);
}

export function permissionCopy(
  permission: OAuthConsentPermission,
  t: Translations,
) {
  switch (permission.value) {
    case "memax:read":
      return {
        title: t.auth.oauthConsent.permissionReadTitle,
        description: t.auth.oauthConsent.permissionReadDesc,
      };
    case "memax:write":
      return {
        title: t.auth.oauthConsent.permissionWriteTitle,
        description: t.auth.oauthConsent.permissionWriteDesc,
      };
    default:
      return {
        title: t.auth.oauthConsent.permissionCustomTitle,
        description: t.auth.oauthConsent.permissionCustomDesc,
      };
  }
}

export function roleLabel(role: OAuthConsentHub["role"], t: Translations) {
  switch (role) {
    case "owner":
      return t.auth.oauthConsent.roleOwner;
    case "admin":
      return t.auth.oauthConsent.roleAdmin;
    case "contributor":
      return t.auth.oauthConsent.roleContributor;
    case "viewer":
      return t.auth.oauthConsent.roleViewer;
    default:
      return role;
  }
}

export function notRequestedLabel(item: string, t: Translations) {
  switch (item) {
    case "Delete memories":
      return t.auth.oauthConsent.notRequestedDelete;
    case "Manage topics or run dreams":
      return t.auth.oauthConsent.notRequestedOrganize;
    case "Manage hub settings or members":
      return t.auth.oauthConsent.notRequestedHubAdmin;
    default:
      return t.auth.oauthConsent.notRequestedCustom;
  }
}
