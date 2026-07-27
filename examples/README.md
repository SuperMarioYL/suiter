# Examples

A 3-command happy path (cold clone → first visible result):

```bash
# 1) one OAuth loopback, token cached to ~/.suiter/tokens.json
suiter login feishu

# 2) read a 飞书 doc as agent-readable JSON
suiter feishu doc read <doc-id> --json

# 3) pipe into your Coding Agent
suiter feishu doc read <doc-id> --json | your-agent summarize
```

Set credentials via env (or `~/.suiter/config.yaml`):

```bash
export SUITER_FEISHU_APP_ID=cli_xxx
export SUITER_FEISHU_APP_SECRET=xxx
```
