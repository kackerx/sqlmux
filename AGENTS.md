# AGENTS.md

sqlmux：按 tmux 的 session / window / pane 思路组织的终端数据库客户端，技术栈为 Go + Bubble Tea v2。

- 设计与决策以 [`specs/tech-design.md`](specs/tech-design.md) 为准。要改设计，先改这份文档，再写代码。
- 产品需求：[PRD - DB TUI v0.0.2](https://claude.ai/design/p/e6118404-bf7a-4420-abfd-e31198df2ba8?file=PRD+-+DB+TUI+v0.0.2.dc.html)；设计稿：[DB TUI v0.0.2](https://claude.ai/design/p/e6118404-bf7a-4420-abfd-e31198df2ba8?file=DB+TUI+v0.0.2.dc.html)。

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

本项目由三个角色协作开发，feature 清单与验收标准见 [`specs/plan.md`](specs/plan.md)。

| 角色 | 职责 |
|---|---|
| 决策者 | 维护 `specs/tech-design.md`，以及 `specs/plan.md` 中的范围和验收标准；回答问题；确认每个阶段的 feature 拆分 |
| worker | 按 `specs/plan.md` 逐个实现 feature |
| tester | 逐个验证 feature |

**通用规则**：

- 对设计或验收标准有疑问，或者发现 spec 与实际情况冲突时，发消息问决策者，不要自己决定产品行为。决策者改完 spec 会通知你。
- 会话名称可能会变，发消息前先用 ListAgents 确认。

**worker 的规则**：

- 只有 worker 在主工作区（本目录）改代码，并提交到 `main`。只提交到本地，不 push。
- 每完成一个可测试的 feature，依次：
  1. 确认单测通过；
  2. 提交一个 commit，提交说明以 feature ID 开头，例如 `F0.3: 状态栏`；
  3. 把 `plan.md` 里这个 feature 的状态改为 `testing`；
  4. 发消息给 tester，写明 feature ID、commit sha、验收要点和运行方式。
- 不必等测试结果，接着做下一个 feature。收到 bug 报告后修复，提交新的 commit，再通知 tester 复测。
- feature 通过测试后，把状态改为 `passed`。

**tester 的规则**：

- **不在主工作区操作**：在单独的 worktree 中测试，用 `git worktree add /Users/ctw/proj/sqlmux-e2e -b e2e` 创建。测试某个 commit 之前，先在 worktree 里执行 `git merge <sha>`。
- **只改 `e2e/` 目录**（e2e 脚本），提交到 `e2e` 分支。worker 会定期把 `e2e` 分支合并进 `main`。
- **测试分三层**：
  1. `go vet ./... && go test ./...`；
  2. 涉及数据库的 feature：先 `docker compose up -d`，再跑 `go test -tags integration ./...`；
  3. 界面行为：用 tmux 做黑盒测试。
- **tmux 黑盒测试的用法**：必须使用独立的 socket，不要碰用户自己的 tmux。
  - 启动：`tmux -L sqlmux-e2e -f /dev/null new-session -d -s t -x 160 -y 45 '<命令>'`
  - 按键：`send-keys`
  - 鼠标单击第 X 列、第 Y 行（从 1 开始计）：`send-keys -l $'\e[<0;X;YM\e[<0;X;Ym'`
  - 读屏幕：`capture-pane -p`，需要颜色时加 `-e`
  - 结束：`tmux -L sqlmux-e2e kill-server`
- **隔离用户数据**：e2e 运行时，把 `XDG_CONFIG_HOME`、`XDG_STATE_HOME`、`XDG_DATA_HOME` 指向临时目录，不要读写用户自己的配置和数据。
- **报告问题**：发给 worker，写明 feature ID、复现步骤（脚本或按键序列）、期望结果（引用 tech-design 的章节或 PRD 编号）、实际结果（贴屏幕截取）。
- **测完一个 feature**：把结论（通过 / 不通过、问题数）同时发给决策者和 worker。

## 其他约定

- **编辑器行为以 nvim 为准**：修改编辑器时，先往 `internal/editor/testdata/cases.txt` 里加用例，再用 `go generate ./internal/editor` 调用 nvim 生成期望结果，最后才改实现（技术方案 §15）。
- **默认键位**：只能用任何终端都能区分的键（技术方案 §6.2），有单测检查。
- **键位文字**：界面上出现的键位文字一律从 keymap 读取，不能写死（技术方案 §6.7）。
