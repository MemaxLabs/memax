import { describe, it, expect } from "vitest";
import { applyHubSettingsPatch, countHubOverrides } from "./use-hub-settings";

// Pure-helper tests. The mutation itself requires a QueryClient
// context which the existing hook tests skip; the interesting logic
// lives in these reducers. If these drift the optimistic cache
// would diverge from the server, so they're worth locking down.
describe("applyHubSettingsPatch", () => {
  it("sets a new key when existing is undefined", () => {
    expect(
      applyHubSettingsPatch(undefined, { dreams_merge_enabled: false }),
    ).toEqual({ dreams_merge_enabled: false });
  });

  it("sets a key when existing is empty", () => {
    expect(applyHubSettingsPatch({}, { dreams_enabled: true })).toEqual({
      dreams_enabled: true,
    });
  });

  it("overrides an existing key", () => {
    expect(
      applyHubSettingsPatch(
        { dreams_merge_enabled: true },
        { dreams_merge_enabled: false },
      ),
    ).toEqual({ dreams_merge_enabled: false });
  });

  it("preserves untouched keys", () => {
    expect(
      applyHubSettingsPatch(
        { dreams_merge_enabled: true, dreams_archive_enabled: false },
        { dreams_merge_enabled: false },
      ),
    ).toEqual({ dreams_merge_enabled: false, dreams_archive_enabled: false });
  });

  it("null deletes an override", () => {
    expect(
      applyHubSettingsPatch(
        { dreams_merge_enabled: false, dreams_archive_enabled: true },
        { dreams_merge_enabled: null },
      ),
    ).toEqual({ dreams_archive_enabled: true });
  });

  it("null on a missing key is a no-op", () => {
    expect(
      applyHubSettingsPatch(
        { dreams_archive_enabled: true },
        { dreams_merge_enabled: null },
      ),
    ).toEqual({ dreams_archive_enabled: true });
  });
});

describe("countHubOverrides", () => {
  it("is 0 when settings is undefined", () => {
    expect(countHubOverrides({ settings: undefined })).toBe(0);
  });

  it("is 0 when settings is empty", () => {
    expect(countHubOverrides({ settings: {} })).toBe(0);
  });

  it("counts explicit keys", () => {
    expect(
      countHubOverrides({
        settings: {
          dreams_merge_enabled: false,
          dreams_restructure_enabled: true,
        },
      }),
    ).toBe(2);
  });
});
