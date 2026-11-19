---
name: release-review-lenient
description: 发版前的外部审查（Codex 审 release PR）主流程 OK 就放行，小问题先记下不修
metadata:
  type: feedback
---

发版前审 release PR（2026-11-19 起由 Codex 审）不求严谨：主流程没问题就通过；会崩、丢数据的修，边角的小问题（竞态下用错行这类）记为已知上限、留候选，不挡发版；修完只跑相关脚本，不再为几行修复重跑完整回归。

**Why:** 用户 2026-11-19 原话：「不需要特别严谨, 主流程ok就可通过, 小问题可以暂忽略」。整套审查 / 测试 / 复审对几行修复太费 token，和 [[small-fixes-skip-the-pipeline]] 同一个方向。
**How to apply:** 用 Codex 复审时用 adversarial-review 加 focus「只报会崩、丢数据、主流程走不通的问题」；拿到结果先按「崩 / 丢数据 / 主流程」和「其余」分两档，后一档直接记 task.md 的已知上限。
