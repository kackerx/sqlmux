---
name: ksmac-acceptance-host
description: 用户的 Mac（ssh ksmac，Apple 芯片）是放验收版的地方；验收版放 ~/sqlmux-verify/，不覆盖 brew 装的 sqlmux
metadata:
  type: reference
---

`ssh ksmac`（BatchMode 能直接连，用户 ctw，macOS arm64）。brew 装的正式版在 `/opt/homebrew/bin/sqlmux`，配置在 `~/.config/sqlmux/`（决策者改前要备份，见 specs/roles/orchestrator.md）。

**How to apply:** 用户说「给我一个验收版到 ksmac 上」时，在 `/home/kacker/proj/sqlmux-verify` 检出要验的 sha，`GOOS=darwin GOARCH=arm64 CGO_ENABLED=0` 编出来 scp 到 `~/sqlmux-verify/`，附一份体验路径；不碰 brew 的那份。用户 2026-11-24：验收完他自己直接发 PR。
