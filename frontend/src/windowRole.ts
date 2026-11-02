export function windowRole(search: string): "main" | "mini" | "lyrics" | "unknown" {
  const query = new URLSearchParams(search);
  if (!query.has("view")) return "main";
  const views = query.getAll("view");
  return views.length === 1 && (views[0] === "mini" || views[0] === "lyrics") ? views[0] : "unknown";
}
