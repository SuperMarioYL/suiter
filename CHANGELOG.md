# Changelog

All notable changes to **suiter** are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html)
for its shipped Go binary.

## [v0.6.0] — 2026-08-19

Four regression-class fixes folded from the v0.6.0 amendment (a code-verified
bug-hunt; 0 raw/prose patches). No new features; the m3 agent-loop
star-moment and the v0.5.0 read-path error-envelope guards stand unchanged.

### Fixed

- **fix-wework-secret-leaked-in-url-error** — WeCom `gettoken` requires
  `corpid`+`corpsecret` in the query and the message APIs require
  `access_token` in the query; the code built those URLs by raw string
  concatenation, so on any transport-level failure Go's `*url.Error` (whose
  `.Error()` embeds the full request URL) leaked the long-lived corpsecret
  (or the ~2h access_token) to stderr/logs, and unescaped values (`+`, `#`,
  `&`) corrupted requests. Fix: build every wework URL with
  `url.Values{}.Set(...).Encode()` (proper escaping) and unwrap `*url.Error`
  → `ue.Err` in `doRaw`/`Login` before wrapping so the secret-bearing URL
  never reaches the formatted error. Regression tests assert a transport
  error on gettoken/message-send/message-read contains neither the secret nor
  `corpsecret=`/`access_token=`, and that a msgid with `#`/`&`/`+` reaches
  WeCom uncorrupted. (`internal/suite/wework/client.go`)
- **fix-suite-http-client-no-timeout** — all four suite clients plus
  feishu `appAccessToken`/`exchange` issued requests via `http.DefaultClient`
  (9 call sites, `Timeout==0` → no deadline), so a black-holed TCP connection
  or hung endpoint blocked the goroutine indefinitely with no cancellation — a
  stalled Feishu doc read could wedge the whole `suiter agent run
  summarize-and-schedule` star-moment with no way out but `kill -9`. The LLM
  client already did the right thing (`internal/llm/summarize.go` uses a 60s
  timeout). Fix: add a shared `suite.HTTPClient()` (30s timeout) and replace
  the 9 `http.DefaultClient.Do` call sites with it. Regression test
  `TestHTTPClient_NonZeroTimeout`. (`internal/suite/registry.go` + the four
  suite clients)
- **fix-config-store-concurrent-write-race** — `Store.Save`/`Delete` did a
  non-atomic load-modify-flush with no lock: the tmp+rename was atomic for the
  final file, but the load-to-mutate-to-flush window was unguarded, so two
  concurrent `suiter login` processes (or goroutines) both loaded the same
  initial map, each added only its own key, and the second rename silently
  erased the first suite's just-cached token (last rename wins). Fix:
  serialize the critical section with an in-process `sync.Mutex` AND an
  exclusive flock on a sibling `.lock` file (cross-process), held across
  load-modify-flush + the existing tmp+atomic-rename. Regression test
  `TestSave_ConcurrentNoLoss` runs N concurrent Saves and asserts none are
  lost (`go test -race`). (`internal/config/store.go`)
- **fix-dingtalk-tencentdocs-write-fake-success** — the v0.5.0 read-path fix
  guarded feishu/wework reads against HTTP-200 error envelopes, but the WRITE
  paths were not touched. `dingtalk.calendarCreateEvent` returned the
  hard-coded literal `created` when the 200-body lacked an `id`, and
  `tencentdocs.sheetWrite` returned `written`; both masked HTTP-200 error
  envelopes (a real DingTalk/Tencent-Docs failure mode for
  permission/validation errors) and wrong-field successes, so the agent loop
  printed `scheduled: created` and exited 0, silently losing the m3
  star-moment event. Fix: mirror the read-path guard — surface the API error
  envelope (DingTalk `code`/`message` + legacy `errcode`/`errmsg`; Tencent
  `code`/`errcode`/`ret` + `errmsg`/`message`) as an error; only return an id
  when the body actually carries one; otherwise surface the raw body so a
  wrong-field success is observable rather than masked. Regression tests
  assert a 200 error envelope → write error (not a fake id) and a real id →
  returned id. (`internal/suite/dingtalk/client.go`,
  `internal/suite/tencentdocs/client.go`)

## [v0.5.0] — 2026-08-16

Two features and one defect-class fix folded from the v0.5.0 amendment. The m3
agent-loop star-moment now works with only an API key set, the Read paths stop
returning HTTP-200 error envelopes as bodies, and the login→use→logout token
lifecycle is complete.

### Added

- **feat-llm-default-model-by-provider** — `newLLMClientFromConfig` now defaults
  the model from the resolved base URL when `SUITER_LLM_MODEL` is unset
  (GLM→`glm-4.6`, DeepSeek→`deepseek-chat`), so `suiter agent run
  summarize-and-schedule` works with only `SUITER_LLM_API_KEY` set (the minimal
  setup the README advertises; previously `model:""` was sent and the m3
  star-moment failed opaquely at the LLM layer). An explicit `SUITER_LLM_MODEL`
  always wins; defaulting by provider (not a single global default) avoids
  sending `glm-4.6` to DeepSeek. Regression test
  `TestNewLLMClientFromConfig_DefaultModelByProvider`.
- **feat-logout-command** — new `suiter logout <suite>` clears a suite's cached
  token from `~/.suiter/tokens.json`, wiring the previously-dead
  `TokenStore.Delete` (zero non-test callers) and completing the
  login→use→logout lifecycle the v0.2.0–v0.4.0 cached-token-expiry fixes assume
  (they tell the user to re-login but offered no CLI way to clear a
  stale/empty/leaked token). Mirrors `suiter login`: same `reg.Get(name)`
  "unknown suite" guard, same `store==nil`-at-use guard; idempotent on a missing
  entry (exit 0). Regression test `TestLogout`.

### Fixed

- **fix-read-silent-suite-error-envelope** — two Read paths returned the raw
  HTTP-200 body without inspecting the suite's HTTP-200-nested error code, so an
  API-level error was returned as if it were the resource body (a silent
  failure on the m1/m3 star-moment path — the agent loop would feed the error
  JSON to the GLM summarizer).
  - `feishu.readDocRawContent` now parses Feishu's `{"code":...,"msg":...}`
    envelope and errors on `code != 0` (mirrors `exchange()`'s `tr.Code != 0`
    guard in the same file). The raw envelope is still returned on success so
    the `--json` agent-readable contract is unchanged.
  - `wework.messageRead` now parses WeCom's `{"errcode":...,"errmsg":...}`
    envelope and errors on `errcode != 0` (mirrors `messageSend()`'s
    `r.ErrCode != 0` guard in the same file). The raw envelope is still returned
    on success, matching the existing `TestRead_MessageRead` success contract.
  - dingtalk (HTTP-status-coded REST API) and tencentdocs (standard OAuth2) are
    unaffected; the fix is scoped to the two suites with a concrete code-at-200
    gap. Regression tests `TestRead_DocRawContent_ErrorEnvelope` and
    `TestRead_MessageRead_ErrorEnvelope`.

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
