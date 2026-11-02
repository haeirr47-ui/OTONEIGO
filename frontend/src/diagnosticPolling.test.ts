import { afterEach, expect, it, vi } from "vitest";
import { startDiagnosticPolling, startSettingsCachePolling } from "./diagnosticPolling";

afterEach(() => vi.useRealTimers());

it("checks immediately, continues after healthy data, and discovers later recovery at 30 seconds", async () => {
  vi.useFakeTimers();
  const read = vi.fn().mockResolvedValueOnce(0).mockResolvedValue(3);
  const accept = vi.fn();
  const stop = startDiagnosticPolling(read, accept);
  await vi.advanceTimersByTimeAsync(0);
  expect(accept).toHaveBeenLastCalledWith(0);
  await vi.advanceTimersByTimeAsync(29_999);
  expect(read).toHaveBeenCalledTimes(1);
  await vi.advanceTimersByTimeAsync(1);
  expect(accept).toHaveBeenLastCalledWith(3);
  stop();
  await vi.advanceTimersByTimeAsync(60_000);
  expect(read).toHaveBeenCalledTimes(2);
});

it("does not overlap slow reads or publish or schedule after cleanup", async () => {
  vi.useFakeTimers();
  let finish!: (value: number) => void;
  const read = vi.fn(
    () =>
      new Promise<number>((resolve) => {
        finish = resolve;
      }),
  );
  const accept = vi.fn();
  const stop = startDiagnosticPolling(read, accept);
  await vi.advanceTimersByTimeAsync(120_000);
  expect(read).toHaveBeenCalledTimes(1);
  stop();
  finish(4);
  await vi.advanceTimersByTimeAsync(60_000);
  expect(accept).not.toHaveBeenCalled();
  expect(read).toHaveBeenCalledTimes(1);
  expect(vi.getTimerCount()).toBe(0);
});

it("retries diagnostic failures at the bounded cadence", async () => {
  vi.useFakeTimers();
  const read = vi.fn().mockRejectedValueOnce(new Error("unavailable")).mockResolvedValue(1);
  const accept = vi.fn();
  const stop = startDiagnosticPolling(read, accept);
  await vi.advanceTimersByTimeAsync(30_000);
  expect(read).toHaveBeenCalledTimes(2);
  expect(accept).toHaveBeenCalledWith(1);
  stop();
});

it("refreshes both cache statistics on settings entry and stops cadence on exit", async () => {
  vi.useFakeTimers();
  const store = { refreshCacheStats: vi.fn(async () => {}), refreshResourceCacheStats: vi.fn(async () => {}) };
  const leave = startSettingsCachePolling(store);
  await vi.advanceTimersByTimeAsync(0);
  expect(store.refreshCacheStats).toHaveBeenCalledTimes(1);
  expect(store.refreshResourceCacheStats).toHaveBeenCalledTimes(1);
  await vi.advanceTimersByTimeAsync(30_000);
  expect(store.refreshCacheStats).toHaveBeenCalledTimes(2);
  leave();
  await vi.advanceTimersByTimeAsync(60_000);
  expect(store.refreshCacheStats).toHaveBeenCalledTimes(2);
  expect(store.refreshResourceCacheStats).toHaveBeenCalledTimes(2);
  const leaveAgain = startSettingsCachePolling(store);
  expect(store.refreshCacheStats).toHaveBeenCalledTimes(3);
  leaveAgain();
});
