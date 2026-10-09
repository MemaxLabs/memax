import { describe, expect, it } from "vitest";
import { CLI_DEFAULT_API, cliCommand, cliPrefix } from "./cli";

describe("the CLI command the app shows", () => {
  it("is the released CLI as drawn, in production", () => {
    expect(cliPrefix(CLI_DEFAULT_API, null)).toBe("npx memax-cli");
  });

  it("aims the CLI at this app's API and its tag elsewhere", () => {
    expect(cliPrefix("https://staging-api.memaxlabs.com", "alpha")).toBe(
      "MEMAX_API_URL=https://staging-api.memaxlabs.com npx memax-cli@alpha",
    );
    expect(cliPrefix("http://localhost:8080", null)).toBe(
      "MEMAX_API_URL=http://localhost:8080 npx memax-cli",
    );
  });

  it("puts the subcommand after it", () => {
    // Tests build without NEXT_PUBLIC_API_URL or NEXT_PUBLIC_CLI_TAG.
    expect(cliCommand("init --space acme-web")).toBe(
      "npx memax-cli init --space acme-web",
    );
  });
});
