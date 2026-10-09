import type { Ref, RefCallback } from "react";

/** Points several refs (objects or callbacks, React 19 cleanups included) at one element. */
export function mergeRefs<T>(
  ...refs: Array<Ref<T> | undefined>
): RefCallback<T> {
  return (value) => {
    const cleanups = refs.map((ref) => {
      if (typeof ref === "function") {
        const cleanup = ref(value);
        return typeof cleanup === "function" ? cleanup : () => ref(null);
      }
      if (ref) {
        ref.current = value;
        return () => {
          ref.current = null;
        };
      }
      return () => {};
    });
    return () => {
      for (const cleanup of cleanups) cleanup();
    };
  };
}
