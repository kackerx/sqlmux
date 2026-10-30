# M19 体验回合 · 任务清单

- **状态**：**todo**（2026-10-30 决策者起草；用户 2026-10-30 提五条，作为 v0.8.0 发布）。
- **目标**：用户用 v0.7.0 时的五条意见：命令行直接开一张表（PR #6 的想法收窄）、执行中的转圈、pane 右上角的四个按钮、不满一页时行数常驻、`tree.toggle` 照 DataGrip 带焦点。外加 v0.7.0 安装验证时记下的包描述（随下个版本改）。
- **范围**：§14「启动」（`--table` / `--where`）、§7.8「转圈」「pane 标题」「查询条」「schema 侧栏」、§7.7 图标、§8.3、§11「执行中」。都已写进各章（2026-10-30）。
- **依赖**：M18 完成（v0.7.0）。
- **完成标准**：两个节点全过；完整回归绿；用户体验验收后 worker 打 `m19` 和 `v0.8.0`，按 AGENTS.md 的两步推送（`release/v0.8.0` 分支开 PR、用户合并、在合入的 commit 上打 tag 推送）。
- **审查节点**：1 = F19.1、F19.5、F19.6（启动参数、侧栏的焦点、包描述）；2 = F19.2 + F19.4（转圈、行数常驻，都动查询条的行数统计，放一个 worker）；3 = F19.3（pane 按钮），**由 `sqlmux-fullstack-dev-2` 在 `/Users/ctw/proj/sqlmux-dev2` 的分支 `f19.3` 上并行做**（AGENTS.md「并行开发」，用户 2026-10-30 定），从 87741d5 拉。F19.3 和其余几条的边界：它只动 pane 标题的提示和命中区、图标表；转圈的帧不放进图标表（F19.2 自己一处），F19.5 的侧栏标题 `«` / `»` 归 F19.5；两边都会改 golden，合入时按 AGENTS.md 的办法重新生成。
- **不做（留候选）**：PR #6 的其余九个参数（理由见 §14）；转圈的开关和帧的配置；pane 按钮的悬停说明文字；结果 tab 和 Redis 键 tab 的行数常驻（这次只改表 tab 的查询条，结果 tab 的标题本来就写行数）；scoop / winget、exec 批多条语句、`{db}`、Redis 第二轮、JSON 按格高亮、选中行整行着色（plan.md 的候选，照旧）。

---

## F19.1 命令行直接开一张表：`--table` / `--where` · 状态：passed（d4cc68a；e2e 56d00dc）

- **依赖**：M14 F14.1（命令行给库）、M6 F6.5（启动和布局）
- **涉及**：`cmd/sqlmux`、`internal/app`（启动后开表）、`internal/i18n`
- 外部 PR #6（MrLiuX）提的 `--table` 加十个参数。用户 2026-10-30：临时看一张表很有用，不可能每张表存一个布局。决策者收窄成两个参数，由我们按流程做，PR #6 致谢后关闭（评论由用户发）。PR #6 自己的问题：参数写在位置参数后面解析不了（Go 的 `flag` 停在第一个位置参数），文档里的示例跑不通。

**开发**（§14「启动」）
- [ ] a. 解析：`--table`、`--where` 和已有的 `--layout`、`--version` 放在位置参数前后都认；位置参数仍然最多一个。usage 行补上两个参数。不手写一个参数解析器：先 `flag.Parse`，再对剩下的参数里第一个位置参数之后的部分再 `Parse` 一次，或者等价的最少写法。
- [ ] b. 不给连接用 connections.toml 的第一个连接，一个连接都没有时英文报错退出（§14）；给了连接名、文件、URL 照现在的规则。usage 错误（`--where` 没有 `--table`、`--table` 加 `--layout`、Redis 加 `--where`）在 `config.Load` 之前或刚读完连接时英文报错退出。Redis 要读完连接才知道引擎，放在读连接之后。
- [ ] c. 开表：默认布局，连上、catalog 到了以后把表开成 data pane 的聚焦 tab（同树上 `↵` 开表，走同一个函数），`--where` 预置成这个 tab 的 WHERE、开表时带着它只取一次（同恢复布局时预置的做法，不先不带条件取一遍；reviewer 2026-10-30 问，决策者定），并用手输执行记历史的那个函数记进 WHERE 历史；不读 `last.toml`；schema 缺省用连接的当前 schema；catalog 里没有这张表 toast「<连接名>：没有表 <名字>」（i18n）。Redis 开键 tab。连不上照现在启动连接失败的处理。
- [ ] d. 退出不写 `last.toml`（同临时连接，`SaveLayoutOnExit` 不调）。

**验收**
- [ ] 单测：参数解析的表驱动（`conn --table t`、`--table t conn`、`--table t --where x conn`、只有 `--table t`、`--where` 单独、`--table` 加 `--layout`），走 `run` 或它解析参数的那一段；schema 缺省和精确匹配；mutation。
- [ ] e2e：`sqlmux play --table t_order --where "id < 11"` 开出 t_order、WHERE 已执行、10 行；`sqlmux --table t_order` 用第一个连接；没有的表 toast；Redis `--table hash:user` 开键 tab；退出后 last.toml 没变（mtime）。

## F19.2 执行中的转圈 · 状态：passed（d4cc68a；e2e 56d00dc）

- **依赖**：§8.3 的 busy、§11 的占位
- **涉及**：`internal/app`、`internal/ui`（statusline、querybar、树）
- 用户 2026-10-30：参考 lazygit 推送时的 `正在推送 •●•`，执行中要有动画。

**开发**（§7.8「转圈」）
- [ ] a. 帧和 ascii 帧照 §7.8；一个共用的帧函数，四处都用它。
- [ ] b. 四处：状态栏 busy 块、结果 tab 的执行中占位、查询条计数完成前的 `…`、树上连接中的 `…`。
- [ ] c. 一个 tick：有转圈在屏幕上时每 150ms 一次，没有就不发。结果占位原来每秒一次的已用时 tick 能并进这个就并（不多一个定时器），并不进说明原因。

**验收**
- [ ] 单测：帧序列和 ascii 帧；没有在途请求时不产生 tick（Update 返回的 Cmd 里没有）；有请求时帧号前进；mutation。
- [ ] golden：busy 块、执行中占位、计数中的查询条各第一帧。
- [ ] e2e：`select pg_sleep(2)` 执行中状态栏和结果占位里出现三点之一的帧（正则匹配四帧），两次截屏帧不同；执行完三点消失；空闲 2 秒内两次截屏一样（没在重画）。

## F19.3 pane 右上角的四个按钮 · 负责：dev-2 · 状态：passed（b0d2cd7，分支 f19.3；e2e 204c2b2）

- **依赖**：M9 F9.6（缩放的样子）
- **涉及**：`internal/app`（view、命中区）、`internal/ui`（icons、frame 的标题提示）
- 用户 2026-10-30：参考 Warp，每个 pane 右上角四个图标：往右新建、往下新建、全屏当前 pane、关闭 pane。

**开发**（§7.8「pane 标题」、§7.7）
- [ ] a. 图标 `split_right`、`split_below`、`unzoom` 加进图标表和主题可覆盖的名字，ascii 照 §7.7；两个分割图标在终端里对照字形确认方向。
- [ ] b. 标题提示的最后一项，走已有的「按优先级摆提示」那一处，不另写摆法；空 pane 也画。
- [ ] c. 命中区：点哪个 pane 的按钮作用于那个 pane（先聚焦再执行），Action 就是现有的四个。
- [ ] d. 发版前 oss 在 `docs/config.md` 的图标名清单补 `split_right`、`split_below`、`unzoom`（dev-2 2026-10-30 提，oss 的文件，worker 不碰）。

**验收**
- [ ] 单测：四个按钮的命中区各自执行对应 Action、作用于被点的 pane 而不是焦点 pane；窄 pane 整组不画；空 pane 有；缩放中画 `unzoom`；mutation。
- [ ] golden：默认布局的 data / query 标题、空 pane、缩放中的 pane、ascii 图标。
- [ ] e2e：点 ① 的往右分割出现新 pane；点缩放、再点还原；点关闭关掉；点不在焦点的 pane 的关闭，关的是它。

## F19.4 结果不满一页时行数常驻 · 状态：passed（d4cc68a；e2e 56d00dc）

- **依赖**：M1 F1.4（查询条）、M10 F10.7（让位顺序）
- **涉及**：`internal/app`（grid 的查询条内容）、`internal/ui`（querybar 的让位）
- 用户 2026-10-30（截图：两个并排的表 tab，左边画着 `auto · 1 rows · 73ms`，右边窄一点就没了；参考 DataGrip 右下角的「157 行」）：结果不满一页时要固定显示条数。

**开发**（§7.8「查询条」）
- [ ] a. 第 1 页行数少于 LIMIT 时，第一页回来就把计数记成这一页的行数、算作计数完成，不发 COUNT（PAGE 立刻 `1/1`，查询条不另走分支）；放不下全文时缩成 `N 行`，让位排到最后。改在 querybar 已有的让位那一处。

**验收**
- [ ] 单测：不满一页、窄到全文放不下时画 `N 行`；不满一页时不发 COUNT、PAGE 是 `1/1`；满一页的照旧发 COUNT、第一个让位；mutation。
- [ ] golden：63 列默认布局里一张 3 行的表、一张 1203 行的表，转置视图各一张。
- [ ] e2e：默认布局 WHERE `id < 4` 后查询条有 `3 行`；清掉 WHERE（满一页）后照旧。

## F19.5 `tree.toggle` 照 DataGrip 带焦点 · 状态：passed（d4cc68a；e2e 56d00dc）

- **依赖**：M10 F10.9（侧栏的收起）
- **涉及**：`internal/app`（action、focus）、`internal/i18n`、`internal/app/tips.go`
- 用户 2026-10-30：现在 `Cmd+B`（`tree.toggle`）只开关侧栏不动焦点。要：开着且焦点在树上 → 收起并回到之前的 pane；开着焦点不在树上 → 聚焦树；收起时 → 展开并聚焦树。

**开发**（§7.8「schema 侧栏」）
- [ ] a. `tree.toggle` 按三种情况；进树时记下来的 pane，关了就回 window 的当前 pane；缩放中先退出缩放。
- [ ] b. 侧栏标题的 `«` / 折叠栏的 `»` 不再等于 `tree.toggle`：`«` 收起（焦点在树上时回到之前的 pane），`»` 展开并聚焦树。
- [ ] c. Action 标题「打开 / 聚焦 / 收起侧栏」，轮播提示的文字跟着改（i18n 两种语言）。

**验收**
- [ ] 单测：三种情况各一条（侧栏状态和焦点），回到的是进树前的 pane 而不是第一个 pane；那个 pane 关了以后的退路；缩放中；点 `«` / `»`；mutation。
- [ ] e2e：焦点在 ②，`SPC b` 焦点到树、侧栏还在；再按收起、焦点回 ②；收起时按展开并在树上（`j` 移动的是树的光标）。

## F19.6 brew / npm 的包描述加上 SQL Server · 状态：passed（d4cc68a；e2e 56d00dc）

- **依赖**：M18（SQL Server）
- **涉及**：`.github/scripts/tap-formula.sh`、`npm/sqlmux/package.json`
- oss 2026-10-26 v0.7.0 安装验证时记下的，「随下个版本改」（plan.md「M18 候选」）。

**开发**
- [ ] a. 两处描述改成 `… for PostgreSQL, MySQL, SQLite, SQL Server and Redis`；顺手 grep 别处有没有同一句。

**验收**
- [ ] 审查看 diff；f11.3 的 e2e 里 package.json 描述那一条跟着改。
