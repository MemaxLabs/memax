// A rename's new words, offered by memax verify and VerifyStale.
import { describe, expect, it } from "vitest";
import { renameSuggestion } from "./suggest.js";

describe("a rename's suggestion", () => {
  it("replaces the old path with the new one", () => {
    expect(
      renameSuggestion(
        "The tiers live in internal/ask/config.go; see internal/ask/config.go.",
        "internal/ask/config.go",
        "internal/ask/tiers.go",
      ),
    ).toBe(
      "The tiers live in internal/ask/tiers.go; see internal/ask/tiers.go.",
    );
  });

  it("replaces a file name named once on its own", () => {
    expect(
      renameSuggestion(
        "Ask's tiers are in `config.go`.",
        "internal/ask/config.go",
        "internal/ask/tiers.go",
      ),
    ).toBe("Ask's tiers are in `tiers.go`.");
  });

  it("suggests nothing it can't place", () => {
    const from = "internal/ask/config.go";
    const to = "internal/ask/tiers.go";
    // Not named at all; inside a longer name; another folder's file; twice.
    expect(renameSuggestion("Ask has three tiers.", from, to)).toBeNull();
    expect(renameSuggestion("See myconfig.go.", from, to)).toBeNull();
    expect(renameSuggestion("See other/config.go.", from, to)).toBeNull();
    expect(renameSuggestion("config.go and config.go", from, to)).toBeNull();
    // Moved, not renamed: the file name alone says nothing new.
    expect(
      renameSuggestion("See config.go.", from, "internal/tiers/config.go"),
    ).toBeNull();
  });
});
