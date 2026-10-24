# sqlmux 技术方案 · §0 结论速览 / §1 范围 / §2 技术选型 / §3 总体架构 / §4 目录结构

> 这是技术方案的一章，各章的索引和阅读规则在 [`../tech-design.md`](../tech-design.md)；章节编号（§N.M）全仓库通用，不随文件改。

## 0. 结论速览

| 项 | 决定 |
|---|---|
| 语言 / 框架 | Go 1.26 + Bubble Tea v2 + Ultraviolet（已确认）。Lip Gloss v2 原本在列，但最终只用到解析颜色这一处，M0 修剪时去掉了 |
| 渲染 | 立即模式：每帧把画面画进一块 cell 缓冲区，同时生成鼠标命中表 |
| 输入 | 键盘、鼠标、命令面板统一转成 Action。默认键位只用所有终端都能区分的键，不依赖 kitty 键盘协议，也不假设用户在用 tmux |
| 键位模型 | 不做 tmux 式的全局前缀键。NORMAL 模式下用空格（`SPC`）作 leader，按下后弹出可点击的键位提示（已确认） |
| 数据库 | pgx/pgconn、go-sql-driver/mysql；每个连接两条（Meta / Main），用到时才连；所有值按文本处理 |
| SQL 处理 | 自研词法扫描器，负责分句、高亮、读写判定、自动 LIMIT |
| SQL 格式化 | 通过 goja 在进程内运行 sql-formatter（JS 库），已做可行性验证 |
| SQL 编辑器 | 自研 vim 编辑器，覆盖 nvim 的基础操作：文本对象、操作符加移动、大小写转换、注释、撤销与重做、`:s` 替换。不做宏和 `.` 重复。键位映射（如 J → 5j）可以按 pane 类型分别配置。行为用 nvim 做差分测试来校验 |
| 补全与模糊匹配 | query 和 WHERE 输入时提供 fzf 式的模糊补全。全项目的模糊匹配都直接使用 fzf 的匹配算法 |
| 单元格编辑 | 按列属性提供选项：时间分段调整、NULL、DEFAULT、布尔、枚举 |
| WHERE | 不做简写语法，直接输入完整的 SQL 条件表达式 |
| 结果展示 | 每个 window 底部一个全宽结果区，内部用 tab 区分：日志、各 query 的结果、已固定的结果（已确认） |
| 范围 | PostgreSQL、MySQL、SQLite（M14）、Redis（M16，键值引擎走表格 tab 下的接口缝，§5、§8.7） |

## 1. 范围

- 覆盖 PRD 第 5 章全部功能需求（Redis 相关除外），以及第 6、7、8 章的键位、鼠标和视觉规范。键位的调整见 §6.8 和 §18。
- 在 PRD 之外新增：query 内的 SQL 模糊补全（§9.7）。PRD 原本只要求 WHERE 输入框补全字段名。
- 不做：PRD 1.4 列出的非目标；表格多选和批量编辑（PRD 已标为后续版本）。

## 2. 技术选型

### 2.1 选型记录：Go + Bubble Tea v2（已确认）

选型依据：

- **团队熟悉 Go。**
- **框架能力足够。** Bubble Tea v2 与 Ultraviolet（Lip Gloss v2 的底层）原生支持：
  - 按 cell 读写的缓冲区；
  - 带 z 序的图层和点击命中测试；
  - 带坐标的鼠标事件，包括悬停；
  - 约束布局（`ultraviolet/layout`，与 ratatui 的 Layout 同构）。

  版本线为 bubbletea v2.0.9。lipgloss 最初也在依赖里，M0 修剪时去掉了，颜色改用 `ansi.XParseColor` 解析。
- **性能不是瓶颈。** 数据库 TUI 只在有输入或数据回来时才重绘，画一帧 200×60 的界面远不到 1ms。真正影响体验的是数据量，靠两点解决：表格只渲染可见行（§7.6），结果集设行数上限（§11）。
  - **M8 F8.20 的实测修正**（用户 2026-10-08：在表格上拖动指针时卡顿、悬停行的底色跟不上，活动监视器里 sqlmux 44%、tmux 40%、kitty 33%）：悬停靠 1003 模式的全量移动上报，指针一动就是一个事件、一次 Update + View，事件率可以到每秒上百次；一帧的开销乘上这个频率才是真实负担。所以要有 profile 的开关和一个可重复的测量（§15「性能」），按测量结果优化到：160×45 的表格上每秒 100 个移动事件，sqlmux 占不到一个核的 10%，悬停底色不落后于指针。

Go 生态相对 Rust 缺三样东西，对策如下：

| 缺口 | 对策 |
|---|---|
| vim 编辑器组件 | 自研，覆盖 nvim 的基础操作（§11）。用 nvim headless 生成期望结果，做差分测试（§15） |
| SQL 词法分析 | 自研扫描器，约 300 行，配表驱动测试（§9.1） |
| SQL 格式化 | 在进程内运行成熟的 JS 库 sql-formatter，已验证可行（§9.5） |

### 2.2 limoni 评估：不采用

[thebanri/limoni](https://github.com/thebanri/limoni) 主打零分配渲染、3D、图表，以及可被 AI 驱动的语义树，和本项目的需求重合不多。关键问题是还不成熟：

- 仓库创建于 2026-08-06，写本文时约 7 周，55 个 star，205 次提交里 194 次来自同一位作者，版本 v0.8.1。
- 未关闭或刚修复的问题，恰好落在本项目依赖的交互上：
  - #48：指针移动被当成 `MousePressMsg`，悬停无法实现；
  - #49：`MouseWheelMsg` 不带坐标；
  - #19：没有可拖动分隔线的 SplitPane；
  - #18：没有 StatusBar；
  - #47：点击事件不会路由到控件，2026-09-21 才修复；
  - #53：输入事件循环还没在真实 PTY 上测试过。

## 3. 总体架构

```
 KeyPressMsg ─► keymap 解析 ──┐
 Mouse*Msg ──► 命中表查找 ────┼─► Action ─► Update（单线程）─► 状态树
 命令面板选中 ────────────────┘                   │   ▲
                                                 ▼   │ resultMsg{tab, seq}
                                             tea.Cmd ─► db.Worker（每条连接一个，互斥锁串行）

 View(): 状态树 ─► Frame（cell 缓冲区 + 命中表）─► Bubble Tea 差分输出
```

四条原则：

1. **只有一棵状态树，只在 Update 里修改。** IO 放在 `tea.Cmd` 里执行，结果以 Msg 的形式回到 Update。
2. **所有操作都是 Action。** 键位、鼠标点击、命令面板最终都落到同一个 Action 上，满足 PRD 第 2、7 章「鼠标和键盘不存在两套逻辑」的要求。
3. **模式由状态推导，不单独存储。** 编辑单元格或输入框获得焦点即为 INSERT；query 的模式就是编辑器自身的模式；命令面板打开即为 COMMAND（B-02）。
4. **视图是状态的纯函数。** 命中表是渲染的副产品，每帧重建。

## 4. 目录结构

```
sqlmux/
├─ AGENTS.md、CLAUDE.md       给 agent 的项目说明；CLAUDE.md 只有一行 @AGENTS.md
├─ .claude/rules/             按路径触发的规则：sql.md（开源参考表）、e2e.md（tmux 黑盒）、editor.md（nvim 差分）、keys.md（键位约定）
├─ README.md、LICENSE、assets/  给 GitHub 访客的说明、许可证（MIT）和截图，决策者维护
├─ cmd/sqlmux/main.go         参数解析、子命令（keys）、启动
├─ internal/
│  ├─ app/                    Model / Update / View、Action 注册表、工作现场模型、布局树
│  ├─ ui/                     Frame、Block、主题、命中表、模糊匹配（fzf 算法的封装）、各组件绘制
│  ├─ keymap/                 键位记法解析（含 keymaps.toml 的键位语法 ParseKeys，M13 F13.15 起）、作用域 trie、次数前缀、用户映射、冲突检测、提示查询、default.toml（embed）；不 import config / ui
│  ├─ editor/                 vim 编辑器的纯逻辑，不 import ui 和 bubbletea；testdata/ 放 nvim 差分用例
│  ├─ sqlkit/                 词法扫描、分句、读写判定、自动 LIMIT、WHERE 拼接校验、补全上下文、格式化（内嵌 sql-formatter）
│  ├─ db/                     Conn / Engine / Source 接口、Worker、postgres/、mysql/、sqlite/（M14 F14.1，modernc.org/sqlite，纯 Go）、mssql/（M18 F18.6，microsoft/go-mssqldb，纯 Go）、redis/（M16，go-redis v9：连接、SCAN 树、键 tab 的数据源、控制台的命令词法与 COMMAND DOCS 补全）、exec/（M17 F17.7：外部执行器，子进程当连接，§8.8）、catalog 查询
│  ├─ config/                 config / connections / state / 布局文件的读写与 XDG 路径
│  └─ i18n/                   界面文案的翻译：T(中文, 参数…)，en 表
├─ e2e/                       tester 的黑盒测试脚本，从 e2e 分支合并进来
├─ testdata/seed/             集成测试的种子数据
├─ docker-compose.yml         集成测试用的 postgres 与 mysql
├─ specs/
│  ├─ tech-design.md          技术方案的索引和写法规则
│  ├─ design/NN-*.md          技术方案各章（§0–§4 在 00-overview.md，之后一章一个文件）
│  ├─ roles/*.md              五个角色各自的规则
│  ├─ plan.md                 里程碑索引、任务文件的格式与状态
│  └─ m<N>-<名字>/task.md     各里程碑的任务清单
├─ justfile                   常规操作的入口
├─ .golangci.yml              golangci-lint v2 的规则
├─ .github/workflows/ci.yml   GitHub Actions：lint、vet、单测、集成测试
├─ .github/workflows/release.yml  打 v* tag 时发布（M11）
├─ .github/scripts/            release workflow 用的脚本：tap-formula.sh 每个 tag 整份生成 Homebrew formula（M11）
├─ .goreleaser.yaml           交叉编译、压缩包、校验和、Release（M11）
├─ npm/                       npm 壳包和平台子包的模板（M11）
├─ bin/                       构建产物，不入库
└─ .handoff/                  会话交接文档，不入库
```

依赖：

- 界面：`charm.land/bubbletea/v2`、`github.com/charmbracelet/ultraviolet`，以及 `github.com/charmbracelet/x/ansi`（宽度计算、颜色解析）。不用 bubbles 的 textinput：它按 rune 删字，退格会把 é、👍🏽 这类字素簇拆开（M1 核对时实测）。单行输入框在 F0.4 命令行的输入处理上扩展。
- 数据库驱动：`github.com/jackc/pgx/v5`、`github.com/go-sql-driver/mysql`、`modernc.org/sqlite`、`github.com/redis/go-redis/v9`（M16，BSD-2）
- 格式化：`github.com/dop251/goja`
- 模糊匹配：`github.com/junegunn/fzf/src/algo`
- 其他：`github.com/BurntSushi/toml`
- 命令行参数：用标准库 `flag`。目前只有 `keys` 一个子命令，加上 `--debug`、`--format`、`--check` 几个参数，不需要引入 kong 这类 CLI 库；等子命令多起来、或者需要 shell 补全时再换。
