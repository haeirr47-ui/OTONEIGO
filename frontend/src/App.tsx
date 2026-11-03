// Presentation shell adapted from OTONEI/src/App.jsx (MIT, copyright (c) 2026 voici5986).
import { useEffect, useRef, useState, type FormEvent } from "react";
import type { DesktopBridge, SearchResult, TrackSnapshot } from "./contracts";
import { normalizeLocalQuery } from "./contracts";
import { usePlayer } from "./PlayerContext";
import { LibraryPanel, SettingsPanel } from "./LibraryPanels";
import { StorageRecoveryNotice } from "./StorageRecoveryNotice";
import { OtoneiNavigation, OtoneiHeader, OtoneiPlayer, TrackCard } from "./OtoneiPresentation";
export { time } from "./format";
// The bridge does not deduplicate search pages; keep the first occurrence so React keys stay unique.
function uniqueTracks(tracks: TrackSnapshot[]) {
  const seen = new Set<string>();
  return tracks.filter((track) => {
    if (seen.has(track.neteaseSongId)) return false;
    seen.add(track.neteaseSongId);
    return true;
  });
}
export function App({ bridge }: { bridge: DesktopBridge }) {
  const { state, store } = usePlayer();
  const q = state.queue;
  const current = q.entries.find((e) => e.queueEntryId === q.currentEntryId)?.track;
  const [query, setQuery] = useState("");
  const [view, setView] = useState<"search" | "favorites" | "history" | "settings">("search");
  useEffect(() => {
    const timer = setInterval(() => void store.pollRegion(), 5000);
    return () => clearInterval(timer);
  }, [store]);
  const [result, setResult] = useState<SearchResult | null>(null);
  const [searching, setSearching] = useState(false);
  const [searchError, setSearchError] = useState("");
  const [page, setPage] = useState(1);
  const [searchedQuery, setSearchedQuery] = useState("");
  const request = useRef<AbortController | null>(null);
  const generation = useRef(0);
  useEffect(
    () => () => {
      generation.current++;
      request.current?.abort();
    },
    [],
  );
  async function search(value: string, nextPage: number, submitted = false) {
    if (store.isClosing()) return;
    try {
      value = normalizeLocalQuery(value);
    } catch {
      setSearchError("搜索关键词不能超过 512 字节。");
      return;
    }
    if (!value) return;
    const historyGeneration = store.getSearchHistoryGeneration();
    request.current?.abort();
    const controller = new AbortController();
    request.current = controller;
    const gen = ++generation.current;
    setSearching(true);
    setSearchError("");
    try {
      const response = await bridge.SearchNetease(value, nextPage, controller.signal);
      if (gen !== generation.current) return;
      if (submitted && nextPage === 1) store.recordSearchQuery(value, historyGeneration);
      setResult(response);
      setPage(nextPage);
      setSearchedQuery(value);
    } catch {
      if (gen === generation.current) setSearchError("搜索失败，请检查网络后重试。");
    } finally {
      if (gen === generation.current) setSearching(false);
    }
  }
  function submit(event: FormEvent) {
    event.preventDefault();
    void search(query, 1, true);
  }
  function add(track: TrackSnapshot, play = false) {
    const id = store.add(track);
    if (play && id) void store.select(id);
  }
  const ready = state.hydration === "ready";
  return (
    <div className="app-container desktop-layout">
      <OtoneiNavigation view={view} onNavigate={setView} />
      <OtoneiHeader
        query={query}
        onQuery={setQuery}
        searching={searching}
        onSubmit={(event) => {
          submit(event);
          setView("search");
        }}
      />
      <main className="main-content">
        <div className="content-area">
          <div className="page-content-wrapper">
            <section aria-label="音乐搜索" hidden={view !== "search"}>
              <h1>{result ? "搜索结果" : "发现音乐"}</h1>
              <p className="muted">
                {result ? `“${searchedQuery}” · 共 ${result.total} 首` : "搜索喜欢的歌曲或歌手，发现属于你的音乐。"}
              </p>
              <section aria-label="最近搜索" className="search-history">
                <div className="section-heading">
                  <h2>最近搜索</h2>
                  <button
                    type="button"
                    disabled={!state.searchHistory.length}
                    onClick={() => store.clearSearchHistory()}
                  >
                    清空搜索记录
                  </button>
                </div>
                {state.searchHistoryError && <p role="alert">{state.searchHistoryError}</p>}
                <ul>
                  {state.searchHistory.map((entry) => (
                    <li key={entry.query}>
                      <button
                        type="button"
                        onClick={() => {
                          setQuery(entry.query);
                          void search(entry.query, 1, true);
                        }}
                      >
                        {entry.query}
                      </button>
                      <button
                        type="button"
                        aria-label={`删除搜索记录：${entry.query}`}
                        onClick={() => store.deleteSearchQuery(entry.query)}
                      >
                        删除
                      </button>
                    </li>
                  ))}
                </ul>
              </section>
              <div className="result-label" aria-live="polite">
                {searching ? "正在搜索…" : ""}
              </div>
              {searchError && (
                <p role="alert" className="error">
                  {searchError}
                </p>
              )}
              <div className="music-list">
                {uniqueTracks(result?.tracks ?? []).map((track) => (
                  <TrackCard
                    key={track.neteaseSongId}
                    track={track}
                    onPlay={() => add(track, true)}
                    onAdd={() => add(track)}
                    disabled={!ready || !state.settings}
                  />
                ))}
              </div>
              {!result?.tracks.length && (
                <div className="alert-light empty-search">
                  <img src="/logo.svg" alt="" width="64" height="64" />
                  <h2>{result ? "没有找到歌曲" : "从一次搜索开始"}</h2>
                  <p>{result ? "试试其他关键词。" : "在上方搜索框输入歌曲或歌手。"}</p>
                </div>
              )}
              {result && (
                <nav className="pagination" aria-label="搜索翻页">
                  <button
                    type="button"
                    disabled={page <= 1 || searching}
                    onClick={() => void search(searchedQuery, page - 1)}
                  >
                    上一页
                  </button>
                  <span>第 {page} 页</span>
                  <button
                    type="button"
                    disabled={!result.hasMore || searching}
                    onClick={() => void search(searchedQuery, page + 1)}
                  >
                    下一页
                  </button>
                </nav>
              )}
            </section>
            {(view === "favorites" || view === "history") && <LibraryPanel key={view} kind={view} />}
            {view === "settings" && <SettingsPanel bridge={bridge} />}
          </div>
        </div>
      </main>
      <div className="notices" aria-live="polite">
        <StorageRecoveryNotice bridge={bridge} />
        {(
          [
            ["error", state.error],
            ["save", state.saveError],
            ["settings", state.settingsError],
            ["library", state.libraryError],
          ] as const
        )
          .filter(([, message]) => Boolean(message))
          .map(([key, message]) => (
            <p key={key} role="alert" className="error">
              {message}
            </p>
          ))}
      </div>
      <OtoneiPlayer current={current} />
    </div>
  );
}
