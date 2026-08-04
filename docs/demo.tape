Output assets/demo.gif

Set FontSize 16
Set Width 1200
Set Height 640
Set Theme "Dracula"
Set Padding 20

# m2 unified — 3 suites, 1 `suiter <suite> <verb>` grammar, no per-suite CLI glue.
Type "suiter suites"
Enter
Sleep 1000ms

# unified verb on 钉钉 (calendar list) — same grammar as feishu, no per-suite wiring
Type "suiter dingtalk calendar list --json"
Enter
Sleep 1500ms

# unified verb on 企微 (message read) — same grammar, same shared TokenStore
Type "suiter wework message read MSGdemo --json"
Enter
Sleep 1500ms

# m1 star moment still wired — feishu doc read → agent-readable JSON
Type "suiter feishu doc read DOXCdemo --json"
Enter
Sleep 1500ms
