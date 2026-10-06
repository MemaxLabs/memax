/**
 * `chatgpt`: project instructions to copy out.
 *
 * OpenAI has no API for writing ChatGPT project instructions, so this target
 * can't be kept in sync and isn't a file on disk. ChatGPT reads the record
 * live through the memax connector; this is the plain-text copy a person
 * pastes into the project once, and again after it changes.
 */
import { compose, headerPlain } from "../document.js";
import { select } from "../select.js";
import type { Adapter } from "./adapter.js";

export const chatgpt: Adapter = {
  kind: "chatgpt",
  version: 1,
  role: "copy",
  tool: "ChatGPT",
  defaultPath: null,
  pathHint: "(chatgpt is copied out, so it takes no path)",
  acceptsPath: () => false,
  limits: {},
  reads: () => null,

  render(model, target, limit) {
    const selection = select(model, target, {
      accepts: (f) =>
        (f.agents.length === 0 || f.agents.includes("chatgpt")) &&
        (f.paths.length === 0 || target.scoped === "inline"),
      prose: true,
    });
    const { title, summary } = model.brief;
    const head = [
      headerPlain(model),
      "",
      title,
      ...(summary ? [summary] : []),
      "Every line cites a memory.",
    ];
    const tail = [
      "Live context",
      "Use the memax connector for anything not here: search_memories, get_memory. Propose new memories with save_memory.",
    ];
    const doc = compose(selection, { style: "plain", head, tail }, limit);
    return [
      {
        path: null,
        label: "ChatGPT project instructions",
        content: doc.content,
        driftText: doc.content,
        userOwned: false,
        doc,
      },
    ];
  },
};
