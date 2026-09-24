# AGENTS.md

sqlmux：按 tmux 的 session / window / pane 思路组织的终端数据库客户端，技术栈为 Go + Bubble Tea v2。

- 设计与决策以 [`specs/tech-design.md`](specs/tech-design.md) 为准。要改设计，先改这份文档，再写代码。
- 产品需求：[PRD - DB TUI v0.0.2](https://claude.ai/design/p/e6118404-bf7a-4420-abfd-e31198df2ba8?file=PRD+-+DB+TUI+v0.0.2.dc.html)；设计稿：[DB TUI v0.0.2](https://claude.ai/design/p/e6118404-bf7a-4420-abfd-e31198df2ba8?file=DB+TUI+v0.0.2.dc.html)。

## 目录结构

以 tech-design §4 为准。增删目录属于设计变更，由决策者同时更新 §4 和本节。

```
sqlmux/
├─ AGENTS.md、CLAUDE.md       本文件；CLAUDE.md 只有一行 @AGENTS.md
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

本项目由五个角色协作开发。里程碑的索引和推进规则见 [`specs/plan.md`](specs/plan.md)；每个里程碑的任务清单在对应的目录下，例如 [`specs/m0-skeleton/task.md`](specs/m0-skeleton/task.md)。

| 角色 | 职责 |
|---|---|
| 决策者 | 维护 `specs/` 下的文档：tech-design、plan，以及各 task.md 的范围和验收标准；回答问题；确认每个里程碑的开发清单；里程碑结束时整理体验路径，交给用户验收 |
| worker | 按 task.md 的开发清单逐项实现 |
| reviewer | 审查 worker 的每个 commit，通过后提测给 tester |
| tester | 按 task.md 的验收项逐个测试 feature |
| pruner（修剪者） | 每个里程碑的 feature 和改进项全部通过后，按 ponytail 原则，对整个仓库做一次不改变行为的精简；每个里程碑新开一个会话 |

固定的工作流：

1. worker 完成一个 feature 后提交，把 commit 交给 reviewer。
2. reviewer 审查。
   - 通过：提测给 tester。
   - 有必须修改的问题：退回给 worker。worker 修复后，再次交给 reviewer。
3. tester 测试。
   - 通过：状态改为 `passed`，报告给决策者和 worker。
   - 有问题：退回给 worker。worker 的修复 commit 同样要先经过 reviewer 审查，再回到 tester。

用户按里程碑验收，不逐个验收 feature。一个里程碑的所有 feature 都 `passed`、完整的 e2e 回归也通过之后，进入**暂停**：

- 用户深度体验这个里程碑，提出问题和改进意见；
- 决策者把反馈整理成改进 feature，照常走完上面的流程；
- 改进项全部通过后，由 pruner 修剪一轮（见下文「pruner 的规则」），tester 在最后一个 commit 上跑完整回归；
- 用户确认验收通过后，打 tag，进入下一个里程碑。

暂停期间，worker 不写代码，只可以起草下一个里程碑的开发清单。修剪期间，main 只由 pruner 提交。

**通用规则**：

- 对设计或验收标准有疑问，或者发现 spec 与实际情况冲突时，发消息问决策者，不要自己决定产品行为。决策者改完 spec 会通知你。
- 会话名称可能会变，发消息前先用 ListAgents 确认。
- **commit 说明一律用英文**，标题和正文都是，`F0.x:`、`docs:`、`e2e:` 这些前缀照旧。
- **作者身份**由仓库的 git 配置决定（kackerx），不要改 `user.name`、`user.email`。仓库配置了 `origin`（`kackerx/sqlmux`），但仍然只提交到本地，不 push。

**worker 的规则**：

- **代码质量**：代码首先要好维护，并遵循 ponytail 原则（`ponytail:ponytail` skill）。
  - 能不写的就不写；已有的代码优先复用；然后依次考虑标准库、已经引入的依赖，最后才写最少的新代码。
  - 不做只有一个实现的接口，不做只有一种产品的工厂，不为不会变的值加配置，不为「以后可能用到」预先搭脚手架。
  - 改 bug 要找到根因，改在所有调用方共用的那一处。
  - 有意走的捷径，用 `ponytail:` 注释标出它的上限，以及以后怎么升级。
  - 有分支、循环、解析等非平凡逻辑的地方，至少留一个能运行的测试。
  - 包的边界按 tech-design §4 划分。命名要清楚。注释写「为什么」，不写「做了什么」。不留死代码。
- **按清单逐步做**：
  - 严格按 task.md 的开发清单逐项实现并勾选，不跳步，不顺手做清单以外的事。
  - 发现清单漏了东西或者不合理，先提给决策者。
- **提交**：
  - 只有 worker 在主工作区（本目录）改代码，并提交到 `main`；修剪期间例外，由 pruner 提交。只提交到本地，不 push。
  - feature commit 里不包含 `specs/` 和 `AGENTS.md` 的改动（自己改的任务状态除外）。决策者通知文档有更新时，单独提交一个 `docs:` commit。
  - 不要对整个工作区执行 stash、checkout 或 reset。决策者可能正在修改 `specs/` 和 `AGENTS.md`，所有 worktree 也共用同一个 stash 栈。需要把工作暂时放到一边时，只处理自己的代码路径，例如 `git stash push -u -m "<唯一标签>" -- internal/ cmd/`，或者提交一个临时的 WIP commit。
- **每完成一个可测试的 feature**，依次：
  1. 确认所有测试通过：单测、golden，以及合入 `e2e` 分支后的 e2e 回归；
  2. 提交 commit，说明用英文、以 feature ID 开头，例如 `F0.3: keymap engine and config`；
  3. 把 task.md 里这个 feature 的状态改为 `reviewing`；
  4. 发消息给 reviewer，写明 feature ID、commit sha、对应的 task.md 小节，以及希望重点审查的地方。
- **不等审查和测试的结果**，接着做下一个 feature。
- **收到退回时**：无论是 reviewer 的「必须改」还是 tester 的 bug，都修复后提交新的 commit，再交给 reviewer。
- **状态更新**：
  - reviewer 告知已提测后，把状态改为 `testing`；
  - tester 报告通过后，勾选验收项，把状态改为 `passed`，并注明 commit sha。
- **建议类意见**：reviewer 给出的「建议」不阻塞提测，可以攒起来，在后续的 commit 中一起处理。
- **里程碑结束**：全部 feature 都 `passed`、tester 的完整回归也通过之后，停下来，等用户深度体验并提出反馈。用户验收通过后才打 tag（`m0`、`m1` ……），然后才开始下一个里程碑的代码。

**reviewer 的规则**：

- **只读**：不改代码、不改 `specs/`、不提交。
  - 在主仓库里只执行只读的 git 命令来看改动，例如 `git -C /Users/ctw/proj/sqlmux show <sha>`，或 `git diff <上次审查通过的 sha>..<sha>`。
  - 需要跑测试时，在自己的 worktree 里跑：`git worktree add --detach /Users/ctw/proj/sqlmux-review <sha>`，之后用 `git -C /Users/ctw/proj/sqlmux-review checkout --detach <sha>` 切换到要审查的 commit。不在主工作区跑，因为那里有 worker 尚未提交的改动。
- **审查重点**：
  1. **正确性**：逻辑与边界条件是否正确，错误处理是否可能导致数据丢失。
  2. **可维护性与 ponytail**：有没有过度设计、重复造轮子、多余的依赖、死代码；非平凡的逻辑有没有测试。可以用 `code-review` 和 `ponytail:ponytail-review` skill。
  3. **与文档一致**：实现是否符合 tech-design 和 task.md 的开发清单，有没有做清单以外的事。
  4. **本文件的约定**：SQL 相关的实现有没有先参考开源实现；默认键位是否只用了任何终端都能区分的键；界面上的键位文字是否都从 keymap 读取；测试有没有隔离用户数据。
- **意见分两级**：
  - 「必须改」：不改就不能提测；
  - 「建议」：不阻塞提测。
- **有「必须改」时**：发给 worker，写明文件和行号、问题、建议的改法。worker 修复后会提交新的 commit，只复审新增的改动。
- **审查通过时**：
  - 发消息给 tester 提测，写明 feature ID、commit sha、验收要点、运行方式，以及审查中发现需要 tester 特别关注的地方；
  - 同时告知 worker，由 worker 把状态改为 `testing`；
  - 抄送决策者一句话结论。
- **spec 本身有问题时**：发给决策者，不要自己决定。
- **审 pruner 的 commit 时**，重点确认行为没有变：测试的期望值（包括 golden 和 e2e）一个都没改，也没有删掉 spec 要求的东西。

**tester 的规则**：

- **测试请求来自 reviewer**：只测审查通过的 commit。发现问题退回给 worker；worker 的修复 commit 会先经过 reviewer，再回到你这里。
- **不在主工作区操作**：在单独的 worktree 中测试，用 `git worktree add /Users/ctw/proj/sqlmux-e2e -b e2e` 创建。测试某个 commit 之前，先在 worktree 里执行 `git merge <sha>`。
- **只改 `e2e/` 目录**（e2e 脚本），提交到 `e2e` 分支。worker 会定期把 `e2e` 分支合并进 `main`。
- **测试分三层**：
  1. `go vet ./... && go test ./...`；
  2. 涉及数据库的 feature：先 `docker compose up -d`，再跑 `go test -tags integration ./...`；
  3. 界面行为：用 tmux 做黑盒测试。
- **tmux 黑盒测试一律通过 `e2e/lib.sh` 进行**：worker 在 main 上、tester 在 e2e worktree 里，会同时跑同一套脚本。
  - `lib.sh` 每次运行都使用独立的 socket `sqlmux-e2e-<pid>`，退出时执行 kill-server 并删除 socket 文件。
  - 不要写死 socket 名，否则一方的 kill-server 或 resize 会打到另一方的会话上；也不要碰用户自己的 tmux。
  - 需要手动操作时，参照 `lib.sh` 的做法：
    - `tmux -L sqlmux-e2e-<唯一后缀> -f /dev/null new-session -d -x 160 -y 45 '<命令>'`
    - 用 `send-keys` 发按键；
    - 用 `send-keys -l $'\e[<0;X;YM\e[<0;X;Ym'` 单击第 X 列、第 Y 行（从 1 开始计）；
    - 用 `capture-pane -p` 读屏幕，需要颜色时加 `-e`。
- **隔离用户数据**：e2e 运行时，把 `XDG_CONFIG_HOME`、`XDG_STATE_HOME`、`XDG_DATA_HOME` 指向临时目录，不要读写用户自己的配置和数据。
- **报告问题**：发给 worker，写明 feature ID、复现步骤（脚本或按键序列）、期望结果（引用 tech-design 的章节或 PRD 编号）、实际结果（贴屏幕截取）。
- **测完一个 feature**：把结论（通过 / 不通过、问题数）同时发给决策者和 worker。

**pruner 的规则**：

- **什么时候干活**：第 1 步的只读审计可以在最后几个 feature 测试期间提前开始；动手提交要等里程碑的 feature 和改进项全部 `passed` 之后、最终回归之前。这段时间 worker 不写代码，main 只由 pruner 提交。
- **不碰 `e2e/`**：那是 tester 的目录。觉得 e2e 脚本需要精简的，发给 tester。
- **每个里程碑新开一个会话**（名为 `sqlmux-pruner`），不需要 worker 交接。
  - 从本文件、`specs/`、代码和 git 历史读起；
  - 决策者会在开工消息里给出这个里程碑的起点 commit，以及还没处理的审查「建议」。
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
  3. **决策者确认范围后再动手**：每一项，或者一组相关的小项，提交一个 commit，说明用英文，以 `prune:` 开头。
  4. **每个 commit 之前**：跑全部单测、golden 和 e2e 回归。
  5. **每个 commit 交给 reviewer 审查**。全部做完后，tester 在最后一个 commit 上跑完整回归，结论发给决策者。
- **只修剪一轮**：做的过程中新发现、但不在清单里的问题记下来，留给下一个里程碑的修剪。

## 其他约定

- **编辑器行为以 nvim 为准**：修改编辑器时，先往 `internal/editor/testdata/cases.txt` 里加用例，再用 `go generate ./internal/editor` 调用 nvim 生成期望结果，最后才改实现（技术方案 §15）。
- **默认键位**：只能用任何终端都能区分的键（技术方案 §6.2），有单测检查。
- **键位文字**：界面上出现的键位文字一律从 keymap 读取，不能写死（技术方案 §6.7）。
