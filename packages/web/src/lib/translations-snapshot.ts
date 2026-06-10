/**
 * Module-level snapshot of the current user-visible translations.
 *
 * # Why this exists
 *
 * Some code runs outside React — notably the TanStack Query
 * MutationCache handlers in `query-client.ts`, which need to read
 * i18n copy to build user-facing error messages. Hooks (`useLocale`)
 * aren't available there.
 *
 * # Contract
 *
 * `LocaleProvider` calls `setTranslationsSnapshot(t)` whenever the
 * active locale changes. Consumers read `getTranslationsSnapshot()`.
 * The initial value is the English bundle so SSR / early calls get
 * *some* sensible text rather than undefined.
 *
 * This is the same pattern `mutation-toast.ts` uses for the React
 * → outside-React bridge, which we've shipped successfully twice.
 * A single module-level ref is cheaper than threading the
 * translations through every call site.
 *
 * Deliberate non-goals: reactivity (consumers re-read on each call),
 * locale detection (the hook layer owns that), locale persistence
 * (localStorage is still the hook's job).
 */
import { en, type Translations } from "@/i18n/locales/en";

let current: Translations = en;

/**
 * Called by `LocaleProvider` whenever `t` changes. Callers outside the
 * provider should treat this as "the provider is the only writer."
 */
export function setTranslationsSnapshot(next: Translations): void {
  current = next;
}

/**
 * Returns the last snapshot set by `LocaleProvider`, or the English
 * bundle if the provider hasn't mounted yet. Always non-null.
 */
export function getTranslationsSnapshot(): Translations {
  return current;
}
