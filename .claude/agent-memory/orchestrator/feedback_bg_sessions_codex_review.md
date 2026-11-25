---
name: bg-sessions-and-codex-review
description: 2026-11-24 起不留常驻会话：worker 按节点用 claude --bg 起、做完收掉；审查和测试都交给 Codex（astra / sol 看活定，不进沙箱）
metadata:
  type: feedback
---

用户把工作流的常驻会话都关了。以后决策者需要执行任务时用 `claude --bg --agent <角色> -n sqlmux-<角色>-<任务> "<任务>"` 起后台会话，一件事一个会话，做完 `claude stop` / `claude rm`；审查不再派给 `sqlmux-code-reviewer`，由决策者用 Codex 插件跑（`codex-companion.mjs review --base <ref> --scope branch`，复审 `adversarial-review --wait --base <ref> --scope branch "<focus>"`），标准见 [[release-review-lenient]]。

**Why:** 用户 2026-11-24 原话：「工作流里的几个会话我都关了, 修改下有需要用bg来启动执行任务也避免上下文堆积, 另外审查的任务交给codex去做吧」。常驻会话上下文越堆越大，每条消息都重读。
**How to apply:** 派活前不找常驻会话，直接起 bg；流程细节已写进 AGENTS.md「后台会话」和「固定的工作流」（a596282），以那里为准。以 kacker 运行，不需要 `IS_SANDBOX=1`（那是 root 时的）。

同日追加：用户说「后续测试和审查, 分别开codex的astra6.2去做吧」：审查和 e2e 测试各起一个 Codex 任务，模型 `gpt-6.2-astra`（本机 models_cache 里有）；tester 的 Claude 会话不再起。
同日再追加：「如果判断会花费较多token和上下文的话就用6.2sol」「不要让codex在沙箱里跑, 直接全权限运行」：范围小的用 astra，重的（e2e、完整回归、大批审查）用 `gpt-6.2-sol`；一律 `codex exec --dangerously-bypass-approvals-and-sandbox`，不走插件（插件把沙箱写死成 read-only / workspace-write）。
同日再追加：「后面开发者也直接用opus5.5 high吧, sonnect效率性能还是弱点」：worker 的 bg 会话带 `--model opus --effort high`，盖过 fullstack-dev 定义里的 Sonnet。
