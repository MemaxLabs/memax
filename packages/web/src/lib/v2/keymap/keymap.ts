/**
 * The V2 keymap dispatcher: one keydown listener for the whole app
 * (plan §6.4), replacing V1's scattered window listeners.
 *
 * - Bindings come from registry.ts; handlers register by binding id in
 *   a scope.
 * - Scopes stack. The app scope is always there; pages push theirs
 *   (Review's K/E/X), and layers push modal ones (⌘K, the `?` sheet, the
 *   space menu). A newer scope wins over an older one, and a modal scope
 *   hides every scope below it.
 * - Sequences ("G T") wait up to `sequenceTimeout` for their next key.
 * - Keys are ignored while typing in a text field (unless the binding
 *   is a ⌘/Ctrl chord marked inInput), while an IME is composing, when
 *   a component already handled the event (defaultPrevented), and on
 *   auto-repeat unless the binding asks for it.
 *
 * DOM-free apart from duck-typed targets, so it's unit-tested in node.
 */

import {
  isBareChord,
  isModifierKey,
  matchesChord,
  parseKeys,
  type KeyEventLike,
  type KeySequence,
  type Platform,
} from "./keys";
import type { KeyBinding } from "./registry";

/** Return `false` to leave the key unhandled, so a lower scope can take it. */
export type KeyHandler = (
  event: KeyEventLike,
  match: { index: number },
) => void | boolean;

export interface KeyScope {
  readonly name: string;
  readonly modal: boolean;
  /** Handles a binding in this scope; returns the unregister function. */
  register(id: string, handler: KeyHandler): () => void;
  /** Removes the scope and its handlers. */
  dispose(): void;
}

export interface Keymap {
  /** The base scope: always active unless a modal scope is open. */
  readonly root: KeyScope;
  pushScope(name: string, options?: { modal?: boolean }): KeyScope;
  /** Feeds one keydown. Returns whether a handler took it. */
  handle(event: KeyEventLike): boolean;
  setPlatform(platform: Platform): void;
  readonly platform: Platform;
  /** Whether a sequence is half typed (G pressed, waiting for T). */
  readonly pending: boolean;
}

interface ScopeRecord {
  name: string;
  modal: boolean;
  order: number;
  handlers: Map<string, KeyHandler[]>;
  disposed: boolean;
}

interface CompiledBinding {
  binding: KeyBinding;
  sequences: KeySequence[];
}

/** The IME owns the keystroke: the Enter that commits it, or a candidate pick. */
export function isComposing(event: KeyEventLike): boolean {
  return event.isComposing === true || event.keyCode === 229;
}

const NON_TEXT_INPUTS = new Set([
  "button",
  "checkbox",
  "color",
  "file",
  "hidden",
  "image",
  "radio",
  "range",
  "reset",
  "submit",
]);

interface TargetLike {
  tagName?: string;
  type?: string;
  isContentEditable?: boolean;
  closest?: (selector: string) => unknown;
}

/** Whether keys typed here are text: inputs, textareas, selects and contenteditable. */
export function isTextEntry(target: unknown): boolean {
  if (!target || typeof target !== "object") return false;
  const el = target as TargetLike;
  if (el.isContentEditable) return true;
  const tag = el.tagName?.toUpperCase();
  if (tag === "TEXTAREA" || tag === "SELECT") return true;
  if (tag === "INPUT") {
    return !NON_TEXT_INPUTS.has((el.type ?? "text").toLowerCase());
  }
  return false;
}

function startsWith(
  sequence: KeySequence,
  prefix: readonly KeyEventLike[],
  event: KeyEventLike,
  platform: Platform,
): "complete" | "partial" | false {
  const steps = [...prefix, event];
  if (steps.length > sequence.length) return false;
  for (let i = 0; i < steps.length; i++) {
    if (!matchesChord(steps[i], sequence[i], platform)) return false;
  }
  return steps.length === sequence.length ? "complete" : "partial";
}

export function createKeymap({
  bindings,
  platform: initialPlatform = "other",
  sequenceTimeout = 1000,
  now = () => Date.now(),
}: {
  bindings: readonly KeyBinding[];
  platform?: Platform;
  sequenceTimeout?: number;
  now?: () => number;
}): Keymap {
  const compiled: CompiledBinding[] = bindings.map((binding) => ({
    binding,
    sequences: (binding.keys ?? []).map(parseKeys),
  }));
  const known = new Set(bindings.map((b) => b.id));
  const scopes: ScopeRecord[] = [];
  let order = 0;
  let platform: Platform = initialPlatform;
  let prefix: KeyEventLike[] = [];
  let prefixAt = 0;

  function makeScope(name: string, modal: boolean): KeyScope {
    const record: ScopeRecord = {
      name,
      modal,
      order: order++,
      handlers: new Map(),
      disposed: false,
    };
    scopes.push(record);
    return {
      name,
      modal,
      register(id, handler) {
        if (!known.has(id)) throw new Error(`No key binding "${id}"`);
        const list = record.handlers.get(id) ?? [];
        list.push(handler);
        record.handlers.set(id, list);
        return () => {
          const current = record.handlers.get(id);
          if (!current) return;
          const i = current.lastIndexOf(handler);
          if (i >= 0) current.splice(i, 1);
        };
      },
      dispose() {
        if (record.disposed) return;
        record.disposed = true;
        const i = scopes.indexOf(record);
        if (i >= 0) scopes.splice(i, 1);
        prefix = [];
      },
    };
  }

  /** Active scopes, highest precedence first: newest first, cut at the top modal. */
  function activeScopes(): ScopeRecord[] {
    const sorted = [...scopes].sort((a, b) => {
      if (a.modal !== b.modal) return a.modal ? -1 : 1;
      return b.order - a.order;
    });
    const top = sorted[0];
    return top?.modal ? [top] : sorted;
  }

  function handlersFor(id: string, active: ScopeRecord[]): KeyHandler[] {
    return active.flatMap((scope) =>
      [...(scope.handlers.get(id) ?? [])].reverse(),
    );
  }

  function eligible(
    binding: KeyBinding,
    sequence: KeySequence,
    event: KeyEventLike,
    typing: boolean,
  ): boolean {
    if (
      typing &&
      !(binding.inInput && sequence.length === 1 && !isBareChord(sequence[0]))
    ) {
      return false;
    }
    if (event.repeat && !binding.repeat) return false;
    return true;
  }

  function tryDispatch(
    event: KeyEventLike,
    steps: KeyEventLike[],
  ): "handled" | "partial" | "none" {
    const active = activeScopes();
    const typing = isTextEntry(event.target);
    let partial = false;
    const complete: { binding: KeyBinding; index: number }[] = [];
    for (const { binding, sequences } of compiled) {
      if (handlersFor(binding.id, active).length === 0) continue;
      sequences.forEach((sequence, index) => {
        if (!eligible(binding, sequence, event, typing)) return;
        const result = startsWith(sequence, steps, event, platform);
        if (result === "complete") complete.push({ binding, index });
        else if (result === "partial") partial = true;
      });
    }
    // Precedence is by scope: walk the scopes, newest first, and give
    // the key to the first handler that takes it.
    for (const scope of active) {
      for (const { binding, index } of complete) {
        const handlers = [...(scope.handlers.get(binding.id) ?? [])].reverse();
        for (const handler of handlers) {
          if (handler(event, { index }) !== false) {
            event.preventDefault?.();
            return "handled";
          }
        }
      }
    }
    return partial ? "partial" : "none";
  }

  const keymap: Keymap = {
    root: makeScope("app", false),
    pushScope(name, options) {
      prefix = [];
      return makeScope(name, options?.modal ?? false);
    },
    setPlatform(next) {
      platform = next;
    },
    get platform() {
      return platform;
    },
    get pending() {
      return prefix.length > 0 && now() - prefixAt <= sequenceTimeout;
    },
    handle(event) {
      if (event.defaultPrevented) return false;
      if (isComposing(event)) {
        prefix = [];
        return false;
      }
      if (isModifierKey(event)) return false;
      const live = prefix.length > 0 && now() - prefixAt <= sequenceTimeout;
      if (live) {
        const result = tryDispatch(event, prefix);
        if (result === "handled") {
          prefix = [];
          return true;
        }
        if (result === "partial") {
          prefix = [...prefix, event];
          prefixAt = now();
          return false;
        }
      }
      // No sequence in progress, or this key broke it: start fresh.
      prefix = [];
      const result = tryDispatch(event, []);
      if (result === "partial") {
        prefix = [event];
        prefixAt = now();
      }
      return result === "handled";
    },
  };
  return keymap;
}
