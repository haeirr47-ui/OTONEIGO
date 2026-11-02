import type { ResolvedStream } from "./contracts";

export const time = (ms: number) => `${Math.floor(ms / 60000)}:${String(Math.floor(ms / 1000) % 60).padStart(2, "0")}`;

const qualityLabels: Record<string, string> = {
  standard: "标准",
  higher: "较高",
  exhigh: "极高",
  lossless: "无损",
  hires: "Hi-Res",
  jyeffect: "高清环绕声",
  sky: "沉浸环绕声",
  jymaster: "超清母带",
};

export function actualStreamQuality(stream: ResolvedStream): string {
  const bitrate = stream.actualBitrate;
  if (typeof bitrate === "number" && Number.isFinite(bitrate) && bitrate > 0)
    return `${Math.round(bitrate / 1000)} kbps`;
  const quality = stream.actualQuality ?? "";
  return Object.hasOwn(qualityLabels, quality) ? qualityLabels[quality] : "实际音质未知";
}
