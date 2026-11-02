// Schedule after completion so slow desktop calls cannot overlap or build a backlog.
export function startDiagnosticPolling<T>(read: () => Promise<T>, accept: (value: T) => void) {
  let active = true;
  let timer: ReturnType<typeof setTimeout> | undefined;
  async function poll() {
    try {
      const value = await read();
      if (active) accept(value);
    } catch {
      // Diagnostic failure must not block otherwise usable local data.
    } finally {
      if (active) timer = setTimeout(() => void poll(), 30_000);
    }
  }
  void poll();
  return () => { active = false; clearTimeout(timer); };
}

type CacheStatsReader = {
  refreshCacheStats(accept?: () => boolean): Promise<void>;
  refreshResourceCacheStats(accept?: () => boolean): Promise<void>;
};
// A new settings mount waits for the previous mount's reads to finish. Their
// validity callbacks prevent those retired reads from mutating the shared store.
const pendingSettingsReads = new WeakMap<CacheStatsReader, Promise<void>>();
export function startSettingsCachePolling(store: CacheStatsReader) {
  let active = true;
  const stop = startDiagnosticPolling(async () => {
    const previous = pendingSettingsReads.get(store);
    if (previous) await previous;
    if (!active) return;
    const pending = Promise.all([
      store.refreshCacheStats(() => active),
      store.refreshResourceCacheStats(() => active),
    ]).then(() => {});
    pendingSettingsReads.set(store, pending);
    try { await pending; }
    finally {
      if (pendingSettingsReads.get(store) === pending) pendingSettingsReads.delete(store);
    }
  }, () => {});
  return () => { active = false; stop(); };
}
