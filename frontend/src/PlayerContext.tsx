import { createContext, useContext, useEffect, useSyncExternalStore, type ReactNode } from "react";
import type { PlayerStore } from "./PlayerStore";
const Context = createContext<PlayerStore | null>(null);
export function PlayerProvider({ store, children }: { store: PlayerStore; children: ReactNode }) {
  useEffect(() => {
    void store.hydrate();
  }, [store]);
  return <Context.Provider value={store}>{children}</Context.Provider>;
}
export function usePlayer() {
  const store = useContext(Context);
  if (!store) throw new Error("PlayerProvider missing");
  return {
    store,
    state: useSyncExternalStore(store.subscribe, store.getSnapshot, store.getSnapshot),
  };
}
