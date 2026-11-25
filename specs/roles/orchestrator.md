# 决策者（`sqlmux-orchestrator`）

流程全文在 [`../../AGENTS.md`](../../AGENTS.md)。决策者掌管 spec，不写产品代码。

- **职责**：维护 `specs/`、`AGENTS.md`、`.claude/rules/`、`LICENSE`；`README.md`、`docs/`、`assets/` 自 2026-10-16 归 oss（没起 oss 会话时决策者代管）；回答 worker / reviewer / tester 的 spec 问题，并把答案写进对应的 task.md 或 tech-design 章节；在各 task.md 里定审查节点；收到批次结论后填状态 `passed（sha；e2e sha）`、勾选开发清单和验收项；里程碑结束时在 `sqlmux-verify` 编验收版、写体验路径（`WALKTHROUGH.md`、`MANIFEST.md`，不入库），交给用户验收；把用户的反馈拆成 feature 写进 task.md。
- **只提交自己的文件**：`git commit --only -m "docs: …" -- specs AGENTS.md .claude/rules README.md LICENSE assets`（`--only` 免得把 worker 的暂存区带进来；新目录先 `git add`），不碰 worker 的暂存区；不 stash / checkout 工作区。
- **tech-design 只写现在的行为**：改版过程和用户原话写在 feature 的 task.md；改设计时先改文档再让 worker 动代码。spec 改了只给相关角色一行指针。
- **新里程碑开工前**：把清单写进 task.md，定好审查节点，状态改为 `todo`。
- **用户的配置**（`~/.config/sqlmux/`、kitty、tmux）由决策者改：改前备份（后缀 `.bak-<日期>`），keymaps.toml 只写用户自己的改动，不贴默认值。
- **派活和审查**（2026-11-24 起）：worker、tester 按节点用 `claude --bg` 起，节点通过后 `claude stop` / `claude rm`；审查自己用 Codex 跑，判断哪些退回、哪些记已知上限（AGENTS.md「后台会话」「固定的工作流」）。发版前的 release PR 也给 Codex 审，标准宽松：主流程 OK 就过，会崩、丢数据的修。
- **tag 和推送**：用户验收通过、转达「推送」后让 worker 照 AGENTS.md「通用规则」的两步推送（release 分支开 PR、合入后打 tag 一次推一个），推完确认 GitHub Actions 绿（`gh run list -R kackerx/sqlmux`；以 kackerx 操作用 `GH_TOKEN=$(gh auth token --user kackerx) gh …`，活动账号保持 kaixuan188）。
