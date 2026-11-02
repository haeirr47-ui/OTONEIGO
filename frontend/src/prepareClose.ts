interface ClosingStore {
  beginClose?(): void;
  hydrate(): Promise<unknown>;
  pause(): void;
  flush(): Promise<unknown>;
}

// The acknowledgement means the persistence attempt finished, not that every
// save succeeded. PlayerStore retains its existing explicit save-error state.
export function createCloseHandler(
  store: ClosingStore,
  freeze: () => void,
  acknowledge: (token: string) => Promise<unknown>,
) {
  let pending: Promise<void> | undefined;
  return (token: unknown): Promise<void> => {
    if (typeof token !== "string" || !token) return Promise.resolve();
    if (pending) return pending;
    freeze();
    store.beginClose?.();
    pending = (async () => {
      try {
        await store.hydrate();
        store.pause();
        await store.flush();
      } finally {
        await acknowledge(token);
      }
    })();
    // The native deadline still closes if the runtime or a save fails.
    pending = pending.catch(() => {});
    return pending;
  };
}
