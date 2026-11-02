<!-- BEGIN codex-astra-luna-orchestrator managed block -->
# Codex project instructions

For complex coding tasks, use the `astra-orchestrator` skill when its trigger conditions match.
The root agent owns architecture, decomposition, integration, and final verification.
Prefer specialized subagents for bounded exploration, implementation, testing, review, and technical research.
Do not delegate trivial work merely for parallelism.
Do not let multiple implementation agents edit the same files without explicit ownership boundaries.
User instructions always take precedence over this orchestration policy.
<!-- END codex-astra-luna-orchestrator managed block -->

## Project boundaries

- This is the independent Windows OTONEIGO project. `OTONEI/`, `go-musicfox-master/` and `NetEaseMusicWorldNext-master/` are ignored, read-only reference copies; never modify or stage them.
- Current delivery is the portable ZIP only. Do not build or run installers or installer smoke tests unless the user explicitly reopens that scope. The packaging command must default to ZIP-only output.
- Follow the root implementation plan. P0 core verification must pass before claiming P1/P2 delivery. Record live, mocked, optional, failed and untested checks accurately.
- Use one Go module, Wails 3, React, SQLite, one frontend queue owner and one audio executor. No separate local HTTP API process.
- Derive the interface from OTONEI's actual React component markup, layout, theme and interaction patterns; adapt and extend that UI for Windows and Netease. A separately designed technical validation screen is not the product UI. Keep legacy Firebase, catalog and playback state out of the adapted presentation.
- Netease IDs stay strings. No Firebase, UNM or other music catalogs in application code or dependencies. Do not copy GPL go-musicfox application code.
- Core source priority: reuse eligible verified local cache first. In automatic mode, choose the Netease playback strategy BEFORE sending requests: known CN uses ordinary requests only; known overseas uses the bounded `X-Real-IP` compatibility directly for every quality attempt, without any ordinary preflight. Unknown/detection failure keeps the explicit conservative ordinary-then-compatible fallback. `always` forces compatibility directly; `off` forces ordinary. Only after the selected Netease path fails may enabled/rate-limited GD `source=netease` run; GD never receives the compatibility header. Accept complete playable Netease audio immediately, never call GD just to chase bitrate or replace a working stream. Search, metadata, lyrics and covers come from Netease, not GD. Preserve GD coalescing, short URL TTL, request spacing and the shared finite deadline. Compatibility cooldown must not silently route known overseas traffic through ordinary requests.
- Do not implement import/export/backup/restore or old-user-data migration. Account capability remains disabled. GD release capability is enabled after independent verification of a bounded same-ID MP3 sample; its user setting remains off by default. Do not generalize that evidence to all tracks, recording versions, rights or quality levels.
- Never persist media URLs, cookies or tokens in queue/library snapshots, logs or Git. Preserve attribution for reused MIT code.
- Overseas detection and bounded player-endpoint header compatibility are implemented P0 experiments, not universal unblocking; CDN rewriting remains unimplemented. Keep login/QR/auth requests outside compatibility rewriting, preserve HTTPS validation and never copy GPL extension code into the MIT client. Do not change system proxy, DNS or Hosts for testing.
- Update architecture, storage, license and P0 verification documentation with changes to their owning behavior. Run appropriate Go/frontend tests and a Windows build; do not represent browser mocks as native WebView2 acceptance.
