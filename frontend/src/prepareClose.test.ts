import { describe, expect, it, vi } from "vitest";
import { createCloseHandler } from "./prepareClose";

describe("normal close", () => {
  it("freezes immediately, waits for hydration and final save, and handles duplicates once", async () => {
    const steps: string[] = [];
    let hydrate!: () => void;
    let flush!: () => void;
    const handler = createCloseHandler(
      {
        beginClose: () => {
          steps.push("beginClose");
        },
        hydrate: () =>
          new Promise<void>((resolve) => {
            hydrate = resolve;
          }),
        pause: () => {
          steps.push("pause");
        },
        flush: () =>
          new Promise<void>((resolve) => {
            steps.push("flush");
            flush = resolve;
          }),
      },
      () => {
        steps.push("freeze");
      },
      async (token) => {
        steps.push(`ack:${token}`);
      },
    );
    const pending = handler("1");
    expect(handler("1")).toBe(pending);
    expect(handler("stale")).toBe(pending);
    expect(steps).toEqual(["freeze", "beginClose"]);
    hydrate();
    await Promise.resolve();
    expect(steps).toEqual(["freeze", "beginClose", "pause", "flush"]);
    flush();
    await pending;
    expect(steps).toEqual(["freeze", "beginClose", "pause", "flush", "ack:1"]);
  });

  it("rejects invalid tokens and acknowledges a failed save attempt without an unhandled rejection", async () => {
    const freeze = vi.fn();
    const acknowledge = vi.fn(async () => {});
    const handler = createCloseHandler(
      {
        hydrate: async () => {},
        pause: vi.fn(),
        flush: async () => {
          throw new Error("save failed");
        },
      },
      freeze,
      acknowledge,
    );
    await handler(null);
    await handler("");
    expect(freeze).not.toHaveBeenCalled();
    await handler("2");
    expect(acknowledge).toHaveBeenCalledExactlyOnceWith("2");
  });
});
