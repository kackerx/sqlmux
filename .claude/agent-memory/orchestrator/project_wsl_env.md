---
name: wsl-environment
description: 2026-11-07 起这套 agent 在用户的 Windows WSL（Ubuntu）上跑；路径、Docker、Windows 版怎么测
metadata:
  type: project
---

2026-11-07 用户把 sqlmux 的决策者 / worker / reviewer / tester 搬到 Windows 主机的 WSL（Ubuntu 26.04，/root/proj/sqlmux）上跑，目的是后续迭代顺便直接测 Windows 版。AGENTS.md 里的 `/Users/ctw/...` 是原来 Mac 的路径。

- Docker Desktop 装在 Windows 主机，WSL 是 mirrored 网络：主机容器发布的端口在 WSL 的 localhost 直接通（55432 / 53306 / 56379），2026-11-07 PG / MySQL / Redis 的集成测试从 WSL 跑全绿。WSL integration 已给 Ubuntu 打开（2026-11-07）：`/usr/bin/docker` 和 `/var/run/docker.sock` 由 Docker Desktop 注入，`docker compose ps` 在 /root/proj/sqlmux 里认得 `sqlmux` 项目；没装、也不要装 Linux docker。主机上现有的容器是 compose 项目 `sqlmux`、来自 Windows 检出 `C:\Users\Administrator\dev\sqlmux`（M18 win-worker 时期）；从 WSL `up` 会接管同名项目、重建容器（named volume 保留数据）。SQL Server 的 51433 没发布，f18.7 的 e2e 跑不了，要用时让用户在 compose 里起 mssql。redis-server 本机也有（linuxbrew）。
- Windows 版可以不出 WSL 测：`GOOS=windows go build ./cmd/sqlmux` 出 exe，拷到 `/mnt/c/Users/Administrator/AppData/Local/Temp/...`，用主机的 `/mnt/c/Users/Administrator/AppData/Local/Programs/Python/Python312/python.exe e2e/windows/smoke.py <C:\路径\sqlmux.exe>` 跑 ConPTY 冒烟（pywinpty + pyte 已装）；2026-11-07 在 v0.9.0 上 9 passed。exe 要放 Windows 盘，不要从 `\\wsl.localhost` 路径起。
- `gh` 登的是 kaixuan188，推送 / 合 PR 时要 `gh auth switch -u kackerx`；git 作者是 kackerx。

**Why:** 环境和 AGENTS.md 写的 Mac 不一样，派活前要知道哪些测试跑不了。
**How to apply:** 派 tester 测 Windows 相关 feature 时直接给上面的命令；涉及 PG / MySQL 的批次先确认 docker 通了。
