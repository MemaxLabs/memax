import { useEffect, useState } from "react";

/**
 * Returns `value` delayed by `delayMs`. Each change to `value` resets the timer,
 * so rapid updates collapse into a single trailing emission. Use for typed input
 * that drives a network request.
 */
export function useDebouncedValue<T>(value: T, delayMs: number): T {
  const [debounced, setDebounced] = useState(value);
  useEffect(() => {
    const id = setTimeout(() => setDebounced(value), delayMs);
    return () => clearTimeout(id);
  }, [value, delayMs]);
  return debounced;
}
