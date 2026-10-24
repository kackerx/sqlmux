# M18 Windows 与 SQL Server · 任务清单

- **状态**：**todo**（2026-10-24 决策者起草；用户 2026-10-19 定 Windows 排 M18、2026-10-20 定 SQL Server 跟在 Windows 后面一起做；2026-10-24 用户在家里的 Windows 主机上起了一个 agent 并提了 PR #3，这个里程碑由它做 Windows 那半、节点 3 起视情况做 SQL Server）。
- **目标**：两件事。一是原生 Windows 能正常简单使用：Windows Terminal 里的 PowerShell / cmd 下装得上、跑得起、配置和子进程不出错，发布出 Windows 的包；做不好的地方明说，不硬做（用户：WSL 已经是 Linux，体验一致）。二是 SQL Server 引擎 `mssql`：树、表格、query、编辑、分页都走现有的共用层，只加一个 `internal/db/mssql` 和一个方言。
- **范围**：§4（`internal/db/mssql`）、§7.8「剪贴板」（Windows 的工具）、§8.1「SQL Server」、§8.5（`OFFSET … FETCH`）、§9（方言 `MSSQL`、`GO` 分批）、§14「Windows」「`engine = "mssql"`」、§15（`SQLMUX_TEST_MSSQL`、Windows runner）。都已写进各章（2026-10-24）。
- **依赖**：M17 完成（v0.6.1）。
- **完成标准**：节点全过；Windows 主机上 `go test ./...` 全绿、CI 的 windows job 绿；用户在 Windows Terminal 里用 Releases 的 zip 装一遍、浏览 SQL Server 容器里的一张表并改一格保存；Mac 上完整回归绿（含 SQL Server 的 e2e）。
- **审查节点**：1 = F18.1–F18.3（Windows 编译、路径与子进程、剪贴板；PR #3 重做后是这一批的第一个 commit）；2 = F18.4 + F18.5（发布与 CI、Windows 的验收方式）；3 = F18.6（SQL Server 引擎）；4 = F18.7（SQL Server 的 e2e、文档）。
- **不做（留候选）**：`%APPDATA%` 以外的 Windows 惯例（注册表、安装器、scoop / winget 包）；老 conhost；Windows 上跑 e2e（没有 tmux，见 F18.5）；SQL Server 的 Windows 集成认证（`trusted_connection`，纯 Go 驱动在非 Windows 上没有）、Always Encrypted、`DELIMITER` 式的存储过程体；exec 批多条语句、`{db}` 占位符、Redis 第二轮（见 plan.md「M18 候选」余下的）。

---

## M18 的协作方式：Windows 主机上的 worker

Windows 那半由用户家里 Windows 主机上的 agent 做（下面叫 **win-worker**），它在 Tailscale 网络里，和 Mac 上的四个会话不通消息；SQL Server 那半默认也由它做（容器在它那台机器上，x86，Docker Desktop 跑官方 amd64 镜像不用模拟），Mac 上的 worker 只做合并、打 tag、推送。规则和 `specs/roles/worker.md` 一样，差异如下：

- **分支**：win-worker 在 `windows-compat` 分支上做（已有 PR #3，draft），不直接提交 `main`。每到一个审查节点把分支 rebase 到 `origin/main` 后推送，在 PR 里留一条评论「节点 N 已送审：<范围>，重点 …」。reviewer 在 Mac 上审 `origin/main..origin/windows-compat`（`/Users/ctw/proj/sqlmux-review` 里 fetch 后 detach 到分支头），退回写成 PR 评论；tester 同样。过了的节点由 Mac 上的 worker `git merge --ff-only` 进 `main`（不要 merge commit；rebase 过的分支才能 ff），决策者填状态。审查开始后不要 force-push 改写已送审的 commit，修复另起 commit。
- **git 身份**：作者身份由仓库 git 配置决定，和 Mac 一致：`git config user.name kackerx`、`git config user.email 55371516+kackerx@users.noreply.github.com`（PR #3 的 commit 是 `win-setup <setup@local>`，重做时改掉）。commit 说明英文，前缀 `F18.<序号>:`（PR #3 的 `windows:` 不合规矩），不加 `Co-Authored-By` 之类的署名行。
- **行尾**：`git config core.autocrlf false`，仓库文件一律 LF；Windows 上写的新文件也是 LF（编辑器设一下）。i18n 的单测遍历整个工作区的 `.go` 文件，CRLF 或工作区里多出来的 `.go` 副本都会让它失败——PR #3 说明里「en.go 存在未被引用的中文键」多半是这个，重新 clone 后再跑一次，还红就把输出贴到 PR。
- **每个 commit 自己保证 `just check` 全绿**，在 Windows 上和 Mac 上一样。Windows 上要装的（都是命令行能装的，`winget` / `go install`）：
  - `just`（`winget install Casey.Just`）；
  - `golangci-lint` v2.14.0（`go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0`，justfile 校验版本号）；
  - `gofmt` 随 Go；Go 版本按 `go.mod`（1.26.3，更新的也行）；
  - Docker Desktop（WSL2 后端）跑 `just db-up`：PG 17、MySQL 8.4、Redis 的集成测试和 M18 新加的 SQL Server 容器都在它上面；端口和 Mac 一样（55432 / 53306 / 56379 / 51433）；
  - `gh`（`winget install GitHub.cli`）发 PR 评论；Python 3 只有 e2e 要，Windows 上不跑 e2e；
  - 没有 tmux：e2e 不在 Windows 上跑，由 Mac 上的 tester 跑（F18.5 定了 Windows 的验收方式）。
- **路径和平台的坑**：测试里拼路径用 `filepath.Join`，不写 `/`；`0o600` 这类权限位在 Windows 上没有 group / other 位，断言按 PR #3 的做法加 `runtime.GOOS` 守卫；`syscall` 的 Unix 符号（`Flock`、`Kill`、`SIGWINCH`）只能在 `//go:build !windows` 的文件里出现；子进程一律经 F18.2 的 helper，不直接 `exec.Command("sh", …)`；`os.Rename` 覆盖已有文件在 Windows 上可以，但目标文件被别的进程打开着会失败（state.json 的锁已经保证一次一个写者）。
- **已有依赖直接用，不重写**（AGENTS.md）：Windows 的系统调用用 `golang.org/x/sys/windows`（已在 `go.mod` 里，间接依赖转直接），不用 `syscall.NewLazyDLL` 手写 `LockFileEx`。
- **spec 问题**：win-worker 没法给决策者发消息，写在 PR 评论里、标「spec 问题」，用户转给决策者；答复也回在 PR 里。

---

## F18.1 Windows 编译与文件锁 · 状态：todo

- **依赖**：无
- **涉及**：`internal/config`（state.json 的锁）、`internal/db`（ssh 测试）、`go.mod`
- 决策者 2026-10-18 交叉编译：只有 `internal/config/state.go` 的 `syscall.Flock` 编不过。PR #3（6632e71）已经做了拆分，按下面的要求重做成一个 commit。

**开发**（§14「Windows」）
- [ ] a. `lockFile` 拆成 `lock_unix.go`（`//go:build !windows`，原来的 `Flock`，逐字节等价）和 `lock_windows.go`（`//go:build windows`，`golang.org/x/sys/windows` 的 `LockFileEx` / `UnlockFileEx`，`LOCKFILE_EXCLUSIVE_LOCK`、不带 `FAIL_IMMEDIATELY`，阻塞语义同 `LOCK_EX`；锁整个文件的第一个字节就够，两个进程锁同一段才互斥）；`go mod tidy` 把 `x/sys` 转成直接依赖。
- [ ] b. `internal/db/ssh_test.go` 加 `//go:build !windows`（用了 `syscall.Kill`）；`state_test.go` 两处 `0o600` 断言加 `runtime.GOOS != "windows"` 守卫（PR #3 的做法，保留）。
- [ ] c. commit 说明 `F18.1: …`，作者 kackerx；PR #3 的说明里列的三个遗留（sqlite 路径、`/bin/sh`、i18n）分别是 F18.2 和「行尾」那条，不在这条里。

**验收**
- [ ] 单测：现有的 state.json 多进程写测试（`beforeRename`）在 Windows 上跑过（两个进程排队，文件不坏）；mutation（Windows 上把 `LockFileEx` 的标志改掉 / 去掉 unlock，测试要红）。
- [ ] `GOOS=windows go vet ./...` 在 Mac 上零报告（reviewer 跑）；win-worker 贴 Windows 上 `go test ./...` 的汇总。

## F18.2 Windows 的路径与子进程 · 状态：todo

- **依赖**：F18.1
- **涉及**：`internal/config`（目录、`password_cmd`、sqlite 路径）、`internal/app`（`$EDITOR` 开 query 文件、`gq` 的 formatprg）
- 三处 `exec.Command("sh", "-c", …)` 在 Windows 上没有 sh；配置目录落在 `~/.config` 不是 Windows 惯例；sqlite 的 `C:\x.db` 会被当成 URL 的 scheme。

**开发**（§14「Windows」）
- [ ] a. 子进程：`config` 包一个 helper `ShellCommand(ctx, line string) *exec.Cmd`（名字 worker 定）：非 Windows `sh -c <line>`，Windows `%COMSPEC% /c <line>`（没设 `COMSPEC` 用 `cmd`）。三处调用都改走它；`$EDITOR` 那处原来用 `sh -c 'ed "$1"' sh path` 传位置参数，Windows 没有，改成把路径用引号拼进命令行（路径里有引号的不处理，已知上限）。`VISUAL` / `EDITOR` 没设时 Windows 默认 `notepad`，不是 `vi`。
- [ ] b. 目录：`XDG_*` 设了照旧优先；没设时 Windows 用 `%APPDATA%\sqlmux`（配置）和 `%LOCALAPPDATA%\sqlmux`（data 和 state，history.jsonl、state.json、布局文件），`os.UserConfigDir` / `os.UserCacheDir` 给的就是这两个；非 Windows 不变。
- [ ] c. sqlite 的 DSN：`C:\…` / `D:/…` 这种一个字母加冒号的是盘符不是 scheme（`Kind` 的判断加一条）；`~` 展开用 `os.UserHomeDir`，Windows 上就是用户目录；路径分隔符两种都认。
- [ ] d. 钥匙串：`hostOS != "darwin"` 的分支（明文或 `password_cmd`）在 Windows 上照旧，不接 Credential Manager（候选）。

**验收**
- [ ] 单测：helper 在两种 GOOS 下的 argv（表驱动，`runtime.GOOS` 分支各一条）；目录函数在 Windows 上的返回（`XDG` 设 / 不设）；`Kind("C:\\a.db")` 是 sqlite、`Kind("postgres://…")` 不变；mutation。
- [ ] win-worker 手测：Windows Terminal 里 `password_cmd = "echo secret"` 能连；`C-o` 用 notepad 开 query 文件、关掉回来内容刷新；`sqlmux C:\path\x.db` 能开；配置文件生成在 `%APPDATA%\sqlmux`。贴截图或文字到 PR。

## F18.3 Windows 的剪贴板 · 状态：todo

- **依赖**：无
- **涉及**：`internal/app/yank.go`
- 写剪贴板已有 OSC 52 兜底，Windows Terminal 支持；读（`"+p`）没有工具就不行。

**开发**（§7.8「剪贴板」）
- [ ] a. `clipTools` 加 Windows 一行：写 `clip`，读 `powershell -NoProfile -Command Get-Clipboard -Raw`（只在 `runtime.GOOS == "windows"` 时加入；读回来的 `\r\n` 换成 `\n`）。其余不动。

**验收**
- [ ] 单测：Windows 下工具表多这一行、非 Windows 不变；`\r\n` 的归一；mutation。
- [ ] win-worker 手测：表格里 `"+y` 后别的程序能贴出来；外面复制一段文字，query 里 `"+p` 贴进来。

## F18.4 发布与 CI · 状态：todo

- **依赖**：F18.1–F18.3
- **涉及**：`.goreleaser.yaml`、`npm/build.mjs`、`.github/workflows/ci.yml`、`README.md`（归 oss）

**开发**（§15）
- [ ] a. goreleaser 加 `windows` 的 `amd64` 和 `arm64`，Windows 的压缩包用 zip（`format_overrides`），二进制名 `sqlmux.exe`。
- [ ] b. npm 壳包加 `win32-x64`、`win32-arm64` 两个平台包（`build.mjs` 的表加两行，壳的启动脚本认 `.exe`）。
- [ ] c. CI 加一个 `windows-latest` 的 job：`go build ./...` 和 `go test ./...`（不跑集成测试、不跑 lint；lint 在 ubuntu 上跑一次就够）。
- [ ] d. README 加「Windows」一小节（oss 做）：Windows Terminal + PowerShell / cmd，装法三条（Releases 的 zip、`go install`、npm），WSL 等于 Linux，老 conhost 不支持，Nerd Font 同样要装。

**验收**
- [ ] `just release-check`（或等价的 goreleaser `--snapshot`）六个目标都编过；CI 三个 job 加 windows job 绿。
- [ ] 发版后 oss 的安装验证多一条：用户在 Windows 上用 zip 和 `npx` 各装一次看 `--version`。

## F18.5 Windows 的验收方式 · 状态：todo

- **依赖**：F18.1–F18.3
- **涉及**：`e2e/`（Mac 的 tester）、`specs/roles/tester.md` 不改（这里写清就够）
- Windows 没有 tmux，现有的 e2e 脚本跑不了；真正要验的是 Windows Terminal 的 ConPTY 路径。

**开发**
- [ ] a. tester 从 Mac `ssh` 到 Windows 主机（OpenSSH Server、公钥、默认 shell pwsh，用户准备），在那边 `go test ./...`、编一个二进制。
- [ ] b. 一个最小的 ConPTY 冒烟脚本（Python `pywinpty` 加 `pyte`，放 `e2e/windows/`，Windows 上跑）：起 sqlmux 连 sqlite 的 demo 文件，发 `j` `k` `↵` `gt` `q`，截屏比几处固定文字（树的连接名、表名、状态栏的 NORMAL）。不求覆盖，只证明键盘和渲染在 ConPTY 上通。
- [ ] c. 一份给用户的手测清单（WALKTHROUGH 的 Windows 一节，决策者写）：真彩色、鼠标点击和滚轮、图标、`C-c` / `C-z`、`"+y`、窗口缩放、中文宽度。

**验收**
- [ ] tester 在 Windows 主机上跑过 a 和 b，结论里附 ConPTY 截屏的文字。

## F18.6 SQL Server 引擎 · 状态：todo

- **依赖**：节点 1 过（win-worker 在 Windows 上做，容器在它那台机器上）
- **涉及**：`internal/db/mssql`（新包，§4）、`internal/db`、`internal/sqlkit`（Dialect）、`internal/config`、`internal/app`（树、表单、`workspace.go` 的 `engines`）、`docker-compose.yml`、`testdata/seed/mssql.sql`、`.github/workflows/ci.yml`
- 用户 2026-10-20 定（reddit 评论有人问）。做法照 M14 的 F14.1：一个新包实现 `db.Engine`，别的都是共用层。

**开发**（§4、§8.1「SQL Server」、§8.5、§9、§14、§15）
- [ ] a. 驱动 `github.com/microsoft/go-mssqldb`（纯 Go，database/sql）；`engine = "mssql"`，DSN `sqlserver://user:pass@host:1433?database=x`（驱动的 URL 形式原样传，`encrypt` / `TrustServerCertificate` 等参数照传；本地容器要 `TrustServerCertificate=true`）；密码来源同其他引擎；`Info` 用 `SERVERPROPERTY('ProductVersion')`；测试按钮 `SELECT 1`。
- [ ] b. 方言 `sqlkit.MSSQL`：标识符 `[name]`（也认 `"name"`），字符串 `'…'` 和 `N'…'`，注释同 PG；**分批**：单独成行的 `GO`（大小写不限、后面可带次数）是客户端分隔符，在那里切开、不发给服务端，批内再按 `;` 分句；格式化用 sql-formatter 的 `transactsql`；补全关键字表共用 PG 的（同 SQLite，决策者定）；`CaseInsensitive` 为真（默认排序规则不分大小写；分大小写的排序规则是已知上限）。
- [ ] c. `Engine`：`Quote` 是 `[s].[t]`，`Arg(n)` 是 `@p<n>`，`Same` / `Eq` 照 MySQL 的形状（`IS NULL` 那条路一样）；`Schemas` 查 `sys.schemas` 去掉 `sys`、`INFORMATION_SCHEMA`、`guest`、`db_*`，current 是 `SCHEMA_NAME()`；`Databases` 查 `sys.databases` 里 `HAS_DBACCESS(name) = 1` 的、去掉 `master` / `tempdb` / `model` / `msdb`，子 Conn 换库同 PG（DSN 的 `database` 参数）；`Tables` 查 `sys.tables` / `sys.views` 加 `sys.dm_db_partition_stats` 的行数估计；`TableColumns` 查 `sys.columns` / `sys.types` / `sys.identity_columns`，主键和唯一索引查 `sys.indexes` / `sys.index_columns`；`Indexes` / `Keys`（`sys.foreign_keys`）/ `Functions`（`sys.objects` 的 FN / IF / TF，`OBJECT_DEFINITION`）/ `Sequences`（`sys.sequences`）；`DDL`：视图和函数用 `OBJECT_DEFINITION`，表没有服务端的 `SHOW CREATE`，按 catalog 拼最小的 `CREATE TABLE`（列、类型、NULL、主键），注明是拼的；`UseSchema` 无事（SQL Server 没有 search_path，`Quick` 和 query 用 `[schema].[table]` 全名，§8.6 的「当前 schema」只影响补全和不带前缀的表名）。
- [ ] d. 分页与计数（§8.5）：`SELECT … ORDER BY <排序列，没有就 (SELECT NULL)> OFFSET @p1 ROWS FETCH NEXT @p2 ROWS ONLY`，多取一行判断还有没有下一页照旧；计数 `COUNT_BIG(*)`。
- [ ] e. `Conn`：`Exec` 一批一次（go-mssqldb 一次能发多条语句，多个结果集用 `NextResultSet` 收齐）；`Query` 走参数；`Stream` 同 PG / MySQL 的流式导出；取消把 ctx 给驱动（它发 attention 包），**在真库上验证**取消一条 `WAITFOR DELAY '00:00:10'` 能在 1 秒内回来、连接还能用；`Broken`：网络错误和 `mssql.Error` 里的连接类错误号；`ErrorOf`：`mssql.Error` 的 `Number` / `Message` / `LineNo`。
- [ ] f. 只读连接：SQL Server 没有只读事务，和 Redis 一样靠 §13 的应用层拦截（`Dangerous` 判定认 T-SQL 的 `DELETE` / `UPDATE` 无 WHERE、`TRUNCATE`、`DROP`、`ALTER`、`EXEC`）；事务 `BEGIN TRAN` / `COMMIT` / `ROLLBACK` 走共用的 `db.End`。
- [ ] g. 类型与文本（§8.1）：以集成测试为准，拿不准的不猜：`datetime2` / `datetime` / `datetimeoffset` / `date` / `time` 的文本形式、`uniqueidentifier`（驱动的 `mssql.UniqueIdentifier`，显示标准 GUID 文本）、`bit` 显示 `true` / `false`、`money` / `decimal` 按文本、`varbinary` / `image` 走 `<binary N bytes>`、`xml` 和 `nvarchar(max)` 按文本；值编辑器和保存的回写（`Same` 的比较）每种各一条集成测试。
- [ ] h. 连接表单的 engine 段加 mssql（host / port 默认 1433 / database / user / password / read_only / production）；树：连接 → 库 → schema → Tables / Views / Functions / Sequences，同 PG。
- [ ] i. 测试环境：`docker-compose.yml` 加 `mssql`（`mcr.microsoft.com/mssql/server:2022-latest`，`platform: linux/amd64`，端口 51433，`ACCEPT_EULA=Y`，`MSSQL_SA_PASSWORD=Sqlmux!2026`，healthcheck 用镜像自带的 `/opt/mssql-tools18/bin/sqlcmd -C`）和一个一次性的 `mssql-seed` 服务（同一镜像，等健康后 `sqlcmd -C -i /seed/mssql.sql` 灌 `testdata/seed/mssql.sql`，和 PG / MySQL 一样只在数据卷为空时）；seed 内容对照 `mysql.sql`（同样的表和行，类型换成 T-SQL 的）；集成测试用 `SQLMUX_TEST_MSSQL`（`sqlserver://sa:Sqlmux!2026@localhost:51433?database=sqlmux&TrustServerCertificate=true`），没设就 skip；CI 的 integration job 用 `services` 起同一镜像加一步 seed。Mac 上这个镜像要 Rosetta，Mac 的 worker / reviewer 直接连 Windows 主机的容器（Tailscale 地址，用户给）。

**验收**
- [ ] 单测：方言（`GO` 分批、`[name]` 引号、`N'…'`）、`Quote` / `Arg` / `Same` / `Eq`、分页语句的拼法；mutation。
- [ ] 集成测试（`SQLMUX_TEST_MSSQL`，和 PG / MySQL 同一组用例）：catalog、各类型的文本、取消、只读拦截、乐观校验、唯一索引作行标识、无主键表只读、库列表、`GO` 分批。
- [ ] win-worker 贴 Windows 上集成测试的汇总；reviewer 在 Mac 上连 Windows 主机的容器跑一遍。

## F18.7 SQL Server 的 e2e 与文档 · 状态：todo

- **依赖**：F18.6
- **涉及**：`e2e/`（tester）、`docs/`、`README.md`（oss）

**开发**
- [ ] a. e2e：照 MySQL 的那组脚本（树、开表、翻页、WHERE、改格保存、query、`GO` 分批、只读连接、库切换）各一份 mssql 的，连接 `mssql`（connections.toml 的测试段加一条，指向 51433）；Mac 上的 tester 连 Windows 主机的容器跑。
- [ ] b. 文档（oss）：README 的引擎列表加 SQL Server，`docs/config.md` 加 `engine = "mssql"` 的 DSN 例子和 `TrustServerCertificate` 的说明，CHANGELOG 0.7.0。

**验收**
- [ ] 完整回归绿；用户在 Windows Terminal 里连自己容器的 `sqlmux` 库，开 `t_order`、改一格保存、跑一条带 `GO` 的 query。

任务文件的格式和状态约定见 [`../plan.md`](../plan.md)。
