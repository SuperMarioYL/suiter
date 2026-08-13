# Changelog

All notable changes to **suiter** are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html)
for its shipped Go binary.

## [v0.4.0] — 2026-08-13

Two regression-class fixes folded from the v0.4.0 plan. No new features; the
m3 agent-loop star-moment and cached-token-expiry guards shipped in v0.3.0
stand unchanged.

### Fixed

- **fix-config-credentials-ignored-before-read** — `buildRegistry()` ran inside
  `NewRoot()` at construction time, baking `viper.GetString(...)` credential
  strings into the four suite clients *before* `viper.ReadInConfig()` ran in
  `PersistentPreRunE` (which only executes during `root.Execute()`). Viper does
  not auto-read the config file on `GetString`, so any credential set only in
  `~/.suiter/config.yaml` (or via `--config`) was absent when clients were
  built, and every `suiter login <suite>` errored with "credentials not set"
  even though the README advertises the config file. Env vars still worked
  (`AutomaticEnv()` is live from `NewRoot()`), which is why the fake-wired
  `dispatcher_test.go` / `agent_test.go` missed it. Fix: read the config file
  (best-effort, resolving the `--config` override from `os.Args` since cobra has
  not parsed flags at construction time) *before* `buildRegistry()`; env-var
  override behavior is preserved. Regression test
  `TestBuildRegistry_ConfigFileCredentials` wires a credential through a temp
  config file and asserts it reaches the dingtalk client (via `AuthURL`'s
  `client_id`), and that an env var still overrides the file.
  (`internal/cli/root.go`)

- **fix-calendar-title-utf8-truncation** — `calendarEventFromSummary()`
  truncated the event title with `title = title[:80]` (a byte slice). For a
  Chinese summary (~3 bytes/rune, where >80 bytes is reached at ~27 Chinese
  characters — essentially every real GLM summary of a 飞书 doc for this
  zh-primary product) the slice landed mid-rune → invalid UTF-8, so
  `json.Marshal` substituted `U+FFFD` and the 钉钉 calendar event title was
  stored with mojibake at the truncation boundary. The same byte-slice defect in
  `truncate()` corrupted the `summary:` line printed to stdout. Fix: truncate
  by rune count (`[]rune(title)[:80]`) so the result is always valid UTF-8, in
  both `calendarEventFromSummary()` and `truncate()`. Regression test
  `TestCalendarEventFromSummary_UTF8Truncation` (table-driven, including long
  Chinese and mixed summaries) asserts the truncated title is valid UTF-8 with
  no `U+FFFD` and ≤80 runes; `TestTruncate_RuneSafe` covers the stdout path.
  (`internal/cli/agent.go`)

## [v0.3.0]

### Added

- m3 agent-loop star-moment (real, no stubs): 腾讯文档 sheet read + the
  OpenAI-compatible GLM/DeepSeek summarizer, wiring the end-to-end loop
  read 飞书 doc → summarize → write 钉钉 calendar event behind the shared
  `Suite` interface + `TokenStore`.

### Fixed

- `fix-dingtalk-cached-token-expiry` — `dingtalk.Client.cachedToken` now checks
  `ExpiresIn`/`ObtainedAt` and returns a clear "token expired (re-run
  `suiter login dingtalk`)" instead of sending a stale token.
- `fix-wework-cached-token-expiry` — same staleness guard in
  `wework.Client.cachedToken`, mirroring the v0.2.0 feishu fix.

## [v0.2.0]

### Added

- m2 suite unification: 钉钉 (calendar list/create) + 企微 (message send/read)
  behind the same `suiter <suite> <verb>` grammar and shared `TokenStore` —
  zero per-suite CLI glue.

### Fixed

- `fix-feishu-oidc-token-parse` — parse the Feishu OIDC `access_token` fields
  nested under `data` (and send the request as a JSON body), matching the
  larksuite SDK contract.
- `fix-feishu-oauth-loopback-hang` — non-blocking channel sends, restrict
  handling to `/callback`, and a 5s `srv.Shutdown` timeout so a stray request
  can no longer abort or deadlock login.
- `fix-feishu-cached-token-expiry` — `feishu.Client.cachedToken` now checks
  `ExpiresIn`/`ObtainedAt` before returning a cached token.

## [v0.1.0]

### Added

- m1: Go binary + cobra root + `suiter login feishu` (OAuth loopback via the
  larksuite SDK) + `suiter feishu doc read <id> --json`; token cached in
  `~/.suiter/tokens.json`.
