---
name: small-fixes-skip-the-pipeline
description: 只改 _test 文件、平台差异这类不重要的小修，不走审查 / 测试的整套流程，改完直接重跑 CI
metadata:
  type: feedback
---

只是一个 `_test` 文件的平台问题、判断不重要的小修：让 worker 快速改掉、重跑 CI 就行，不要再从头走审查 → 测试 → 结论的流程。

**Why:** 用户 2026-11-08（v0.10.0 的 Windows CI 红，只是测试断言 0600）：「不需要再重头走流程来浪费 token」。整套流程每条消息都让几个会话各跑一轮，对一行测试改动不值。
**How to apply:** 判断标准是「产品代码不动 + 原因明确 + 一行能改」。这种情况下 worker 直接修、推、看 CI；也别为它顺手立「机制」类的任务（我当时想加 `just test-windows`，用户没要，放到候选）。产品行为有变、或原因没定位清楚的，照常走流程。
