import { createContext, useContext, useEffect, useMemo, useRef, useSyncExternalStore, type ReactNode } from "react";
import type { PlayerState, PlayerStore } from "./PlayerStore";
const Context = createContext<PlayerStore | null>(null);
export function PlayerProvider({ store, children }: { store: PlayerStore; children: ReactNode }) {
  useEffect(() => {
    void store.hydrate();
  }, [store]);
  return <Context.Provider value={store}>{children}</Context.Provider>;
}
export function usePlayerStore() {
  const store = useContext(Context);
  if (!store) throw new Error("PlayerProvider missing");
  return store;
}
export function shallowEqual<T extends object>(left: T, right: T) {
  if (Object.is(left, right)) return true;
  const keys = Object.keys(left) as (keyof T)[];
  return (
    keys.length === Object.keys(right).length &&
    keys.every((key) => Object.hasOwn(right, key) && Object.is(left[key], right[key]))
  );
}
// The selector runs on every notification and again inside each render, so a
// selector that builds an object or array MUST pass an equality function (for
// example `shallowEqual`). With the default Object.is such a selector still
// terminates, but it re-renders on every store patch, which is what this hook
// exists to avoid.
export function usePlayerSelector<T>(
  selector: (state: PlayerState) => T,
  isEqual: (left: T, right: T) => boolean = Object.is,
): T {
  const store = usePlayerStore();
  // Reuse the last committed selection when a render supplies a new selector/equality function.
  const committed = useRef<{ value: T } | null>(null);
  const getSelection = useMemo(() => {
    // Cache keyed by snapshot identity: React calls this getter more than once per
    // render and the subscription path calls it between renders, so the same
    // snapshot must yield the same reference. The closure is shared by those call
    // sites; that is safe only because the key is the snapshot object itself.
    let cached: { snapshot: PlayerState; selection: T } | undefined;
    return () => {
      const snapshot = store.getSnapshot();
      if (cached && Object.is(snapshot, cached.snapshot)) return cached.selection;
      const selection = selector(snapshot);
      if (cached && isEqual(cached.selection, selection)) {
        cached = { snapshot, selection: cached.selection };
      } else if (!cached && committed.current && isEqual(committed.current.value, selection)) {
        cached = { snapshot, selection: committed.current.value };
      } else {
        cached = { snapshot, selection };
      }
      return cached.selection;
    };
  }, [store, selector, isEqual]);
  const value = useSyncExternalStore(store.subscribe, getSelection, getSelection);
  useEffect(() => {
    committed.current = { value };
  }, [value]);
  return value;
}
export function usePlayer() {
  const store = usePlayerStore();
  return { store, state: usePlayerSelector((state) => state) };
}
