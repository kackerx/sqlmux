# AGENTS.md

sqlmux：按 tmux 的 session / window / pane 思路组织的终端数据库客户端，Go + Bubble Tea v2，PostgreSQL、MySQL、SQLite 和 Redis。

- 设计与决策以 [`specs/tech-design.md`](specs/tech-design.md)（索引，各章在 `specs/design/`）为准：要改设计，先改文档，再写代码。里程碑索引和任务文件的格式在 [`specs/plan.md`](specs/plan.md)，每个里程碑的清单在 `specs/m<N>-<名字>/task.md`。
- 目录结构见 tech-design §4。增删目录是设计变更，由决策者改 §4。
- 常规操作走 `just`：`just --list` 看全部入口；`just check` 是提交前的全部检查（vet、单测、集成测试、lint、gofmt、tidy）。

## 角色和工作区

| 角色 | 会话名 | 启动命令 | 规则 |
|---|---|---|---|
| 决策者 | `sqlmux-orchestrator` | `claude -n sqlmux-orchestrator --agent orchestrator --effort high` | [`specs/roles/orchestrator.md`](specs/roles/orchestrator.md) |
| worker | `sqlmux-fullstack-dev` | `claude -n sqlmux-fullstack-dev --agent fullstack-dev --effort high` | [`specs/roles/worker.md`](specs/roles/worker.md) |
| 并行 worker | `sqlmux-fullstack-dev-2` | 决策者按 task.md 起的后台会话：在 `/Users/ctw/proj/sqlmux-dev2` 里 `claude --bg -n sqlmux-fullstack-dev-2 --agent fullstack-dev --effort high`，只做 task.md 分给它的 feature，见下面「并行开发」 | 同 worker |
| reviewer | `sqlmux-code-reviewer` | `claude -n sqlmux-code-reviewer --agent code-reviewer --effort xhigh` | [`specs/roles/reviewer.md`](specs/roles/reviewer.md) |
| tester | `sqlmux-test-engineer` | 同上格式，`--agent test-engineer --effort high`，在 `/Users/ctw/proj/sqlmux-e2e` 里起 | [`specs/roles/tester.md`](specs/roles/tester.md) |
| pruner | `sqlmux-pruner` | `claude -n sqlmux-pruner`，用户按需启动，不是必经步骤 | [`specs/roles/pruner.md`](specs/roles/pruner.md) |
| oss | `sqlmux-oss` | `claude -n sqlmux-oss --agent oss-maintainer --effort high`，用户按需启动：README、对外文档、演示 GIF、changelog / release note、发版后装一遍、发布帖、issue | [`specs/roles/oss.md`](specs/roles/oss.md) |

**接手会话时先读自己那份角色规则**，别的角色的不用读。agent 定义在 `~/.claude/agents/`，是通用角色，项目特有的东西只写在本文件和角色规则里。

| 工作区 | 使用者 | 分支 |
|---|---|---|
| `/Users/ctw/proj/sqlmux` | 决策者、worker、reviewer、oss 的会话都在这里起；只有 worker（修剪期间是 pruner）在这里改代码，oss 只改 `README.md`、`docs/`、`CHANGELOG.md`、`assets/`（和决策者一样 `git commit --only -- <路径>`） | `main` |
| `/Users/ctw/proj/sqlmux-dev2` | 并行 worker（只在有并行 feature 时存在，决策者 `git worktree add` 建、合完删） | task.md 写的 feature 分支，如 `f19.3` |
| `/Users/ctw/proj/sqlmux-e2e` | tester | `e2e` |
| `/Users/ctw/proj/sqlmux-review` | reviewer 跑测试 | detached |
| `/Users/ctw/proj/sqlmux-verify` | 决策者编验收版给用户 | detached |

启动和重开都带 `--agent` 和 `--effort`（frontmatter 的 effort 对 `--agent` 起的主会话不生效）；重开用 `claude --resume <会话名> --agent <agent> --effort <同上>`，不要 `claude -c` 或只写 `--resume`，模型会退回默认。上下文到 700–800k token 时在原会话里 `/handoff` 再 `/clear`，名字不变，不通知任何人。

## 固定的工作流（按批）

1. worker 按 task.md 的开发清单逐个 feature 提交到 `main`，每个 commit 自己保证 `just check` 全绿和相关 e2e 通过，不等审查。
2. 到 task.md 标出的**审查节点**，worker 一条消息把 `<上一个节点>..<sha>` 发给 reviewer，逐个 feature 写明重点；同时给 tester 一行「节点 N 已送审：<范围>」。全量 e2e 由 worker 在送审时同时跑。
3. reviewer 整批审：spec 问题先问决策者、等答复后一次退齐；必须改的一条消息退回 worker，worker 修完发修复范围，只复审新增改动；通过且全量 e2e 没有清单外失败后，一条消息整批提测 tester。
4. tester 整批测：有问题退回 worker（修复先经 reviewer 复审）；全部通过把批次结论发给决策者和 worker，决策者填状态 `passed（sha；e2e sha）`。
5. 修复 commit 排在后续 feature 之后时，照常在修复的 sha 上审、测，只看本批的验收项；后续 feature 造成的 e2e 失败由 worker 列出，不算本批问题。

**里程碑结束**：最后一个节点通过、tester 跑完完整回归后暂停，用户体验并提意见；决策者把反馈写成新 feature 追加进 task.md；用户验收通过后 worker 打 tag、推送。暂停期间 worker 不写代码，只可以起草下一个里程碑的清单发给决策者。

**全仓审计**：用户按需让 reviewer 做一次只读的全仓工程质量审计（做法在 `specs/roles/reviewer.md`），每条发现带一个探针，结果发决策者排成一个里程碑（第一次是 M13）。探针和仓库副本放在 worktree 外面（`/Users/ctw/proj/sqlmux-aud/`）：i18n 的单测遍历整个 worktree，未跟踪的 `.go` 副本也会被扫到。

**并行开发**（用户 2026-10-30 定）：task.md 里有和别的 feature 完全隔离的，决策者可以把它分给并行 worker，在 task.md 的审查节点那一行写明「节点 N 由 `sqlmux-fullstack-dev-2` 在分支 `<名字>` 上做」，每个 feature 的标题后面写 `· 负责：dev-2`。一个 feature 只归一个 worker，没写归属的都是 worker 的；谁也不做不归自己的 feature，做着发现要碰对方正在改的东西就停下问决策者。
- **什么算隔离**：不改同一个函数，不依赖对方还没提交的改动，不动 `go.mod` / `go.sum`（新依赖只有 worker 加；并行 worker 要用新依赖先问决策者）、`justfile`、`.golangci.yml`、`testdata/seed/`。同一个文件里不同的函数可以并行；golden 文件两边都会动，不算冲突的理由，按下面的办法合。
- **并行 worker 的流程**：分支从当时的 `main` 拉，只在自己的 worktree 里提交，规矩同 worker（`just check`、相关 e2e、commit 前缀）。集成测试照「集成测试环境」直接连主工作区的容器，不 `up`。到节点先 `git merge main` 进自己的分支、解决冲突（golden 冲突不手合：取 `main` 的那份后重新生成，逐个看 diff 只差自己这条 feature 的改动），`just check` 绿了再送审，范围写 `main..<分支头>`；reviewer 在 `sqlmux-review` 里检出这个 sha 审，tester 在分支头跑全量回归（同 M18 的 `windows-compat`）。退回、提测、结论照常，结论发给决策者和两个 worker。
- **合入**：节点通过后由 worker 在主工作区 `git merge --no-ff <分支>`（合的就是通过的那个 sha）。这期间 `main` 又往前走了、合并有冲突时，交回并行 worker 再 `merge main` 一次，reviewer 只审这次合并的解决，tester 只跑相关脚本。合完决策者填状态、删 worktree 和分支。

**消息只发四类**：spec 问题（→ 决策者）、退回（reviewer / tester → worker）、批次提测（worker → reviewer → tester）、批次结论（tester → 决策者和 worker）。oss 多两类：发版后的安装验证结论（oss → 决策者）、issue 里能复现的 bug 和需求（oss → 决策者，决策者转成 feature 或测试用例）。不发「收到」、抄送、改状态、回报 sha。spec 改了，决策者只给相关角色一行指针（「docs <sha> 改了 §7.8」），reviewer 和 tester 按 commit 里的 spec 工作。

## 通用规则

- 对设计或验收标准有疑问、spec 和实际冲突，问决策者，不自己定产品行为。
- 代码遵循 ponytail 原则（`ponytail:ponytail` skill）：能不写的不写，先复用、再标准库、再已有依赖，最后才写最少的新代码；不做单实现的接口和「以后可能用到」的脚手架；改 bug 找根因改在共用的那一处；有意的捷径用 `ponytail:` 注释标出上限；非平凡逻辑至少一个能运行的测试。仓库里已经只有一处的东西不写第二份，动手前先 grep：浮层的集合和顺序、列表选中项保持可见、单行输入框的按键、toast、事务收尾（`db.End`）、两个驱动共用的保存 / 分页 / 读循环（2026-10-11 的全仓审计里这几类各长出了 2–12 份，M13 收回一处）。
- commit 说明一律英文，前缀 `F<里程碑>.<序号>:`、`docs:`、`e2e:`、`prune:`；不加 `Co-Authored-By` 之类的署名行，会话提示要求加的也不加。作者身份由仓库的 git 配置决定，不改 `user.name` / `user.email`。
- `origin` 是公开仓库 `kackerx/sqlmux`。平时只提交到本地；用户验收通过、worker 打完 tag 后，**决策者转达用户的「推送」**才由 worker 推送，打 tag 不等于推送（2026-10-13 worker 收到批次结论就推了 main 和四个 tag，用户还没验收）。`main` 有 ruleset（2026-10-25 用户定：禁删、禁 force push、改动必须经 PR 合入，没有豁免），所以推送分两步：worker 把 `main` 推成一个分支（`git push origin main:release/<tag>`）开 PR，用户用 kackerx 合并；合完 worker `git fetch` 后在合入的 commit 上打 tag、`git push origin <tag>`。一次推超过 3 个 tag GitHub 不触发任何 workflow（那次 v0.2.2 没出 Release），版本 tag 一次推一个。`e2e` 分支不推送。
- 不对整个工作区 stash / checkout / reset：四个 worktree 共用一个 stash 栈。要暂放工作只处理自己的路径（`git stash push -u -m "<标签>" -- internal/ cmd/`）或提交 WIP commit。
- 凭记忆拿不准的数据库行为（驱动的取消语义、类型的文本格式、information_schema 的版本差异）不要猜，写最小的集成测试在 PG / MySQL 上跑一遍，以结果为准。驱动或依赖已有的能力直接用，不重写（PG 标识符加引号用 `pgx.Identifier{...}.Sanitize()`）。SQL 相关的实现先看开源实现，参考表在 `.claude/rules/sql.md`，碰到 `internal/sqlkit`、`internal/db`、`internal/app` 的文件时自动加载。

**集成测试环境**：`docker compose` 只在主工作区起一份（worker `just db-up`，空转占 400MB 以上，机器紧张时可以停）。其他 worktree 不要执行 `up`：compose 按目录名取项目名会再起一套抢端口，`-p sqlmux` 也不行（seed 挂载路径不同会重建主工作区的容器）。它们直接连：`SQLMUX_TEST_PG` 连 PG，`SQLMUX_TEST_MYSQL='sqlmux:sqlmux@tcp(localhost:53306)/sqlmux'` 连 MySQL，`SQLMUX_TEST_MSSQL` 连 SQL Server（M18 起，51433；Mac 上镜像要 Rosetta，M18 期间连用户 Windows 主机上的容器，地址问用户）；只读测试共用 `sqlmux` 库没问题，要锁表或写库就在同一实例上自己建库（`sqlmux_e2e_<pid>`），用 `testdata/seed/pg.sql` / `mysql.sql` 导入，退出时 drop。seed 只在数据卷为空时导入一次，改了 `testdata/seed/*.sql` 要 `docker compose up -d -V`，worker 改完 seed 要通知 reviewer 和 tester。
