// Presentation shell adapted from OTONEI/src/App.jsx (MIT, copyright (c) 2026 voici5986).
import { useCallback, useEffect, useRef, useState, type FormEvent } from "react";
import type { DesktopBridge, EntitySearchResult, SearchResult, TrackSnapshot } from "./contracts";
import { normalizeLocalQuery } from "./contracts";
import { shallowEqual, usePlayerSelector, usePlayerStore } from "./PlayerContext";
import { LibraryPanel, SettingsPanel } from "./LibraryPanels";
import { DiscoveryPanel } from "./DiscoveryPanel";
import { EntityPanel } from "./EntityPanel";
import { EntityViewContext, type EntityViewRequest } from "./entityView";
import { StorageRecoveryNotice } from "./StorageRecoveryNotice";
import { OtoneiNavigation, OtoneiHeader, OtoneiPlayer, TrackCard, type View } from "./OtoneiPresentation";
const searchKinds = [
  { id: "song", label: "歌曲" },
  { id: "album", label: "专辑" },
  { id: "artist", label: "歌手" },
  { id: "playlist", label: "歌单" },
] as const;
// Switching kinds must not keep showing results for a keyword the input no longer
// contains; a blank input falls back to the last submitted query.
export function searchTermFor(query: string, searchedQuery: string) {
  return query.trim() ? query : searchedQuery;
}
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
  const store = usePlayerStore();
  const state = usePlayerSelector(
    (state) => ({
      hydration: state.hydration,
      settingsReady: !!state.settings,
      searchHistory: state.searchHistory,
      searchHistoryError: state.searchHistoryError,
      error: state.error,
      saveError: state.saveError,
      settingsError: state.settingsError,
      libraryError: state.libraryError,
    }),
    shallowEqual,
  );
  const [query, setQuery] = useState("");
  const [view, setView] = useState<View>("search");
  const [searchKind, setSearchKind] = useState<(typeof searchKinds)[number]["id"]>("song");
  const [entityResult, setEntityResult] = useState<EntitySearchResult | null>(null);
  const [detail, setDetail] = useState<EntityViewRequest | null>(null);
  const [detailReturnView, setDetailReturnView] = useState<View>("search");
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
  async function search(
    value: string,
    nextPage: number,
    submitted = false,
    kind: (typeof searchKinds)[number]["id"] = searchKind,
  ) {
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
      if (kind === "song") {
        const response = await bridge.SearchNetease(value, nextPage, controller.signal);
        if (gen !== generation.current) return;
        setResult(response);
        setEntityResult(null);
      } else {
        if (!bridge.SearchEntities) throw new Error("UNSUPPORTED");
        const response = await bridge.SearchEntities(value, kind, nextPage, controller.signal);
        if (gen !== generation.current) return;
        setEntityResult(response);
        setResult(null);
      }
      if (submitted && nextPage === 1) store.recordSearchQuery(value, historyGeneration);
      setPage(nextPage);
      setSearchedQuery(value);
    } catch {
      if (gen === generation.current) setSearchError("搜索失败，请检查网络后重试。");
    } finally {
      if (gen === generation.current) setSearching(false);
    }
  }
  function chooseKind(kind: (typeof searchKinds)[number]["id"]) {
    generation.current++;
    request.current?.abort();
    setSearchKind(kind);
    setResult(null);
    setEntityResult(null);
    setPage(1);
    setSearchError("");
    // The aborted request fails the generation check, so it can no longer clear
    // the flag; clear it here before deciding whether a new search starts.
    setSearching(false);
    const next = searchTermFor(query, searchedQuery);
    if (next) void search(next, 1, false, kind);
    else setSearchedQuery("");
  }
  const openEntity = useCallback(
    (next: EntityViewRequest) => {
      if (store.isClosing()) return;
      setDetailReturnView(view);
      setDetail(next);
    },
    [store, view],
  );
  function submit(event: FormEvent) {
    event.preventDefault();
    // A submitted search replaces the detail page; otherwise the new results stay
    // hidden behind EntityPanel until the user navigates back.
    setDetail(null);
    void search(query, 1, true);
  }
  const add = useCallback(
    (track: TrackSnapshot) => {
      store.add(track);
    },
    [store],
  );
  const play = useCallback(
    (track: TrackSnapshot) => {
      const id = store.add(track);
      if (id) void store.select(id);
    },
    [store],
  );
  const ready = state.hydration === "ready";
  return (
    <EntityViewContext.Provider value={openEntity}>
      <div className="app-container desktop-layout">
        <OtoneiNavigation
          view={view}
          onNavigate={(next) => {
            setDetail(null);
            setView(next);
          }}
        />
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
              {detail ? (
                <EntityPanel
                  key={`${detail.kind}:${detail.id}`}
                  bridge={bridge}
                  request={detail}
                  onBack={() => {
                    setDetail(null);
                    setView(detailReturnView);
                  }}
                />
              ) : (
                <>
                  <section aria-label="音乐搜索" hidden={view !== "search"}>
                    <h1>{result || entityResult ? "搜索结果" : "发现音乐"}</h1>
                    <p className="muted">
                      {result
                        ? `“${searchedQuery}” · 共 ${result.total} 首`
                        : entityResult
                          ? `“${searchedQuery}” · 共 ${entityResult.total} 个结果`
                          : "搜索喜欢的歌曲或歌手，发现属于你的音乐。"}
                    </p>
                    <div className="search-kind-tabs">
                      {searchKinds.map((entry) => (
                        <button
                          key={entry.id}
                          type="button"
                          aria-pressed={searchKind === entry.id}
                          onClick={() => chooseKind(entry.id)}
                        >
                          {entry.label}
                        </button>
                      ))}
                    </div>
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
                    {searchKind === "song" ? (
                      <>
                        <div className="music-list">
                          {uniqueTracks(result?.tracks ?? []).map((track) => (
                            <TrackCard
                              key={track.neteaseSongId}
                              track={track}
                              onPlay={play}
                              onAdd={add}
                              disabled={!ready || !state.settingsReady}
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
                      </>
                    ) : (
                      <>
                        <div className="music-list entity-list">
                          {(entityResult?.items ?? []).map((item) => (
                            <button
                              type="button"
                              key={item.id}
                              className="music-card entity-item"
                              onClick={() =>
                                openEntity({
                                  kind: entityResult?.kind ?? "album",
                                  id: item.id,
                                  name: item.name,
                                })
                              }
                            >
                              <strong>{item.name}</strong>
                              {item.subtitle && <small className="muted">{item.subtitle}</small>}
                              {!!item.trackCount && (
                                <small className="muted">
                                  {item.trackCount} {searchKind === "artist" ? "张专辑" : "首"}
                                </small>
                              )}
                            </button>
                          ))}
                        </div>
                        {!entityResult?.items.length && (
                          <div className="alert-light empty-search">
                            <img src="/logo.svg" alt="" width="64" height="64" />
                            <h2>{entityResult ? "没有找到相关内容" : "从一次搜索开始"}</h2>
                            <p>{entityResult ? "试试其他关键词或切换类型。" : "在上方搜索框输入歌曲或歌手。"}</p>
                          </div>
                        )}
                        {entityResult?.partial && <p className="muted">部分结果暂不可用。</p>}
                      </>
                    )}
                    {(result || entityResult) && (
                      <nav className="pagination" aria-label="搜索翻页">
                        <button
                          type="button"
                          disabled={page <= 1 || searching}
                          onClick={() => void search(searchedQuery, page - 1, false, searchKind)}
                        >
                          上一页
                        </button>
                        <span>第 {page} 页</span>
                        <button
                          type="button"
                          disabled={!(result?.hasMore || entityResult?.hasMore) || searching}
                          onClick={() => void search(searchedQuery, page + 1, false, searchKind)}
                        >
                          下一页
                        </button>
                      </nav>
                    )}
                  </section>
                  {view === "rankings" && <DiscoveryPanel bridge={bridge} />}
                  {(view === "favorites" || view === "history") && <LibraryPanel key={view} kind={view} />}
                  {view === "settings" && <SettingsPanel bridge={bridge} />}
                </>
              )}
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
        <OtoneiPlayer />
      </div>
    </EntityViewContext.Provider>
  );
}
