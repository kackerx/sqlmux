# AGENTS.md

sqlmux：按 tmux 的 session / window / pane 思路组织的终端数据库客户端，Go + Bubble Tea v2，PostgreSQL、MySQL、SQLite 和 Redis。

- 设计与决策以 [`specs/tech-design.md`](specs/tech-design.md)（索引，各章在 `specs/design/`）为准：要改设计，先改文档，再写代码。里程碑索引和任务文件的格式在 [`specs/plan.md`](specs/plan.md)，每个里程碑的清单在 `specs/m<N>-<名字>/task.md`。
- 目录结构见 tech-design §4。增删目录是设计变更，由决策者改 §4。
- 常规操作走 `just`：`just --list` 看全部入口；`just check` 是提交前的全部检查（vet、单测、集成测试、lint、gofmt、tidy）。

## 角色和工作区

| 角色 | 会话名 | 启动命令 | 规则 |
|---|---|---|---|
| 决策者 | `sqlmux-orchestrator` | `claude -n sqlmux-orchestrator --agent orchestrator --effort high` | [`specs/roles/orchestrator.md`](specs/roles/orchestrator.md) |
| worker | `sqlmux-dev-n<节点>` | 决策者每个审查节点起一个后台会话：主工作区里 `claude --bg -n sqlmux-dev-n<节点> --agent fullstack-dev --model opus --effort high "<任务>"`（用户 2026-11-24 定用 Opus，agent 定义里的 Sonnet 不够用） | [`specs/roles/worker.md`](specs/roles/worker.md) |
| 并行 worker | `sqlmux-dev2-n<节点>` | 同 worker，在 `/home/kacker/proj/sqlmux-dev2` 里起，只做 task.md 分给它的 feature，见下面「并行开发」 | 同 worker |
| 审查 | Codex | 决策者在 `/home/kacker/proj/sqlmux-review` 里跑 Codex 插件，见「固定的工作流」第 3 步 | [`specs/roles/reviewer.md`](specs/roles/reviewer.md) 是审查重点和全仓审计的做法 |
| 测试 | Codex | 决策者在 `/home/kacker/proj/sqlmux-e2e` 里跑 Codex 的 task，见「固定的工作流」第 4 步 | [`specs/roles/tester.md`](specs/roles/tester.md) 是测试的做法，写进给 Codex 的任务 |
| pruner | `sqlmux-pruner` | `claude -n sqlmux-pruner`，用户按需启动，不是必经步骤 | [`specs/roles/pruner.md`](specs/roles/pruner.md) |
| oss | `sqlmux-oss` | `claude -n sqlmux-oss --agent oss-maintainer --effort high`，用户按需启动：README、对外文档、演示 GIF、changelog / release note、发版后装一遍、发布帖、issue | [`specs/roles/oss.md`](specs/roles/oss.md) |

**接手会话时先读自己那份角色规则**，别的角色的不用读。agent 定义在 `~/.claude/agents/`，是通用角色，项目特有的东西只写在本文件和角色规则里。

**后台会话和 Codex**（用户 2026-11-24 定）：worker 不常驻，一个节点一个会话，免得上下文越堆越大；审查和测试都交给 Codex，各起一个任务，不进沙箱、全权限运行（用户定；Codex 插件的命令把沙箱写死了，所以直接用 `codex exec`）。模型由决策者看活定：范围小的用 `gpt-6.2-astra`，会耗大量 token 和上下文的（e2e 测试、完整回归、大批的审查）用 `gpt-6.2-sol`。决策者派活时把任务、队友规则和「完成后用 SendMessage 把结论发给 sqlmux-orchestrator」写进启动命令；会话的名字就是收件地址。节点通过后决策者 `claude stop` 再 `claude rm` worker 会话。worker 会话管到节点通过为止，退回的修复也由它做；里程碑结束后用户验收追加的 feature 照样按节点起新会话。

| 工作区 | 使用者 | 分支 |
|---|---|---|
| `/home/kacker/proj/sqlmux` | 决策者、worker、oss 的会话都在这里起；只有 worker（修剪期间是 pruner）在这里改代码，oss 只改 `README.md`、`docs/`、`CHANGELOG.md`、`assets/`（和决策者一样 `git commit --only -- <路径>`） | `main` |
| `/home/kacker/proj/sqlmux-dev2` | 并行 worker（只在有并行 feature 时存在，决策者 `git worktree add` 建、合完删） | task.md 写的 feature 分支，如 `f19.3` |
| `/home/kacker/proj/sqlmux-e2e` | Codex 的测试任务（文中的 tester 都指它） | `e2e` |
| `/home/kacker/proj/sqlmux-review` | 决策者检出送审的 sha 给 Codex 审 | detached |
| `/home/kacker/proj/sqlmux-verify` | 决策者编验收版给用户 | detached |

启动和重开都带 `--agent` 和 `--effort`（frontmatter 的 effort 对 `--agent` 起的主会话不生效）；重开用 `claude --resume <会话名> --agent <agent> --effort <同上>`，不要 `claude -c` 或只写 `--resume`，模型会退回默认。上下文到 700–800k token 时在原会话里 `/handoff` 再 `/clear`，名字不变，不通知任何人。

## 固定的工作流（按批）

1. 决策者为一个审查节点起 worker 会话。worker 按 task.md 的开发清单逐个 feature 提交到 `main`，每个 commit 自己保证 `just check` 全绿和相关 e2e 通过。
2. 到 task.md 标出的**审查节点**，worker 一条消息把 `<上一个节点>..<sha>` 发给决策者，逐个 feature 写明重点；全量 e2e 由 worker 在送审时同时跑，跑完有清单外失败再补一条。
3. 决策者用 Codex 整批审：`git -C /home/kacker/proj/sqlmux-review checkout --detach <sha>`，在那里 `codex exec review --dangerously-bypass-approvals-and-sandbox -m gpt-6.2-astra --base <上一个节点> -o <结论文件>`（放进后台的 Bash 跑，几分钟）。Codex 的结论是「找到这几处」，不是「其余没问题」，所以先对照 spec 逐条判断：会崩、丢数据、违背 spec 的退回 worker（一条消息，附文件行号和复现）；边角问题记进 task.md 的已知上限。worker 修完发修复范围，决策者用同一条命令、`--base <修复前的 sha>`、末尾加要确认的点，只复审修复。lint 和 `just check` 由 worker 保证，决策者不重跑。通过且全量 e2e 没有清单外失败后进第 4 步。
4. 决策者用 Codex 整批测：`/home/kacker/proj/sqlmux-e2e` 里先 `git merge <sha>`，再 `codex exec --dangerously-bypass-approvals-and-sandbox -m gpt-6.2-sol -c model_reasoning_effort=high -C /home/kacker/proj/sqlmux-e2e -o <结论文件> - < <任务文件>`（后台的 Bash 跑）。任务照 tester.md 写：要覆盖的验收项、环境、已知上限、只改 `e2e/` 并以 `e2e:` 提交到 `e2e` 分支、结论逐项 PASS / FAIL 附复现。Codex 发不了消息，有问题由决策者附复现退回 worker（修复先经第 3 步的复审，再只跑相关脚本）；全部通过决策者填状态 `passed（sha；e2e sha）`，收掉 worker 会话。里程碑末的完整回归也这样跑。
5. 修复 commit 排在后续 feature 之后时，照常在修复的 sha 上审、测，只看本批的验收项；后续 feature 造成的 e2e 失败由 worker 列出，不算本批问题。

**里程碑结束**：最后一个节点通过、tester 跑完完整回归后暂停，用户体验并提意见；决策者把反馈写成新 feature 追加进 task.md；验收期间追加的节点可以只跑相关脚本，但**发版前 tester 在最终的 sha 上重跑一次完整回归**（M21 节点 3 没重跑，F21.7 改的共用补全框把 WHERE 历史下拉弄坏了，带着发了 v0.10.0）；用户验收通过后 worker 打 tag、推送。暂停期间 worker 不写代码，只可以起草下一个里程碑的清单发给决策者。

**全仓审计**：用户按需让决策者起一个 `--agent code-reviewer --effort xhigh` 的后台会话做一次只读的全仓工程质量审计（做法在 `specs/roles/reviewer.md`），每条发现带一个探针，结果发决策者排成一个里程碑（第一次是 M13）。探针和仓库副本放在 worktree 外面（`/home/kacker/proj/sqlmux-aud/`）：i18n 的单测遍历整个 worktree，未跟踪的 `.go` 副本也会被扫到。

**并行开发**（用户 2026-10-30 定）：task.md 里有和别的 feature 完全隔离的，决策者可以把它分给并行 worker，在 task.md 的审查节点那一行写明「节点 N 由并行 worker 在分支 `<名字>` 上做」，每个 feature 的标题后面写 `· 负责：dev-2`。一个 feature 只归一个 worker，没写归属的都是 worker 的；谁也不做不归自己的 feature，做着发现要碰对方正在改的东西就停下问决策者。
- **什么算隔离**：不改同一个函数，不依赖对方还没提交的改动，不动 `go.mod` / `go.sum`（新依赖只有 worker 加；并行 worker 要用新依赖先问决策者）、`justfile`、`.golangci.yml`、`testdata/seed/`。同一个文件里不同的函数可以并行；golden 文件两边都会动，不算冲突的理由，按下面的办法合。
- **并行 worker 的流程**：分支从当时的 `main` 拉，只在自己的 worktree 里提交，规矩同 worker（`just check`、相关 e2e、commit 前缀）。集成测试照「集成测试环境」直接连主工作区的容器，不 `up`。到节点先 `git merge main` 进自己的分支、解决冲突（golden 冲突不手合：取 `main` 的那份后重新生成，逐个看 diff 只差自己这条 feature 的改动），`just check` 绿了再送审，范围写 `main..<分支头>`；决策者在 `sqlmux-review` 里检出这个 sha 审，tester 在分支头跑全量回归（同 M18 的 `windows-compat`）。退回、提测、结论照常，结论发给决策者和两个 worker。
- **合入**：节点通过后由 worker 在主工作区 `git merge --no-ff <分支>`（合的就是通过的那个 sha）。这期间 `main` 又往前走了、合并有冲突时，交回并行 worker 再 `merge main` 一次，决策者只审这次合并的解决，tester 只跑相关脚本。合完决策者填状态、删 worktree 和分支。

**消息只发四类**：spec 问题（→ 决策者）、退回（决策者转 Codex 审查和测试的问题 → worker）、送审（worker → 决策者）。oss 多两类：发版后的安装验证结论（oss → 决策者）、issue 里能复现的 bug 和需求（oss → 决策者，决策者转成 feature 或测试用例）。不发「收到」、抄送、改状态、回报 sha。spec 改了，决策者只给相关角色一行指针（「docs <sha> 改了 §7.8」），worker 按 commit 里的 spec 工作。

## 通用规则

- 对设计或验收标准有疑问、spec 和实际冲突，问决策者，不自己定产品行为。
- 代码遵循 ponytail 原则（`ponytail:ponytail` skill）：能不写的不写，先复用、再标准库、再已有依赖，最后才写最少的新代码；不做单实现的接口和「以后可能用到」的脚手架；改 bug 找根因改在共用的那一处；有意的捷径用 `ponytail:` 注释标出上限；非平凡逻辑至少一个能运行的测试。仓库里已经只有一处的东西不写第二份，动手前先 grep：浮层的集合和顺序、列表选中项保持可见、单行输入框的按键、toast、事务收尾（`db.End`）、两个驱动共用的保存 / 分页 / 读循环（2026-10-11 的全仓审计里这几类各长出了 2–12 份，M13 收回一处）。
- commit 说明一律英文，前缀 `F<里程碑>.<序号>:`、`docs:`、`e2e:`、`prune:`；不加 `Co-Authored-By` 之类的署名行，会话提示要求加的也不加。作者身份由仓库的 git 配置决定，不改 `user.name` / `user.email`。
- `origin` 是公开仓库 `kackerx/sqlmux`。平时只提交到本地；用户验收通过、worker 打完 tag 后，**决策者转达用户的「推送」**才由 worker 推送，打 tag 不等于推送（2026-10-13 worker 收到批次结论就推了 main 和四个 tag，用户还没验收）。`main` 有 ruleset（2026-10-25 用户定：禁删、禁 force push、改动必须经 PR 合入，没有豁免），所以推送分两步：worker 把 `main` 推成一个分支（`git push origin main:release/<tag>`）开 PR，用户用 kackerx 合并；合完 worker `git fetch` 后在合入的 commit 上打 tag、`git push origin <tag>`。一次推超过 3 个 tag GitHub 不触发任何 workflow（那次 v0.2.2 没出 Release），版本 tag 一次推一个。`e2e` 分支不推送。
- 不对整个工作区 stash / checkout / reset：四个 worktree 共用一个 stash 栈。要暂放工作只处理自己的路径（`git stash push -u -m "<标签>" -- internal/ cmd/`）或提交 WIP commit。
- 凭记忆拿不准的数据库行为（驱动的取消语义、类型的文本格式、information_schema 的版本差异）不要猜，写最小的集成测试在 PG / MySQL 上跑一遍，以结果为准。驱动或依赖已有的能力直接用，不重写（PG 标识符加引号用 `pgx.Identifier{...}.Sanitize()`）。SQL 相关的实现先看开源实现，参考表在 `.claude/rules/sql.md`，碰到 `internal/sqlkit`、`internal/db`、`internal/app` 的文件时自动加载。

**集成测试环境**：`docker compose` 只在主工作区起一份（worker `just db-up`，空转占 400MB 以上，机器紧张时可以停）。其他 worktree 不要执行 `up`：compose 按目录名取项目名会再起一套抢端口，`-p sqlmux` 也不行（seed 挂载路径不同会重建主工作区的容器）。它们直接连：`SQLMUX_TEST_PG` 连 PG，`SQLMUX_TEST_MYSQL='sqlmux:sqlmux@tcp(localhost:53306)/sqlmux'` 连 MySQL，`SQLMUX_TEST_MSSQL` 连 SQL Server（M18 起，51433；Mac 上镜像要 Rosetta，M18 期间连用户 Windows 主机上的容器，地址问用户）；只读测试共用 `sqlmux` 库没问题，要锁表或写库就在同一实例上自己建库（`sqlmux_e2e_<pid>`），用 `testdata/seed/pg.sql` / `mysql.sql` 导入，退出时 drop。seed 只在数据卷为空时导入一次，改了 `testdata/seed/*.sql` 要 `docker compose up -d -V`，worker 改完 seed 要在送审消息里写明。

**WSL（2026-11-07 起）**：整套会话在用户 Windows 主机的 WSL（Ubuntu）上跑；2026-11-19 起以普通用户 `kacker` 运行（原来是 root：root 下权限类的断言测不了，后台会话还得带 `IS_SANDBOX=1`），工作区就是上表的 `/home/kacker/proj/…`（搬家后 `git worktree repair` 过）。docker 是 Docker Desktop 的 WSL integration（不装 Linux docker）；compose 项目 `sqlmux` 只从主工作区操作，不再从 Windows 的检出里 `up`。Windows 版的验证不用离开 WSL：`GOOS=windows go build -o <C 盘路径>/sqlmux.exe ./cmd/sqlmux`，用主机的 Python（`/mnt/c/Users/Administrator/AppData/Local/Programs/Python/Python312/python.exe`，pywinpty 和 pyte 已装）跑 `e2e/windows/smoke.py <C:\路径\sqlmux.exe>`；exe 放 C 盘，不从 `\\wsl.localhost` 起。SQL Server 的容器没起（51433 不通），f18.7 的 e2e 算清单外环境问题，要测 mssql 时让用户在 compose 里起。全量 e2e 在这台机器串行要 3 小时以上，用 6 路并行（约 50 分钟），并行里红的串行重跑一次再算。
