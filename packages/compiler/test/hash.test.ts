import { createHash } from "node:crypto";
import { describe, expect, it } from "vitest";
import { sha256Portable, utf8 } from "../src/hash.js";
import { sha256Hex } from "../src/index.js";
import { rng } from "./helpers.js";

const node = (text: string) =>
  createHash("sha256").update(text, "utf8").digest("hex");

describe("sha256", () => {
  it("matches the FIPS 180-4 test vectors", () => {
    expect(sha256Hex("")).toBe(
      "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
    );
    expect(sha256Hex("abc")).toBe(
      "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
    );
    expect(
      sha256Portable(
        utf8("abcdbcdecdefdefgefghfghighijhijkijkljklmklmnlmnomnopnopq"),
      ),
    ).toBe("248d6a61d20638b8e5c026930c3e6039a33ce45964ff2167f6ecedd419db06c1");
  });

  it("gives the same hash on the portable path as on node:crypto", () => {
    const random = rng(7);
    const alphabet = [
      "a",
      "Z",
      " ",
      "\n",
      "\u{e9}",
      "\u{90e8}",
      "\u{1f600}",
      "[M-0219]",
      "\r\n",
    ];
    for (let n = 0; n < 300; n++) {
      // Lengths around the 55/56/64-byte padding boundaries matter most.
      const length = n < 130 ? n : Math.floor(random() * 5000);
      let text = "";
      while (text.length < length)
        text += alphabet[Math.floor(random() * alphabet.length)];
      expect(sha256Portable(utf8(text))).toBe(node(text));
      expect(sha256Hex(text)).toBe(node(text));
    }
  });

  it("encodes lone surrogates as U+FFFD, like node:crypto", () => {
    const lone = "a\u{d800}b";
    expect(sha256Portable(utf8(lone))).toBe(node(lone));
  });
});
