# M21 命令行与外层多路复用器 · 任务清单

- **状态**：**两个节点全过，等用户验收**（2026-11-08；验收版在 /root/proj/sqlmux-verify，WALKTHROUGH.md 在那里）。（2026-11-07 决策者起草；用户 2026-11-07 定 v0.9.0 发出后直接进 M21。从这个里程碑起整套会话在用户 Windows 主机的 WSL 上跑，路径见 AGENTS.md）。
- **目标**：命令行能指定默认连接、直接跑一条语句；Redis 键名索引存盘、启动即用；聚焦和未聚焦的边框能用不同线型；在 tmux 里 `C-h/j/k/l` 能越过 sqlmux 的边界；测试残留库自动清掉。发 v0.10.0。
- **范围**：§14「启动」（`default_connection`、`--sql`、`--schema`）和 config.toml 样例、§9.8「索引存盘」、§7.3 / §14 的 `border_focus_style`、§6 的 `pane.focus.*` 越界、§15「集成测试」的残留库清理。都已写进各章（2026-11-07，docs commit 见 git log）。
- **依赖**：M20 完成（v0.9.0）。
- **完成标准**：两个节点全过；完整回归绿（清单外 0）；Windows 版冒烟（`GOOS=windows` 编 exe，主机 python 跑 `e2e/windows/smoke.py`，tester 在 WSL 里跑）绿；用户体验验收后 worker 打 `m21` 和 `v0.10.0`，照 AGENTS.md 两步推送。
- **审查节点**：1 = F21.1 + F21.2 + F21.3；2 = F21.4 + F21.5 + F21.6；3 = F21.7（用户 2026-11-08 验收时提）。 节点 1 过（9da01dd；e2e c311cad，2026-11-08，0 个问题）。tester 的观察（不改）：`--sql` 和 `--where` 同时给报的是「--where needs --table」，不是互斥句子，行为对。全量 e2e 串行 3 小时以上，6 路并行约 50 分钟，并行时 f0.1、f17.13、f20.4 偶尔红、串行绿。 节点 2 过（ef1f344；e2e 7766a7e，2026-11-08，0 个问题；完整回归 283 个脚本清单外 0；F21.4 的启动即用另有 f21.4b：重启后旧键马上可补、重建完换新、flushdb 后文件清空）。没测：F21.6 同一个 tmux server 挂两个 client、SQL Server。都是 worker 在 `main` 上做。F21.6 排最后，节点 2 做不下就丢（用户 2026-10-31）。
- **不做（留候选）**：`--sql` 的非交互模式（结果打到 stdout，那是 psql `-c` 的活）；SQL catalog 存盘；WezTerm / Zellij 的越界切换（有人要再加）；连接改名后迁索引文件；每条连接上的 `default = true`。

---

## F21.0 e2e 和 `just check` 在 WSL 上跑 · 状态：passed（eae2a01；e2e c311cad，tester 2026-11-08：a–c、f 做完，另修了清单外的十几处 GNU / tmux 3.8 / locale 差异，全量清单外 0） · 负责：tester（`e2e/`，e2e 分支）+ worker（`justfile` 一行）

- worker 2026-11-07：节点 1 全量 e2e 36 / 280 红，大头是 Mac 和 WSL 的差异，不是回归。决策者定（2026-11-08），不等节点 1：
- [ ] a.（tester）BSD 命令：`stat -f %Lp / %m / %Fm / %z`（19 处）、`md5`（WSL 只有 `md5sum`）。在 `e2e/lib.sh` 加可移植的小函数（`fmode` / `fmtime` / `fsize` / `md5`，按 `uname` 或按命令存在与否选 GNU / BSD），17 个脚本改过去。不在 PATH 里放 shim（脚本看不出依赖）。
- [ ] b.（tester）root 跑：`chmod 444 / 000`、只读目录挡不住 root，f8.16*、f8.19*、f8.21、f7.4* 的「写不进」「读不了」断言在 root 下 skip（lib.sh 一个 `e2e_skip_if_root`，打印原因），同单测 c23a469 按 Geteuid 跳过。这些路径 Mac 上已过、M21 不改，root 下不覆盖是已知上限，写进 §15。
- [ ] c.（tester）网络：WSL mirrored 模式下连 127.0.0.1 的关闭端口是超时不是 refused，f1.1、f8.16（C-t 端口 1）、f12.5、f16.1、f18.13 里断言 `connection refused` 的改成只断言「出错、错误行里有地址」，不认死文案。
- [ ] d.（worker，随节点 1）工具链：brew 的 go 是 1.27.3，gofmt 和 CI 的 1.26.3 不同、golangci-lint 要用同版 Go 编。`justfile` 顶部 `export GOTOOLCHAIN := "go" + \`sed -n 's/^go //p' go.mod\``，所有 `just` 入口都用 go.mod 那个版本（第一次自动下载），`lint` 的安装提示照样走它；不在 AGENTS.md 里写 PATH。
- [ ] e. 清单外（不改）：SQL Server 容器没起（f18.7*）、ruby / brew audit 那条是 macOS 才有的；goreleaser worker 自己 `brew install`。
- [ ] f. 性能脚本 f11.x：等 worker 在 79e6ca3 干净副本上的对照结果；对照也红就是机器差异，tester 把门槛按这台机器放宽一档（同 M17 f13.2 的做法），对照绿才算回归。

**验收**
- [ ] 全量 e2e 在 WSL 上清单外 0（上面 e 列的除外）；`just check` 在 WSL 上不带任何 PATH 前置全绿，`gofmt -l` 为空。

## F21.1 测试残留库的自动清理 · 状态：passed（9da01dd；e2e c311cad）

- **涉及**：`justfile`（新入口 `db-clean`，`e2e` 全量开跑前调）、`internal/db/postgres/testdsn.go` / `mysql/testdsn.go`（只看命名，不改）
- 决策者 2026-11-02：同一类问题第二次出现。M20 节点 6 全量里 f3.11 红 2 条，是 worker 的集成测试崩掉后留在 PG 上的 `sqlmux_worker_38735_…` 库名太长、撑宽了 `gs` 下拉框；之前 MySQL 上也有共享实例里的测试库干扰计数。

**开发**（§15「集成测试」）
- [ ] a. `just db-clean`：列出 PG / MySQL 上名字是 `sqlmux_e2e_<pid>` 或 `sqlmux_worker_<pid>_…` 的库，`<pid>` 在本机已经不在（`kill -0` 失败）的 drop（PG 带 `with (force)`）。只动这两种名字、pid 已死的库，不碰 `sqlmux`、`play` 和活着的。通过 `docker compose exec` 用容器里的 `psql` / `mysql` 跑，不依赖本机装客户端（WSL 上没有）。打印删了哪些、留了哪些。
- [ ] b. `just e2e` 不带参数（全量）时开跑前先调它；带脚本名时不调。`just test-integration` 不调（单个包的测试几秒就完，不值一次 docker exec）。
- [ ] c. pid 在别的机器上（tester 的 worktree 和主工作区在同一台机器，这一版不用管）：`shortcut:` 注释标上限。

**验收**
- [ ] 集成测试或脚本自测：在 PG 和 MySQL 上各建 `sqlmux_e2e_999999`（死 pid）、`sqlmux_worker_<自己的 pid>_x`（活 pid）、`sqlmux_keep`（不匹配），跑一遍后只有第一个没了。
- [ ] `just e2e`（全量）的开头日志里有 db-clean 的输出；`just e2e f0.1` 没有。

## F21.2 默认连接 `default_connection` · 状态：passed（9da01dd；e2e c311cad）

- **涉及**：`internal/config`（`default_connection`、未知名字的 toast）、`internal/app`（启动、`--table` / `--sql` 选连接的那一处）
- 用户 2026-10-31 定要加。原问：`--table` / `--sql` 不给连接时用哪个。现在 F19.1 用的是 connections.toml 的第一个，没有 last.toml 时默认布局的启动连接也是它；第一个同时决定树的顺序，想换默认连接就得挪文件里的顺序。

**开发**（§14「启动」）
- [ ] a. config.toml 顶层 `default_connection = "<名字>"`，没写照旧用第一个。`--table`、`--sql`、没有 `last.toml` 时的默认布局，三处都按它：选连接的逻辑只有一处（现在 F19.1 和默认布局各取「第一个」的，收成一个函数）。
- [ ] b. 名字不在 connections.toml 里：启动 toast「default_connection: no connection <名字>」（i18n），退回第一个，不阻止启动（同未知键的处理）。
- [ ] c. 不做：connections.toml 每条上的 `default = true`。

**验收**
- [ ] 单测：没写用第一个；写了用它；写了不存在的用第一个且有 toast；`--table` 和默认布局走同一个函数（mutation：改那一处两边都变）。
- [ ] e2e：三个连接、`default_connection` 指第二个，不带参数（无 last.toml）启动树的焦点在第二个连接上；`--table <第二个连接独有的表>` 开得出来；指个不存在的名字，toast 文字对、树照旧第一个。

## F21.3 命令行 `--sql "<语句>"` 和 `--schema` · 状态：passed（9da01dd；e2e c311cad）

- **依赖**：F21.2
- **涉及**：`cmd/sqlmux`（参数）、`internal/app`（启动布局、`freeQuery()`、query 标题的 schema 下拉那一处）、`internal/config`（usage）
- 用户 2026-10-31：比 `--table` 更通用；一个 query 一个结果。

**开发**（§14「启动」，已写全：布局、连接、schema、互斥、确认）
- [ ] a. `sqlmux [连接] [--schema …] --sql "select …"`：语句放进 `freeQuery()` 开的新编号 query（不占 `query_1`），立刻照 query 里 ↵ 执行那样跑；结果 500 行、翻页、导出照 §11。多条语句照 query 的规则逐条执行。
- [ ] b. 布局：一个 window，只有侧栏、一个聚焦的 query pane 和它下面的结果区；没有引导页的 data pane、没有 `query_1`。树照常可收。
- [ ] c. 连接按位置参数，没给按 F21.2；只读连接和危险语句的确认照 §13 原样弹。
- [ ] d. `--schema`：值的写法同 query 标题的下拉（PG `[库.]schema`、MySQL 库名、SQL Server `[库.]schema`、Redis db 号）；缺省是这个连接的（同 `--table` 现在的规则）；不存在时 toast 并留在缺省值。`--table` 也认它：没写 schema 的表名在它里面找。
- [ ] e. usage：`--sql` 和 `--table` / `--where` 互斥、和 `--layout` 互斥，英文报错退出（在 `config.Load` 之前，同现有 usage 错误）；退出不写 `last.toml`。usage 行列出 `--sql`、`--schema`。
- [ ] f. 解析和 `--table` 共用「位置参数前后都认」的那一处，不另写一遍。

**验收**
- [ ] 单测：参数解析（前后位置、互斥、usage 文案）；`--schema` 的四种写法各解析对；不存在的 schema 留在缺省且 toast。
- [ ] e2e（PG）：`sqlmux seed --sql "select count(*) from orders"` 启动后结果区有一行计数、query pane 里是这条语句、没有 data pane、`gt` 没有别的 tab；`:qa` 退出后 `last.toml` 没被写。
- [ ] e2e（PG）：`--schema other --sql "select * from t"` 跑的是 other 里的表；`--schema nope` 有 toast、用的是缺省 schema。
- [ ] e2e（PG）：只读连接上 `--sql "delete from orders"` 整次不执行、日志 tab 里有 read-only（§13，不弹确认）；普通连接上无 WHERE 的 `delete` 弹确认，取消后 query 还在。（reviewer 2026-11-07 问，决策者定：原文「只读连接弹确认」和 §13 矛盾，实现照 §13 不动。）
- [ ] `--sql` 的 query 不改就不写文件（同临时连接的 query：只在编辑器里，语句在 history.jsonl），退出后 Queries 列表里没有它；改过的才写（reviewer 2026-11-07 问，决策者定：每跑一次就多一个 query_N 文件会烦）。
- [ ] e2e（Redis）：`--sql "dbsize"` 结果区有数；`--schema 1` 用 db1。
- [ ] e2e：`--sql` 和 `--table` 同时给退出码非 0、stderr 一行 usage。

## F21.4 Redis 键名索引存盘，启动即用 · 状态：passed（ef1f344；e2e 7766a7e）

- **依赖**：M20 F20.8
- **涉及**：`internal/app`（索引的建、换、增量那一处）、`internal/config`（`[redis] index_cache`、data 目录路径）
- 用户 2026-11-02：照 JetBrains 做。起因是 F20.11：`redis-6379` 是 150ms 往返的远端 Redis，6 万键冷启动第一次补全 0.7 秒左右、整份索引几秒才建完。

**开发**（§9.8「索引存盘」，已写全：路径、格式、读写时机、配置、不做）
- [ ] a. 写：每连接每 db 一个文件 `$XDG_DATA_HOME/sqlmux/redis-index/<连接名>/db<n>`，一行一个键名（含换行的键名用 Redis 协议的转义，worker 定并在文件头一行写格式版本），权限 0600；整份重建换上后写；增量更新只改内存，去抖几秒或退出时写一次。截断的照截断存，`DBSIZE` 的总数一起存。
- [ ] b. 读：这个 db 第一次补全时有文件就先载入、马上模糊匹配；同时照现有规则后台重建，建好换上并写回。补全框底部 `dim` 一行「索引更新中」直到换上（要不要留由 reviewer 定）。
- [ ] c. 读坏了当没有，重建覆盖；`index_cache = false` 不读不写、已有文件不删；exec 连接照旧不建索引也不存。
- [ ] d. 写文件走 `config` 包已有的原子写（临时文件 + rename）那一处，不另写。

**验收**
- [ ] 单测：写出的文件格式（含带换行、带 `\r` 的键名往返）、0600、截断和总数；第一次补全先用文件里的再被重建换掉；坏文件被忽略并覆盖；`index_cache = false` 不读不写；退出时写一次（增量后）。
- [ ] 集成测试（Redis 56379）：2 万键，建一次、退出、再启动，第一次补全在后台 SCAN 开始前就有候选（断言候选到达早于第一条 SCAN，或用 slowproxy 看顺序）。
- [ ] e2e：150ms 延迟代理、6 万键的 db（F20.11 的脚本）：冷启动第二次运行时 `get p:emp` 候选在最后一个字符后 300ms 内出现（第一次运行建好索引、`:qa`）；送审附第一次和第二次「最后一个字符到候选」的时间。

## F21.5 聚焦和未聚焦的边框用不同线型 `border_focus_style` · 状态：passed（ef1f344；e2e 7766a7e）

- **涉及**：`internal/config`、`internal/ui`（画边框取线型的那一处）
- 用户 2026-10-31：未聚焦的细一点、聚焦的粗一点；lazygit 只有一个全局 `border`。

**开发**（§7.3、§14）
- [ ] a. config.toml 顶层 `border_focus_style`，取值同 `border`（single / rounded / double / bold / hidden），没写跟 `border`；聚焦的 pane 和浮层用它，其余用 `border`。取线型只在一处按「是否聚焦」选，颜色照旧 `border` / `border_focus`。
- [ ] b. `bold` 没有圆角：`border = "rounded"` + `border_focus_style = "bold"` 时聚焦 pane 是直角、切焦点四个角跟着变，是预期，docs 里写一句（oss 的 config.md，随发版）。

**验收**
- [ ] golden：`rounded` + `bold` 时聚焦 pane 粗直角、其余圆角细线；没写时两种一样；浮层（命令面板）用聚焦的线型。
- [ ] e2e：`C-l` 切焦点后 capture 的边框字符对应换掉。

## F21.6 越过 sqlmux 的边界切到外层 tmux 的 pane · 状态：passed（ef1f344；e2e 7766a7e）

- **涉及**：`internal/app`（`pane.focus.left/down/up/right` 那一处）、`internal/config`（`[pane] outer_nav`）、`docs/`（一段可复制的 tmux 配置，oss）
- issue #4（qumn，2026-10-25）。用户 2026-10-31：自己没这个需求、需求量不确定；决策者定只做 tmux，排最后，放不下就丢。

**开发**（§6「pane.focus」）
- [ ] a. `[pane] outer_nav = "off" | "auto"`，缺省 `off`。`auto` 且 `$TMUX` 设了：`pane.focus.<方向>` 在那一侧已经没有 pane（含只有一个 pane、缩放中、焦点在最左的侧栏按左）时，执行 `tmux select-pane -L/-D/-U/-R`（`exec.Command`，不等输出，失败只记日志）；还有相邻 pane 时照旧在里面切。`off` 或没有 `$TMUX` 时行为不变。
- [ ] b. 从外面切回 sqlmux 是外层的配置：docs 给 vim-tmux-navigator 那段 `is_vim` 正则加上 `sqlmux` 的写法。
- [ ] c. 不做 WezTerm / Zellij。

**验收**
- [ ] 单测：四个方向在边上 / 不在边上、`off` / `auto`、有无 `$TMUX` 的矩阵，断言调不调外部命令（把执行外部命令的那一处换成可注入的函数）。
- [ ] e2e：在 e2e 的 tmux 里把 sqlmux 开在右边的 pane、左边是 shell，`outer_nav = "auto"`，sqlmux 里焦点在侧栏按 `C-h`，`tmux display -p '#{pane_active}'` 显示焦点到了左边的 pane；`off` 时不动。

---

以下是用户 2026-11-08 验收 7766a7e 时提的，节点 3，worker 在 `main` 上做。

## F21.7 索引第一次建立期间补全框要有反馈 · 状态：todo

- **依赖**：F21.4
- **涉及**：`internal/app`（Redis 补全框、F21.4 画「索引更新中」那一处）、`internal/i18n`
- 用户 2026-11-08：WSL 经 430ms 往返的链路连 Mac 的 Redis（6 万键），query 里敲 `GET p:` 没有补全框，以为功能没做。实际是索引第一次建（约 7 轮 SCAN ≈ 3 秒）和前缀兜底（同样扫全库）都还没回来，Mac 直连几毫秒往返所以「马上有」。这 3 秒里没有任何反馈是缺口。

**开发**（§9.8「索引存盘」末尾一句）
- [ ] a. 这个 db 的索引还没建好、也没有文件可载入时，在 key 参数位置打字就先出补全框，里面只有底部那一行 dim 的「索引建立中」（复用 F21.4「索引更新中」那一行的画法和位置，文案另一条 i18n）；前缀兜底的结果到了照旧填进去，索引建好换成模糊结果。不改 SCAN 的轮数和去抖。
- [ ] b. 索引建失败（连接断、没权限 SCAN）时这一行换成错误原因，不无限「建立中」。
- tester 2026-11-08 用 MONITOR 量了冷启动（500ms 往返、6 万键、树展开 10000 键）：0–8s 连接和树的 SCAN，8.6–23.7s 树的 TYPE 流水，24.4s / 25.9s 才发 `COMMAND DOCS` / `COMMAND`，27.4s 补全的第一个 SCAN，第一批候选在启动后 36.5s；有索引文件时也要 22.7s（排在 TYPE 和 DOCS 后面，文件帮不上）；1000ms 往返时 120s 没有候选、430s 索引还没建完（期望 7 页 SCAN 十几秒）。根因不在补全本身，是 `Meta` 上的请求排队：树的整次加载是一个请求，后面的都等它。决策者 2026-11-08 定，F21.7 加三条：
- [ ] c. `COMMAND` / `COMMAND DOCS` 在连接建立后立刻发（高亮本来就要它），不等树；树的加载改成每批（一页 SCAN 加它的 TYPE pipeline）一个请求，别的请求能插进来（同 §9.8 索引建立的规则），§8.7 已改。目标：500ms 往返、连上就打字，`DOCS` 在 2–3 秒内到、第一批候选在最后一个字符后 10 秒内。
- [ ] d. 1000ms 往返时索引 430s 建不完：先量（MONITOR 看是不是同一页在重发、go-redis 的 ReadTimeout 对一页 10000 个键的回包是不是太短），改根因；目标 1000ms 往返 6 万键索引 30 秒内建完、文件写出。超时值要改就改在 `internal/db/redis` 拨号那一处并写进 §8.7，附数字。
- [ ] e. 验收补：500ms 代理、连上就敲 `GET p:`，300ms 内有框和「索引建立中」，10 秒内有候选；1000ms 代理索引 30 秒内建完、`:qa` 后文件在。e2e 的等待时间按这两个数定。

**验收**
- [ ] 单测：冷启动在 key 位置打字 → 框开着、只有「建立中」一行；前缀结果到达 → 候选进来、那行还在；索引换上 → 那行消失；SCAN 出错 → 那行是错误文字。
- [ ] e2e：1 秒往返的代理 + 6 万键，敲 `GET p:` 后 300ms 内有框和「建立中」；索引建完后那行消失、候选是模糊结果；`:qa` 重启后同 f21.4b（有文件就是「更新中」，不是「建立中」）。

