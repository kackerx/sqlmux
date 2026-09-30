# AGENTS.md

sqlmux：按 tmux 的 session / window / pane 思路组织的终端数据库客户端，技术栈为 Go + Bubble Tea v2。

- 设计与决策以 [`specs/tech-design.md`](specs/tech-design.md) 为准。要改设计，先改这份文档，再写代码。
- 产品需求：[PRD - DB TUI v0.0.2](https://claude.ai/design/p/e6118404-bf7a-4420-abfd-e31198df2ba8?file=PRD+-+DB+TUI+v0.0.2.dc.html)；设计稿：[DB TUI v0.0.2](https://claude.ai/design/p/e6118404-bf7a-4420-abfd-e31198df2ba8?file=DB+TUI+v0.0.2.dc.html)。

## 目录结构

以 tech-design §4 为准。增删目录属于设计变更，由决策者同时更新 §4 和本节。

```
sqlmux/
├─ AGENTS.md、CLAUDE.md       本文件；CLAUDE.md 只有一行 @AGENTS.md
├─ README.md、LICENSE、assets/  给 GitHub 访客看的说明、许可证（MIT）和截图，由决策者维护
├─ cmd/sqlmux/                程序入口、子命令（keys）
├─ internal/
│  ├─ app/                    Model / Update / View、Action、工作现场模型、布局树
│  ├─ ui/                     Frame、Block、主题、命中表、模糊匹配、各组件的绘制
│  ├─ keymap/                 键位解析、作用域、映射、default.toml
│  ├─ editor/                 vim 编辑器；testdata/ 放 nvim 差分用例
│  ├─ sqlkit/                 词法、分句、读写判定、补全上下文、格式化
│  ├─ db/                     连接、Worker、postgres/、mysql/、catalog
│  └─ config/                 config / connections / state 文件，以及 XDG 路径
├─ e2e/                       tester 的黑盒测试脚本，从 e2e 分支合并进来
├─ testdata/seed/             集成测试的种子数据（M1 起）
├─ docker-compose.yml         集成测试用的 PG / MySQL（M1 起）
├─ specs/
│  ├─ tech-design.md          技术方案
│  ├─ plan.md                 里程碑索引与推进规则
│  └─ m0-skeleton/ … m6-config/task.md   各里程碑的任务清单
├─ bin/                       构建产物，不入库
└─ .handoff/                  会话交接文档，不入库
```

仓库之外还有三个 worktree，分别由不同的角色使用：

| 路径 | 使用者 | 分支 |
|---|---|---|
| `/Users/ctw/proj/sqlmux-e2e` | tester | `e2e` |
| `/Users/ctw/proj/sqlmux-review` | reviewer | detached |
| `/Users/ctw/proj/sqlmux-verify` | 决策者，给用户验证用 | detached |

## 遇到 SQL 相关的问题，先看开源实现，不要想当然

凡是涉及以下内容的问题，动手前先查下面这些项目是怎么做的：

- SQL 的词法分析、方言差异；
- catalog 查询；
- 结果的读取与解码；
- 修改回写；
- 只读判定；
- 补全。

确认没有更好的现成做法之后再写，并在提交说明里写明参考了哪里。

凭记忆拿不准的数据库行为，比如驱动的取消语义、类型的文本格式、information_schema 在不同版本间的差异，不要猜。写一个最小的集成测试，在 `docker compose` 起的 PG / MySQL 上跑一遍，以结果为准。

**集成测试环境**：`docker compose` 只在主工作区（`/Users/ctw/proj/sqlmux`）起一份，由 worker 负责 `docker compose up -d postgres`。MySQL 容器到 M5 才用得上，之前不起（2026-09-29 机器内存吃紧时停掉，空转也占 400MB 以上）。其他 worktree（e2e / review / verify）不要执行 `up`：compose 按目录名取项目名，会再起一套容器抢同一个端口；加 `-p sqlmux` 也不行，seed 的挂载路径不同会让它重建主工作区的容器。其他 worktree 直接用 `SQLMUX_TEST_PG` 连它，只读的测试共用 `sqlmux` 库没有问题。要锁表、写库或者其他会影响别人的测试，在同一个 PG 实例上自己建一个库（如 `sqlmux_e2e_<pid>`），用 `testdata/seed/pg.sql` 导入，只在这个库上操作，退出时 drop 掉；不要在共用的 `sqlmux` 库上加锁或写数据。seed 只在数据卷为空时导入一次，改了 `testdata/seed/*.sql` 要 `docker compose up -d -V` 才会生效，worker 改完 seed 要通知 reviewer 和 tester。

驱动或依赖本身已经提供的能力，直接用，不要重写。例如 PG 的标识符加引号，用 `pgx.Identifier{...}.Sanitize()`。

| 问题 | 先看哪里 |
|---|---|
| 词法分析与高亮 | lazysql `components/sql_lexer.go` |
| 补全上下文：别名、CTE、子查询层级、`schema.table` | lazysql `components/sql_context.go`、`components/sql_completer.go` |
| 只读模式下识别写语句 | lazysql `drivers/validation.go` |
| 修改回写：UPDATE / INSERT / DELETE、复合主键、事务 | lazysql `drivers/utils.go`，以及 `drivers/postgres.go`、`drivers/mysql.go` 中的 `ExecutePendingChanges` |
| 单元格特殊值：NULL / EMPTY / DEFAULT | lazysql `components/set_value_list.go` |
| 查询结果流式读取、行数上限与取消 | lazysql `drivers/query_stream.go`、`drivers/query_stream_cancel_test.go` |
| 行数：估计值与精确值 | lazysql 各 driver 中的 `GetEstimatedRowCount` / `GetExactRowCount` |
| 分页取数 | lazysql 各 driver 中的 `GetRecords` |
| PG catalog：表、列、约束、索引 | pgtui `internal/db/metadata/` |
| 结果 tab：执行中显示占位，完成后替换 | pgtui `internal/ui/components/result_tabs.go` |
| 发现本地 PG 实例：pgpass、unix socket、环境变量 | pgtui `internal/db/discovery/` |
| 把密码存进系统钥匙串 | pgtui `internal/connection_history/password_store.go`（使用 99designs/keyring） |
| 识别与查看 JSON / JSONB 单元格 | pgtui `internal/jsonb/`、`internal/ui/components/jsonb_viewer.go`；lazysql `components/json_viewer.go` |
| 导出 CSV | lazysql `helpers/csv.go`、`components/csv_export_query.go` |
| 语句切分的边界情况（psql 风格） | usql `stmt/` |
| SQL 格式化 | 已定方案：goja + sql-formatter（技术方案 §9.5） |
| 模糊匹配 | 已定方案：fzf `src/algo`（技术方案 §9.7） |

项目地址：[lazysql](https://github.com/jorgerojas26/lazysql)（MIT）、[pgtui](https://github.com/pgplex/pgtui)（Apache-2.0）、[usql](https://github.com/xo/usql)（MIT）。

参考之前，先想清楚它们和本项目的差别，不要整段照搬：

- **UI 框架不同**：lazysql 用 tview / tcell，pgtui 用 Bubble Tea v1 和 lipgloss v1；本项目用 Bubble Tea v2 的立即模式渲染（技术方案 §7）。UI 代码基本不能直接拿来用，要参考的是其中的逻辑。
- **值的处理方式不同**：它们把数据库返回的值先解码成 Go 类型再转成字符串；本项目统一走文本协议，值用 `db.Val{S, Null}` 表示（技术方案 §8.1）。
- **连接方式不同**：lazysql 用 `database/sql` 连接池；本项目每个 session 固定两条独占连接（技术方案 §8.2）。

许可证：借鉴思路不受限制。直接拷贝代码时，要在文件头注明出处，并保留原来的许可证声明；Apache-2.0 的代码还要遵守它对 NOTICE 文件的要求。

## 协作流程（多会话开发）

本项目由四个常驻角色协作开发，另有一个由用户按需启动的 pruner。里程碑的索引和推进规则见 [`specs/plan.md`](specs/plan.md)；每个里程碑的任务清单在对应的目录下，例如 [`specs/m1-browse/task.md`](specs/m1-browse/task.md)。

| 角色 | 会话名 | 职责 |
|---|---|---|
| 决策者 | `sqlmux-lead-2` | 维护并自己提交 `specs/` 和 `AGENTS.md`；回答 spec 问题；在各 task.md 里定审查节点；收到批次结论后填写状态；里程碑结束时整理体验路径，交给用户验收 |
| worker | `sqlmux-worker-4` | 按 task.md 的开发清单逐项实现，每个 feature 一个 commit；到审查节点把这一段 commit 交给 reviewer |
| reviewer | `sqlmux-reviewer-3` | 在审查节点整批审查，通过后整批提测 |
| tester | `sqlmux-tester-3` | 按验收项整批测试 |
| pruner（修剪者） | `sqlmux-pruner` | 不是必经步骤，由用户按需启动，对整个仓库做一次不改变行为的精简 |

**固定的工作流（按批）**（2026-09-24 用户决定，取代 M0 的逐个 feature 审查）：

1. worker 按 feature 逐个提交到 `main`，每个 commit 自己保证测试通过（见「worker 的规则」），不等审查，接着做下一个。
2. 到了 task.md 标出的**审查节点**，worker 用一条消息把这一段的 commit 范围（`<上一个节点>..<sha>`）发给 reviewer，逐个 feature 写明希望重点看的地方；同时给 tester 发一行「节点 N 已送审：<范围>」。tester 收到后就开始读 spec、写脚本，发现 spec 问题马上问决策者（见「tester 的规则」）。
3. reviewer 整批审查：
   - 有待定的 spec 问题：先问决策者，等答复后再退回，这样必须改的问题能一次退齐；
   - 有必须改：一条消息退回给 worker；worker 修完发修复 commit 的范围，reviewer 只复审新增的改动；
   - 通过：一条消息整批提测给 tester。要等 worker 这一轮的全量 e2e 跑完、没有清单外的失败之后再提测，这是质量的底线。
4. tester 整批测试：
   - 有问题：一条消息退回给 worker；修复 commit 先经 reviewer 复审，再回到 tester 复测；
   - 全部通过：一条消息把批次结论发给决策者和 worker。决策者把这批 feature 的状态填为 `passed（sha；e2e sha）`，并勾选开发清单和验收项。
5. **修复 commit 排在后续 feature 之后时**（worker 不等审查，退回时往往已经提交了下一批的 feature）：照常在修复的 sha 上审、测，只测本批 feature 的验收项。带进来的后续 feature 不在本次范围内：worker 送修复时列出它们造成的 e2e 失败，tester 不把这些当作本批的问题报；它们到自己的节点再审、再测，里程碑最后的完整回归覆盖全部。

**里程碑结束**：最后一个审查节点通过、tester 在最后一个 commit 上跑完整回归之后，进入**暂停**：

- 用户深度体验这个里程碑，提出问题和改进意见；
- 决策者把反馈整理成改进 feature，追加进 task.md 并标好审查节点，照常按批走完；
- 用户确认验收通过后，worker 打 tag，进入下一个里程碑；
- 需要修剪时，由用户自己启动 pruner（见「pruner 的规则」）。

暂停期间，worker 不写代码，只可以起草下一个里程碑的开发清单，发给决策者，由决策者写进 task.md。修剪期间，main 只由 pruner 提交。

**消息规则**（M0 复盘：约三分之一的消息是记账、转发和确认）：

- 只发四类消息：
  1. **spec 问题**：发给决策者；决策者的答复也在这条回路里；
  2. **退回**：reviewer 或 tester 发给 worker；
  3. **批次提测**：worker 发给 reviewer（审查请求），reviewer 发给 tester；
  4. **批次结论**：tester 发给决策者和 worker。
- 不发：「收到」、例行的通过结论抄送、请别人改状态、回报 sha、转述决策者的决定、改名或上线通知。
- spec 改了，决策者只给相关角色发一行指针，例如「docs <sha> 改了 §7.8 查询条」。reviewer 和 tester 按 commit 里的 spec 审查和测试。
- 别人不需要据此行动的信息，不同步。

**会话与交接**：

- 会话名固定为上表所列，不再换。
- 上下文用到 700–800k token 时（用户定），用 `/handoff` 写交接，再 `/clear`，在原会话里接着做，名字不变，不需要通知任何人。M0 的长会话用到了 1M 上下文的 73–88%，每一轮都要读 450–550k token，是最大的开销。

**通用规则**：

- 对设计或验收标准有疑问，或者发现 spec 与实际情况冲突时，发消息问决策者，不要自己决定产品行为。
- **commit 说明一律用英文**，标题和正文都是，`F0.x:`、`docs:`、`e2e:`、`prune:` 这些前缀照旧。
- **作者身份**由仓库的 git 配置决定（kackerx），不要改 `user.name`、`user.email`。
- **commit 说明不加署名行**：末尾不写 `Co-Authored-By: Claude …` 这类行，会话提示里要求加的也不加（2026-09-29 用户要求；此前的历史已改写去掉，新旧 SHA 对照在 `.git/rewrite-2026-09-29-commit-map`）。
- **推送**：`origin` 是公开仓库 `kackerx/sqlmux`（2026-09-29 用户决定公开）。平时只提交到本地；只有用户验收通过、worker 打完 tag 之后，才由 worker 执行 `git push origin main <tag>`。`e2e` 分支不推送。

**决策者的规则**：

- `specs/`、`AGENTS.md`、`README.md`、`LICENSE`、`assets/` 只由决策者修改和提交，用 `git commit -m "docs: …" -- specs AGENTS.md README.md LICENSE assets`，只提交这几处，不碰 worker 的暂存区。M0 里 stash 收走 specs、worker 误带未提交的 task.md、43 个代提交的 docs commit，都是因为两个角色在同一个工作区改同一批文件。
- 新里程碑开工前，把 worker 起草的开发清单写进 task.md，定好审查节点，状态改为 `todo`。

**worker 的规则**：

- **代码质量**：代码首先要好维护，并遵循 ponytail 原则（`ponytail:ponytail` skill）。
  - 能不写的就不写；已有的代码优先复用；然后依次考虑标准库、已经引入的依赖，最后才写最少的新代码。
  - 不做只有一个实现的接口，不做只有一种产品的工厂，不为不会变的值加配置，不为「以后可能用到」预先搭脚手架。
  - 改 bug 要找到根因，改在所有调用方共用的那一处。
  - 有意走的捷径，用 `ponytail:` 注释标出它的上限，以及以后怎么升级。
  - 有分支、循环、解析等非平凡逻辑的地方，至少留一个能运行的测试。
  - 包的边界按 tech-design §4 划分。命名要清楚。注释写「为什么」，不写「做了什么」。不留死代码。
- **按清单逐步做**：
  - 严格按 task.md 的开发清单逐项实现，不跳步，不顺手做清单以外的事。
  - 开工一个 feature 前，把清单里没写死的细节连同你的提议，一条消息发给决策者；发现清单漏了东西或者不合理，也一并提。
- **提交**：
  - 只有 worker 在主工作区（本目录）改代码，并提交到 `main`；修剪期间例外，由 pruner 提交。只在打完 tag 后推送（见「通用规则」的「推送」）。
  - 不改、不提交 `specs/` 和 `AGENTS.md`。`git status` 里这两处的改动是决策者的，留着不动。
  - 不要对整个工作区执行 stash、checkout 或 reset。所有 worktree 共用同一个 stash 栈。需要把工作暂时放到一边时，只处理自己的代码路径，例如 `git stash push -u -m "<唯一标签>" -- internal/ cmd/`，或者提交一个临时的 WIP commit。
- **每个 commit 自己保证测试通过**（按批审查后，没有人逐个 commit 替你把关）：
  - `go vet ./...`、`go test ./...` 通过，`gofmt -l .` 输出为空，`go mod tidy` 之后没有改动；
  - 涉及数据库的，`go test -tags integration ./...` 通过；
  - e2e 只跑和这次改动相关的脚本（合入 `e2e` 分支之后）。**全量 e2e 每个审查节点跑一次**；修复 commit 也只跑相关的脚本，tester 在批次上会跑全量，里程碑最后还有一次完整回归（M1 复盘：全量一轮约 20 分钟，每个 commit 都跑，一天等了 217 分钟）；
  - **送审和全量 e2e 同时进行**（用户：提速，但首要保证质量）：上面的检查和相关的 e2e 都通过后就送审，全量 e2e 同时在后台跑。送审消息里写明全量还在跑，并列出预期的失败。跑完只有出现清单外的失败时，才再给 reviewer 发一条，算这一轮的补充，不另开一轮。跑到一半合进了 `e2e` 分支的，不重跑，除非新脚本覆盖的正是这一批；
  - 跑 e2e 用一次阻塞的命令等它结束再读结果，或者放到后台等完成通知，不要 `sleep` 轮询：每次轮询都要把整个上下文重读一遍；
  - 一次跑多个脚本时只编译一次 sqlmux，各脚本共用这个二进制，不要每个脚本各编一份（一份 26MB，全量一轮 1GB）；跑完删掉临时目录里的二进制；
  - 说明用英文、以 feature ID 开头，例如 `F1.6: data pane tabs`。
- **到审查节点**：一条消息把 commit 范围发给 reviewer，逐个 feature 写明希望重点审查的地方。
- **收到退回时**：无论是 reviewer 的「必须改」还是 tester 的 bug，都修复后提交新的 commit，把修复的范围发给 reviewer。
- **建议类意见**：reviewer 给出的「建议」不阻塞提测，可以攒起来，在后续的 commit 中一起处理。
- **里程碑结束**：停下来，等用户深度体验并提出反馈。用户验收通过后才打 tag（`m0`、`m1` ……），然后才开始下一个里程碑的代码。

**reviewer 的规则**：

- **只读**：不改代码、不改 `specs/`、不提交。
  - 在主仓库里只执行只读的 git 命令来看改动，例如 `git -C /Users/ctw/proj/sqlmux log <上一个节点>..<sha>`、`git diff <上一个节点>..<sha>`。
  - 需要跑测试时，在自己的 worktree 里跑：`git worktree add --detach /Users/ctw/proj/sqlmux-review <sha>`，之后用 `git -C /Users/ctw/proj/sqlmux-review checkout --detach <sha>` 切换到要审查的 commit。不在主工作区跑，因为那里有 worker 尚未提交的改动。
- **审查重点**：
  1. **正确性**：逻辑与边界条件是否正确，错误处理是否可能导致数据丢失。
  2. **可维护性与 ponytail**：有没有过度设计、重复造轮子、多余的依赖、死代码；非平凡的逻辑有没有测试。可以用 `code-review` 和 `ponytail:ponytail-review` skill。
  3. **与文档一致**：实现是否符合 tech-design 和 task.md 的开发清单，有没有做清单以外的事。
  4. **本文件的约定**：SQL 相关的实现有没有先参考开源实现；默认键位是否只用了任何终端都能区分的键；界面上的键位文字是否都从 keymap 读取；测试有没有隔离用户数据。
- **意见分两级**：
  - 「必须改」：不改就不能提测；
  - 「建议」：不阻塞提测。
- **spec 本身有问题时**：先发给决策者，等答复之后再把必须改的问题一次退回。不要自己决定，也不要先退回、再补一轮。
- **有「必须改」时**：一条消息发给 worker，写明文件和行号、问题、建议的改法。worker 修复后，只复审新增的改动。
- **审查通过时**：一条消息发给 tester，写明 commit 范围、各 feature 的验收要点、运行方式，以及审查中发现需要 tester 特别关注的地方。不抄送决策者，不通知 worker 改状态。
- **审 pruner 的 commit 时**，重点确认行为没有变：测试的期望值（包括 golden 和 e2e）一个都没改，也没有删掉 spec 要求的东西。

**tester 的规则**：

- **测试请求来自 reviewer**：只测审查通过的批次，测试结论只在审查通过的 sha 上给。发现问题退回给 worker；worker 的修复 commit 会先经过 reviewer，再回到你这里。
- **worker 送审时就开始准备**：收到 worker 的「节点 N 已送审」后，不等 reviewer，先读这批的 spec 和验收项、写脚本、在送审的 sha 上试跑。试跑只跑新写或改过的脚本，不跑全量：这一轮的全量由 worker 在送审时跑（2026-09-29：两边同时跑全量，把机器跑卡了）。发现 spec 没写到或写得有问题的地方，马上问决策者；决策者的答复要改代码时，会转给还在审查的 reviewer，并进它的那一轮退回，不再单独多退一轮（M1 复盘：F1.6–F1.7 的三个 spec 问题在测试时才提，多了一轮 30–40 分钟的退回）。
- **不在主工作区操作**：在单独的 worktree 中测试，用 `git worktree add /Users/ctw/proj/sqlmux-e2e -b e2e` 创建。测试之前，先在 worktree 里执行 `git merge <sha>`。
- **只改 `e2e/` 目录**（e2e 脚本），提交到 `e2e` 分支。worker 会定期把 `e2e` 分支合并进 `main`。
- **测试分三层**：
  1. `go vet ./... && go test ./...`（含 golden）；
  2. 涉及数据库的 feature：按「集成测试环境」连主工作区的 PG，跑 `go test -tags integration ./...`；
  3. 界面行为：用 tmux 做黑盒测试。
- **e2e 只测真实终端才能验证的行为**：终端模式、SGR 鼠标、resize、字素宽度、时序（连按、超时、取消）、配置加载，以及真实数据库上的端到端流程。布局、颜色、位置交给渲染 golden（worker 写）。M0 每次改 UI 都要重写几十条 e2e 断言，F0.13 一次就是 33 条。
  - 已有的 e2e 保留。UI 改动让纯布局的 e2e 断言失效、而 golden 已经覆盖时，删掉这些断言，不重写；golden 没覆盖的，在退回里请 worker 补 golden 场景。
- **跑全量回归时也只编译一次 sqlmux**，各脚本共用；`lib.sh` 里给出跳过编译的开关，跑完删掉临时目录里的二进制和日志（2026-09-29：scratch 里攒了 14 轮、6GB）。
- **tmux 黑盒测试一律通过 `e2e/lib.sh` 进行**：worker 在 main 上、tester 在 e2e worktree 里，会同时跑同一套脚本。
  - `lib.sh` 每次运行都使用独立的 socket `sqlmux-e2e-<pid>`，退出时执行 kill-server 并删除 socket 文件。
  - 不要写死 socket 名，否则一方的 kill-server 或 resize 会打到另一方的会话上；也不要碰用户自己的 tmux。
  - sqlmux 退出后落到的 shell 在临时目录里运行，`HISTFILE=/dev/null`，不要写进用户的 shell 历史。
  - 需要手动操作时，参照 `lib.sh` 的做法：
    - `tmux -L sqlmux-e2e-<唯一后缀> -f /dev/null new-session -d -x 160 -y 45 '<命令>'`
    - 用 `send-keys` 发按键；
    - 用 `send-keys -l $'\e[<0;X;YM\e[<0;X;Ym'` 单击第 X 列、第 Y 行（从 1 开始计）；
    - 用 `capture-pane -p` 读屏幕，需要颜色时加 `-e`。
- **隔离用户数据**：e2e 运行时，把 `XDG_CONFIG_HOME`、`XDG_STATE_HOME`、`XDG_DATA_HOME` 指向临时目录，不要读写用户自己的配置和数据。系统剪贴板也一样：`lib.sh` 在每次运行的临时目录里放假的 `pbcopy` / `pbpaste`（读写这个目录里的一个文件），放在 sqlmux 进程 `PATH` 的最前面，测试一次也不碰用户真实的剪贴板；测 OSC 52 后备时把 `PATH` 设成找不到这些工具（2026-09-29，tester 提议）。
- **报告问题**：一条消息发给 worker，写明 feature ID、复现步骤（脚本或按键序列）、期望结果（引用 tech-design 的章节或 PRD 编号）、实际结果（贴屏幕截取）。
- **测完一批**：一条消息把结论（每个 feature 通过 / 不通过、问题数、e2e 的 sha）同时发给决策者和 worker。

**pruner 的规则**：

- **什么时候干活**：由用户按需启动，不是里程碑的必经步骤。动手提交期间，worker 不写代码，main 只由 pruner 提交。
- **不碰 `e2e/`**：那是 tester 的目录。觉得 e2e 脚本需要精简的，发给 tester。
- **每次新开一个会话**（名为 `sqlmux-pruner`），不需要 worker 交接。
  - 从本文件、`specs/`、代码和 git 历史读起；
  - 范围由用户启动时给出；没有给出时，从上一个 tag 到当前 HEAD。
- **目标**（ponytail，`ponytail:ponytail` skill）：
  - 删掉死代码和没人用的导出；
  - 合并重复的代码，包括测试里重复的辅助函数；
  - 去掉只有一个实现的接口，以及为「以后」预留的参数和配置；
  - 简化过长、过绕的函数；
  - 让包的边界符合 tech-design §4；
  - 处理积压的审查「建议」；
  - 清点 `ponytail:` 注释：已经具备升级条件的提出来，过时的删掉。
- **硬性约束**：
  - **不改行为**：单元测试、golden、e2e 都不改期望值就能通过。要改期望值的，就不算修剪，提给决策者。
  - 不改 `specs/` 和 `AGENTS.md`。发现 spec 本身带来了多余的复杂度，提给决策者。
  - 不加依赖，不加新的抽象，不做没有测量依据的性能优化，也不为了风格偏好大面积改名或搬文件。
- **做法**：
  1. **先只读**：用 `ponytail:ponytail-audit` 扫整个仓库，用 `ponytail:ponytail-debt` 汇总 `ponytail:` 注释；需要时再用 `simplify`、`code-review`。
  2. **列清单发给决策者**：每一项写明位置、要删或要改什么、理由和风险，按收益排序。
  3. **决策者确认范围后再动手**：每一项，或者一组相关的小项，提交一个 commit，说明用英文，以 `prune:` 开头。每个 commit 自己保证全绿。
  4. **全部做完后整批交给 reviewer**，通过后由 tester 在最后一个 commit 上跑完整回归。
- **只修剪一轮**：做的过程中新发现、但不在清单里的问题记下来，留给下一次修剪。

## 其他约定

- **编辑器行为以 nvim 为准**：修改编辑器时，先往 `internal/editor/testdata/cases.txt` 里加用例，再用 `go generate ./internal/editor` 调用 nvim 生成期望结果，最后才改实现（技术方案 §15）。
- **默认键位**：只能用任何终端都能区分的键（技术方案 §6.2），有单测检查。
- **键位文字**：界面上出现的键位文字一律从 keymap 读取，不能写死（技术方案 §6.7）。
