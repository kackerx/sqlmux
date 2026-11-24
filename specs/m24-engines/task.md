# M24 DuckDB · 任务清单

- **状态**：todo（2026-11-24 决策者起草，用户同日定直接开工）。
- **目标**：DuckDB 作为第六个引擎：打开 `.duckdb` 文件或内存库，树、表格、改格子保存、query、取消都和 PG 一样能用；不用 cgo，发布方式和二进制大小不变。发 v0.12.0。
- **范围**：§8.1「DuckDB」（全部细节在那一条）、§4 的 `internal/db/duckdb`、§14 的 `engine = "duckdb"` 和 `[duckdb] library`、命令行的 `.duckdb` / `.ddb`、§7.8 连接表单的 duckdb 段。评估和实测在 `eval.md`；探针在仓库外 `/home/kacker/proj/sqlmux-aud/engines/duckdb/nocgo/b/`（purego 绑定 167 行，取消、两条连接、`cast(columns(*) as varchar)` 都在 Linux 和 Windows 上跑过），照它写，不从零摸索。
- **依赖**：M22 完成（v0.11.0）。M23 书签往后排。
- **完成标准**：两个节点全过；完整回归绿（清单外 0）；Windows 冒烟绿，另在 Windows 上连一次 DuckDB；用户验收后打 `m24`、`v0.12.0`，照 AGENTS.md 两步推送。
- **审查节点**：1 = F24.1 + F24.2；2 = F24.3 + F24.4。都由 worker 在 `main` 上做，按 AGENTS.md 的后台会话和 Codex 审查走。
- **不做（留候选）**：`attach` 进来的库进树（数据库层）；官方 cgo 驱动的构建包；DuckDB 的多进程写（2.0 的 Quack）；无主键表靠 `rowid` 编辑；在 sqlmux 外面限制 DuckDB 的内存（崩溃会带走界面，用户可在 DSN 里设 `memory_limit`）。

**开发用的库**：`/home/kacker/proj/sqlmux-aud/engines/duckdb/dl/lib/libduckdb.so`（v2.0.1，linux/amd64），集成测试 `SQLMUX_TEST_DUCKDB=<这个路径>`；DuckDB CLI 在同目录的 `../cli/duckdb`，对照文本用（本机设了 `AI_AGENT` 时 CLI 要加 `-no-agent`）。

---

## F24.1 libduckdb 的绑定与 Conn · 状态：todo

- **依赖**：无
- **涉及**：`internal/db/duckdb`（新包）、`go.mod`（加 `github.com/ebitengine/purego`）
- **先交方案再动手**（决策者要求）：在真库上把下面 a–f 里标「实测」的几处跑一遍，连同打算的类型和函数清单一条消息发给决策者，批了再写。

**开发**（§8.1「DuckDB」）
- [ ] a. 绑定：purego 注册用到的 C 函数（照探针 `duck.go`，`duckdb_result` 只传指针）；`load_unix.go` / `load_windows.go` 两个文件分平台的加载。库的查找按 §8.1「库从哪来」的顺序（这一条只做查找和报错：找不到时返回一个可识别的错误，F24.3 接上确认下载）。
- [ ] b. 实例和连接：同一个文件在进程里只 `duckdb_open` 一次，Meta 和 Main 各 `duckdb_connect` 一条；用 C API 的实例缓存还是包里自己一个按路径的表加引用计数，**实测**后定，最后一条连接关了才 `duckdb_close`（断开连接后用户的 Python 要能打开这个文件）。DSN：路径规则照 SQLite 的 `Path`（复用，不抄），`?mode=rwc` 才新建，其余 `?k=v` 用 `duckdb_set_config`；`read_only` 连接用只读事务、`?mode=ro` 才是文件级只读（§8.1，worker 2026-11-24 实测后改）；**实测** Meta 的只读用什么办法（事务级只读）。`Info`：`Addr` 是路径，版本 `select version()`。
- [ ] c. `Exec` / `Query`：用 `duckdb_extract_statements` 或共用的分句切开；查询形的语句包成 `select cast(columns(*) as varchar) from (<语句>)`，列名和类型从 prepared statement 的列信息或 `describe` 取（**实测**哪个省一次往返）；同名列用真名。非查询形的语句原样跑，行数用 `duckdb_rows_changed`；**实测** `returning`、`show`、`pragma`、`describe`、`explain` 的结果文本，给不出的写成已知上限。`Stream`（流式导出）同 PG / MySQL 的形状。
- [ ] d. 取消：ctx 结束时另一个 goroutine 调 `duckdb_interrupt`，每 300 ms 重发直到查询返回；事务里取消后 `TxStatus` 报 `'E'`。
- [ ] e. `Broken` 恒为不是断线；`ErrorOf` 拆 `<类别> Error: <消息>`。
- [ ] f. `duckdb_value_varchar` 在头文件里标了废弃（2.0.1 仍导出），只用来读已经是 VARCHAR 的值；在调用处用 `shortcut:` 注明上限和以后换成 chunk API 的办法。

**验收**
- [ ] 单测：DSN 解析（`~`、相对路径、`?mode=rwc`、配置项、`:memory:`）、错误拆分、语句包装；mutation。
- [ ] 集成测试（`SQLMUX_TEST_DUCKDB`）：各类型的文本和 CLI 的一致（LIST、STRUCT、MAP、DECIMAL、HUGEINT、TIMESTAMPTZ 换 `TimeZone`、UUID、BLOB、INTERVAL、NULL 和字符串 'NULL'）；取消一条长查询 1 秒内回来、连接还能用；事务里取消后 aborted；Meta 写入被拒；两条连接互不阻塞；断开后另一个进程（CLI）能打开文件。
- [ ] `GOOS=windows`、`GOOS=darwin`、`GOARCH=arm64` 交叉编译过，`CGO_ENABLED=0`。

## F24.2 DuckDB 引擎与接线 · 状态：todo

- **依赖**：F24.1
- **涉及**：`internal/db/duckdb`、`internal/sqlkit`（Dialect、格式化映射）、`internal/config`（engine 白名单、`[duckdb] library`）、`internal/app`（`workspace.go` 的 `engines`、表单、树的图标、命令行位置参数）、`testdata/seed/duckdb.sql`、`.github/workflows/ci.yml`
- 做法照 F18.6：一个包实现 `db.Engine`，其余是共用层。新加一个引擎时要改到的每一处，对照 F18.6 的 a–i 和 `git log --stat` 里那次的改动面列清单，一处不漏。

**开发**（§8.1、§8.5、§9、§14、§7.8）
- [ ] a. 方言 `sqlkit.DuckDB`：词法、补全关键字同 PG；`format.go` 的映射加 `duckdb`（sql-formatter 已带）；`CaseInsensitive` 照 DuckDB 的标识符规则（不分大小写、保留原样），在方案里说明对补全的影响。
- [ ] b. `Engine`：`Quote` / `Arg` / `Same` / `Eq`（整个括起来）/ `Schemas` / `Tables`（行数估计）/ `TableColumns`（主键、唯一约束）/ `Keys`（外键，`gd` 跳转要能用）/ `Indexes` / `Functions`（用户的宏）/ `Sequences` / `FunctionDef` / `DDL` / `Quick` / `UseSchema`，查询见 §8.1。`Databases` 返回 nil。
- [ ] c. 分页、计数、AutoLimit 走 PG 的形状；保存、乐观校验、事务收尾走共用层，不另写。无主键也没有唯一约束的表只读，原因写在界面上（同 SQLite 的 blob 键那条的话术）。
- [ ] d. 配置和界面：`engine = "duckdb"`；`[duckdb] library`；连接表单的 duckdb 段同 sqlite（§7.8）；树的连接图标加 `duckdb`（nerd font 里挑一个，ascii 回退，主题可覆盖，同 `postgres` / `redis`）；命令行 `sqlmux x.duckdb` / `x.ddb` 开临时 DuckDB 连接（§14）；`--table` 的当前 schema 是 `main`。
- [ ] e. 测试环境：`testdata/seed/duckdb.sql`（对照 `pg.sql` 的表和行，类型换成 DuckDB 的；外键只能写在 `create table` 里，DuckDB 不支持 `alter table add constraint foreign key`），集成测试在临时目录建库导入；CI 的 integration job 加一步下载钉死版本的 libduckdb（同 F24.3 的地址和 sha256），设 `SQLMUX_TEST_DUCKDB`。

**验收**
- [ ] 单测：方言、`Quote` / `Arg` / `Same` / `Eq`、命令行认扩展名；mutation。
- [ ] 集成测试（和 PG / SQLite 同一组用例）：catalog、DDL、分页、乐观校验（旧值被改报行已变化；旧值在表里重复时只改一行）、唯一约束当行标识、无主键表只读、外键、`UseSchema`、只读连接拦截写。
- [ ] e2e（节点 1 的 tester）：打开 DuckDB 文件、树、开表翻页、改一格保存、别人改了报行已变化、无主键表只读、query 里查一个 CSV 文件、取消长查询、只读连接拦写、命令行 `sqlmux x.duckdb`；libduckdb 用 `[duckdb] library` 指向开发用的库。表单 engine 行多了 `│ duckdb`，`f7.4.sh`、`f18.7c.sh` 跟着改。
- [ ] Windows：`GOOS=windows` 编的 exe 放 C 盘，从 DuckDB 的 Release 下 `duckdb.dll`，配 `[duckdb] library`，在主机上连一次、开表、改一格保存、取消一条长查询（AGENTS.md「WSL」的冒烟办法）。

## F24.3 找不到 libduckdb 时确认下载 · 状态：todo

- **依赖**：F24.1
- **涉及**：`internal/db/duckdb`（下载和校验）、`internal/app`（确认框，复用现有的确认浮层）、`internal/config`（数据目录）

**开发**（§8.1「库从哪来」）
- [ ] a. 钉死的版本（开工时取 DuckDB 最新的稳定版，现在是 2.0.1）和每个平台包的 URL、sha256 写在源码的一张表里（linux amd64 / arm64、darwin amd64 / arm64、windows amd64 / arm64；哪个平台没有官方包就不在表里，报错给手动办法）；包是 zip 还是 tar.gz、库文件在包里叫什么，照 Release 的实际资产写。
- [ ] b. 连 DuckDB 时找不到库：弹确认框（版本、大小、下载地址、存放路径）；确认后下载到数据目录 `duckdb/<版本>/`，先写临时文件、校验 sha256、解出库文件、改名就位，失败删掉残留；下载中能取消，进度用现有的 toast / 状态。不确认就报错，附手动办法（brew、Release 地址、`[duckdb] library`）。测试连接按钮走同一条路。
- [ ] c. 加载失败但文件在（Windows 缺 VC++ 运行库、架构不对）：报错点明原因，不反复提示下载。

**验收**
- [ ] 单测：sha256 不对时不落盘、残留被删；表里每个平台都有条目或明确不支持；用一个本地的 HTTP 服务假装 Release（不连外网）。
- [ ] 手动：清掉数据目录后在 Linux 和 Windows 上各走一遍「确认 → 下载 → 连上」，worker 贴结果。

## F24.4 e2e 与文档 · 状态：todo

- **依赖**：F24.2、F24.3
- [ ] a. e2e（tester）：确认下载那条用本地假服务或只测「取消下载给出手动办法」；库在、库坏（加载失败）两种提示。打开文件、浏览、编辑、取消这些在节点 1 的 tester 那一轮已经写了（见 F24.2 的验收）。
- [ ] b. 文档（oss，没起 oss 会话时决策者代管）：README 和 docs 的引擎列表、DuckDB 的连接写法和库的来源；brew formula 和 npm 包的描述加 DuckDB（`.github/scripts/tap-formula.sh`、`npm/sqlmux/package.json`）；CHANGELOG。
