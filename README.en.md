<div align="right"><sub><b>English</b>&nbsp;&nbsp;⇄&nbsp;&nbsp;<a href="./README.md">中文</a></sub></div>

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="./assets/hero-dark.svg">
  <source media="(prefers-color-scheme: light)" srcset="./assets/hero-light.svg">
  <img src="./assets/hero-light.svg" width="880" alt="suiter — Coding-Agent CLI for 飞书/钉钉/企微/腾讯文档">
</picture>

<p align="center"><sub>googleworkspace/cli — but for 飞书/钉钉/企微/腾讯文档. A Go Coding-Agent CLI that collapses 4 OAuth flows into one <code>suiter login</code>, so your agent reads/writes your own CN office-suite data with zero per-app glue.</sub></p>

<p align="center">
  <a href="./LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue.svg" alt="MIT"></a>
  <a href="https://github.com/SuperMarioYL/suiter/releases/latest"><img src="https://img.shields.io/github/v/release/SuperMarioYL/suiter?label=release&color=blue" alt="latest release"></a>
  <img src="https://img.shields.io/github/actions/workflow/status/SuperMarioYL/suiter/ci.yml?label=CI&branch=main" alt="CI">
  <img src="https://img.shields.io/badge/Go-1.24-0071E3?logo=go&logoColor=white" alt="Go 1.24">
  <img src="https://img.shields.io/badge/Coding%20Agents-ready-5E5CE6" alt="Coding Agents">
  <img src="https://img.shields.io/badge/Coding%20Agent-CLI-10A37F" alt="Coding Agent">
</p>

> **One `suiter login` replaces 4 hand-built OAuth integrations — your Coding Agent reads/writes your own 飞书 docs, 钉钉 calendar, 企微 messages, 腾讯文档 with zero per-app glue.**

## Table of contents

- [What](#what)
- [Why now](#why-now)
- [Architecture](#architecture)
- [Install](#install)
- [Quickstart](#quickstart)
- [Usage](#usage)
- [Suites](#suites)
- [Agent](#agent)
- [Demo](#demo)
- [Config](#config)
- [Roadmap](#roadmap)
- [License](#license)

<h2><img src="https://api.iconify.design/tabler:package.svg?color=%230071E3&width=24" height="22" align="absmiddle" alt=""> What</h2>

suiter is a single Go binary your Coding Agent calls to read and write your own CN office-suite data. Every subcommand speaks `--json` stdio, so an agent (Claude Code / Trae / Cline) can pipe it straight into context. One `Suite` interface, one grammar: `suiter <suite> <verb> <id>`.

<h2><img src="https://api.iconify.design/tabler:bulb.svg?color=%230071E3&width=24" height="22" align="absmiddle" alt=""> Why now</h2>

suiter is the Coding-Agent CLI that [@nlpnerd](https://github.com/nlpnerd) has been waiting for — the closest analog to [ChromeDevTools/chrome-devtools-mcp](https://github.com/ChromeDevTools/chrome-devtools-mcp), but for 飞书/钉钉/企微/腾讯文档 instead of the browser. The "under-the-hood engineering work across tools org-wide" that the [affaan-m/ECC](https://github.com/affaan-m/ECC) agent catalog keeps flagging? suiter pays that tax once per suite and caches it to `~/.suiter/tokens.json`, so your Coding Agents read your CN office-suite data with zero per-app glue — the way googleworkspace/cli already does for Google Workspace, but for the four suites a CN dev actually lives in.

```
Before — 4 hand-built OAuth integrations     After — 1 suiter login
┌──────────────────────────────────┐     ┌───────────────────────────────┐
│ feishu SDK   → OAuth #1 ┐        │     │ $ suiter login feishu         │
│ dingtalk SDK → OAuth #2 │ 4× glue│     │ $ suiter login dingtalk       │
│ wework SDK   → OAuth #3 │        │     │ $ suiter login wework         │
│ tencent SDK  → OAuth #4 ┘        │     │ $ suiter login tencentdocs    │
│ (each: paging, refresh, events) │     │ → ~/.suiter/tokens.json (1)   │
└──────────────────────────────────┘     └───────────────────────────────┘
```

<h2><img src="https://api.iconify.design/tabler:topology-star-3.svg?color=%230071E3&width=24" height="22" align="absmiddle" alt=""> Architecture</h2>

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="./assets/atlas-dark.svg">
  <source media="(prefers-color-scheme: light)" srcset="./assets/atlas-light.svg">
  <img src="./assets/atlas-light.svg" width="880" alt="Architecture: Coding Agent → suiter CLI (Suite/Registry/TokenStore) → 飞书/钉钉/企微/腾讯文档">
</picture>

A single binary, in-process — no daemon, no server, no Kubernetes: Cobra CLI → Suite registry → 4 suite clients, backed by the shared `TokenStore` (`~/.suiter/tokens.json`) and a thin OpenAI-compatible LLM client (GLM / DeepSeek; no self-hosted models).

<h2><img src="https://api.iconify.design/tabler:download.svg?color=%230071E3&width=24" height="22" align="absmiddle" alt=""> Install</h2>

```bash
go install github.com/SuperMarioYL/suiter@latest
```

Requires Go 1.24+. The binary lands on `$GOPATH/bin`.

<h2><img src="https://api.iconify.design/tabler:rocket.svg?color=%230071E3&width=24" height="22" align="absmiddle" alt=""> Quickstart</h2>

```bash
export SUITER_FEISHU_APP_ID=cli_xxx SUITER_FEISHU_APP_SECRET=xxx   # from open.feishu.cn
suiter login feishu                                                  # one OAuth loopback, token cached
suiter feishu doc read <doc-id> --json                               # doc body → agent-readable JSON
```

<details><summary>sample output</summary>

```json
{
  "code": 0,
  "msg": "success",
  "data": { "content": "Weekly sync: 1) ship 7/30 ... 2) owner @zhang ..." }
}
```

</details>

<h2><img src="https://api.iconify.design/tabler:terminal-2.svg?color=%230071E3&width=24" height="22" align="absmiddle" alt=""> Usage</h2>

```bash
# 1) one OAuth loopback, token cached to ~/.suiter/tokens.json
suiter login feishu

# 2) Feishu doc body → JSON to stdout
suiter feishu doc read <doc-id> --json

# 3) pipe into your Coding Agent
suiter feishu doc read <doc-id> --json | your-agent summarize

# 4) list registered suites
suiter suites

# 5) m3 end-to-end star moment: read 飞书 → GLM summarize → 钉钉 calendar event
suiter agent run summarize-and-schedule <doc-id>
```

More in [`examples/`](./examples).

<h2><img src="https://api.iconify.design/tabler:layout-grid.svg?color=%230071E3&width=24" height="22" align="absmiddle" alt=""> Suites</h2>

| Suite | Status | Verbs |
|---|---|---|
| 飞书 feishu | m1 ✓ | `login`, `doc read` |
| 钉钉 dingtalk | m2 ✓ | `login`, `calendar list/get/create` |
| 企微 wework | m2 ✓ | `login`, `message send/read` |
| 腾讯文档 tencentdocs | m3 ✓ | `login`, `sheet read/write` |

Every suite implements the same `Suite` interface (`Name` / `Login` / `Read` / `Write`) behind one `Suite` registry — adding a suite verb takes zero new CLI glue, which is the whole thesis the unification has to prove.

<h2><img src="https://api.iconify.design/tabler:robot.svg?color=%230071E3&width=24" height="22" align="absmiddle" alt=""> Agent</h2>

The star moment (m3) is a 60-second loop: `suiter feishu doc read <id> --json` → GLM/DeepSeek `summarize` → `suiter dingtalk calendar create`. v0.2 already exposes `--json` stdio on every verb and wires 钉钉/企微 for real under the same `suiter <suite> <verb>` grammar, so an agent orchestrates across three suites today; v0.3 wires `suiter agent run summarize-and-schedule` end-to-end — read a 飞书 doc → GLM/DeepSeek summarize → write a 钉钉 calendar event, one command. A full MCP server stays on the roadmap — the unified abstraction is now real across all 4 suites, then seals as MCP.

<h2><img src="https://api.iconify.design/tabler:photo.svg?color=%230071E3&width=24" height="22" align="absmiddle" alt=""> Demo</h2>

**suiter: 4 OAuth → 1 CLI in 60s (m3 agent loop — read 飞书 → GLM summarize → 钉钉 calendar)**

![demo](assets/demo.gif)

> Rendered by CI from [`docs/demo.tape`](./docs/demo.tape) (vhs). Refresh manually via the `demo` workflow. The gif above runs the no-credentials unified-grammar tour; the full 60s screencast script:

```text
suiter login feishu       # one OAuth loopback, token cached in ~/.suiter/tokens.json
suiter login dingtalk     # same flow, same shared TokenStore (m2 unified)
suiter feishu doc read <id> --json          # agent-readable doc body
suiter agent run summarize-and-schedule <feishu-doc-id>   # read 飞书 → GLM summarize → 钉钉 calendar event (m3 star moment)
suiter dingtalk calendar list --json        # unified verb, same grammar, no per-suite CLI glue
```

<h2><img src="https://api.iconify.design/tabler:adjustments.svg?color=%230071E3&width=24" height="22" align="absmiddle" alt=""> Config</h2>

Priority: `--config <path>` flag > env vars > `~/.suiter/config.yaml`.

| Key | Env var | Default | Meaning |
|---|---|---|---|
| `feishu_app_id` | `SUITER_FEISHU_APP_ID` | `""` | Feishu app_id |
| `feishu_app_secret` | `SUITER_FEISHU_APP_SECRET` | `""` | Feishu app_secret |
| `dingtalk_app_key` | `SUITER_DINGTALK_APP_KEY` | `""` | 钉钉 appKey (live in v0.2) |
| `dingtalk_app_secret` | `SUITER_DINGTALK_APP_SECRET` | `""` | 钉钉 appSecret (live in v0.2) |
| `wework_corp_id` | `SUITER_WEWORK_CORP_ID` | `""` | 企微 corpId (live in v0.2) |
| `wework_agent_id` | `SUITER_WEWORK_AGENT_ID` | `""` | 企微 agentId (live in v0.2) |
| `wework_secret` | `SUITER_WEWORK_SECRET` | `""` | 企微 secret (live in v0.2) |
| `tencentdocs_client_id` | `SUITER_TENCENTDOCS_CLIENT_ID` | `""` | 腾讯文档 clientId (live in v0.3) |
| `tencentdocs_client_secret` | `SUITER_TENCENTDOCS_CLIENT_SECRET` | `""` | 腾讯文档 clientSecret (live in v0.3) |
| `llm_base_url` | `SUITER_LLM_BASE_URL` | GLM `open.bigmodel.cn` | OpenAI-compatible model root (GLM/DeepSeek) |
| `llm_api_key` | `SUITER_LLM_API_KEY` | `""` | GLM/DeepSeek API key (required for the `agent run` summarize step) |
| `llm_model` | `SUITER_LLM_MODEL` | `""` | model name (e.g. `glm-4.6` / `deepseek-chat`) |

Tokens are cached at `~/.suiter/tokens.json` (0600 perms, single dev, local machine; multi-user / cloud vault explicitly out of scope for v0.1).

<h2><img src="https://api.iconify.design/tabler:map-2.svg?color=%230071E3&width=24" height="22" align="absmiddle" alt=""> Roadmap</h2>

- [x] **m1** — `suiter login feishu` + `suiter feishu doc read <id> --json`; token cached at `~/.suiter/tokens.json`, survives across sessions.
- [x] **m2** — 钉钉 (`calendar list/get/create`) + 企微 (`message send/read`) behind the same `suiter <suite> <verb>` grammar + shared `TokenStore`; a generic dispatcher means adding a suite verb takes zero new CLI glue. The unify-3-suites thesis is proven.
- [x] **m3** — 腾讯文档 (`sheet read/write`) + GLM/DeepSeek summarizer; end-to-end `suiter agent run summarize-and-schedule` (read 飞书 → summarize → 钉钉 calendar event). v0.3 also fixes the dingtalk/wework cached-token-expiry gap (mirroring the v0.2 feishu fix).
- [ ] future — MCP server, team auth, hosted token vault.

After pushing, set repo topics:

```bash
gh repo edit --add-topic coding-agent --add-topic cli --add-topic feishu --add-topic dingtalk
```

<h2><img src="https://api.iconify.design/tabler:license.svg?color=%230071E3&width=24" height="22" align="absmiddle" alt=""> License</h2>

MIT — see [LICENSE](./LICENSE). File issues or PRs on [GitHub Issues](https://github.com/SuperMarioYL/suiter/issues).

## Share this

```
suiter — the Coding-Agent CLI for 飞书/钉钉/企微/腾讯文档. 4 OAuth → 1 `suiter login`. Your agent reads your own CN office data, zero per-app glue. https://github.com/SuperMarioYL/suiter
```

<p align="center"><sub><a href="./LICENSE">MIT</a> © 2026 SuperMarioYL</sub></p>
