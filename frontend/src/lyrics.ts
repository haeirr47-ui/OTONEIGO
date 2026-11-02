// Timed LRC lines are presentation data only; no extra playback engine or persistence.
export function parseLyrics(text: string): { time: number; text: string }[] {
  const lines: { time: number; text: string }[] = [];
  for (const line of text.split(/\r?\n/)) {
    const tags = [...line.matchAll(/\[(\d+):(\d+(?:\.\d+)?)\]/g)];
    const value = line.replace(/\[[^\]]*\]/g, "").trim();
    for (const tag of tags)
      lines.push({
        time: (Number(tag[1]) * 60 + Number(tag[2])) * 1000,
        text: value,
      });
  }
  return lines.sort((a, b) => a.time - b.time);
}
export function alignLyrics(original: string, translation: string) {
  const translated = parseLyrics(translation);
  return parseLyrics(original).map((line) => {
    let nearest: { time: number; text: string } | undefined;
    for (const candidate of translated) {
      const distance = Math.abs(candidate.time - line.time);
      if (distance <= 750 && (!nearest || distance < Math.abs(nearest.time - line.time))) nearest = candidate;
    }
    return { ...line, translated: nearest?.text };
  });
}
