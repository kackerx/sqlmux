# sqlmux 技术方案 · §8 数据访问

> 这是技术方案的一章，各章的索引和阅读规则在 [`../tech-design.md`](../tech-design.md)；章节编号（§N.M）全仓库通用，不随文件改。

## 8. 数据访问

### 8.1 接口

```go
type Conn interface {
    Exec(ctx context.Context, sql string, maxRows int) ([]Result, error)  // 多语句，走简单协议
    Query(ctx context.Context, sql string, args ...Val) (Result, error)   // 单语句，参数按文本或 NULL 传
    Close() error
}

type Val struct{ S string; Null bool }

type Result struct {
    Cols      []Col             // Name, Type（数据库类型名）, Numeric
    Rows      [][]Val
    Tag       string            // 例如 "UPDATE 3"
    Affected  int64             // 影响行数；保存、新增、删除按它检查（PG 上和按 Tag 判断的结果一致）
    Truncated bool
    Took      time.Duration
}
```

- `postgres`、`mysql` 各实现一份 `Conn`，外加各自的 catalog 查询和方言函数（`QuoteIdent`、`Placeholder(n)`）。M1 只有 `postgres` 一个实现，接口仍然要有：`db` 里的 Worker 不能 import `db/postgres`，而 `db/postgres` 要用 `db` 的 `Val` / `Result`，接口是断开这个环的办法，M5 的 mysql 是第二个实现。
- **取消走 context，不单设 Cancel 方法**：取消当前请求的 ctx，由 pgconn 的 `CancelRequestContextWatcherHandler` 发 CancelRequest（§8.3）。直接调 `PgConn.CancelRequest` 有竞态：请求刚结束时发出的取消会打到下一条查询上，pgconn 源码 `HandleCancel` 的注释写明了这一点。
- `Col.Type` 在 PG 下用 pgtype 内置的 OID 表转成类型名（`int8`、`text`、`jsonb` …），认不出的 OID（枚举等自定义类型）留空；按列着色（F1.3）改用 catalog 的 `format_type`，快速 SQL 里的自定义类型按字符串处理。
- `Session` 只依赖这些接口。以后接入非 SQL 引擎（如 Redis）时，由引擎决定 window 里能创建哪些 pane。

**值一律按文本处理**：客户端只负责显示和回写，按文本处理可以绕开各类类型的解码问题（枚举、数组、range、geometry 等）。

- **PG**：
  - `pgconn.Exec`（简单协议）本身就返回文本；`ExecParams` 把结果格式指定为文本，参数 OID 传 0，由服务端推断类型。
  - 列类型取自 FieldDescription 里的 OID。
  - 建连时通过 RuntimeParams（startup 消息）设置 `DateStyle = ISO, YMD`，保证时间文本的格式可以解析（§10.2）。每条连接都会带上，不会漏设，也不需要额外的往返。
  - 连接参数交给 `pgconn.ParseConfig(dsn)` 解析，环境变量（`PGHOST` 等）和 `~/.pgpass` 都由它处理，不自己实现。没写 `application_name` 时补成 `sqlmux`，没写连接超时时补成 10s。
- **MySQL**：DSN 加上 `interpolateParams=true`，全程走文本协议；列类型从 `ColumnTypes()` 获取。go-sql-driver 钉在 v1.7.1（M5 F5.4 审查时定）：v1.8 起文本协议下也把整数、浮点数解析成 Go 数值，database/sql 再转回字符串，和服务端原文不一样（double 1500000 变成 `1.5e+06`，float(10,2) 的 1.50 变成 `1.5`，zerofill 的 007 变成 `7`），显示不对、保存永远报「行数据已变化」，v1.10 也没有关掉的开关。驱动加了开关或者改用原始字节读时再升级，go.mod 旁用 `ponytail:` 注释写明。
- **SQLite**（M14 F14.1，`modernc.org/sqlite`，无 cgo）：DSN 是文件路径（`~` 展开，相对路径按启动目录；`?mode=ro` 等 URI 参数照传给驱动）；路径不存在时报错、不新建空文件（打错路径不该生出一个库；要新建在路径后面带 `?mode=rwc`，如 `x.db?mode=rwc`，不认 `file:` 前缀，决策者定）。类型是动态的，`Col.Type` 用声明类型（可能为空），值走文本：REAL 驱动给的是 float64，显示最短往返（`0.3333333333333333`，SQLite 自己 cast 是 17 位），保存时的 `Same` 按数值比、不比文本（审查时在真文件上 600 次保存 124 次误报「行已变」）；DATE / DATETIME / TIMESTAMP 声明的列 modernc 一律解析成 time.Time（没有开关），按 SQLite 的标准文本 `YYYY-MM-DD HH:MM:SS[.SSS]`（UTC、不带时区）显示，`Same` 用 `julianday(col) = julianday(?)` 比，原文的其他写法（只有日期、`T` 分隔、`.000`）显示时丢掉，`ponytail:` 标出（审查时定，备选是换驱动或上游加开关）；blob 是 `<binary N bytes>`；行标识里有 blob 列（UUID 存 blob(16)）时整表只读并给原因，同 MySQL 的二进制键。行标识：主键（rowid 表里不是 `INTEGER PRIMARY KEY`、又没声明 NOT NULL 的主键列可以是 NULL，这种主键不算，复审时定），没有就可用的唯一索引，再没有就 `rowid`——这时表格最前面多一列只读的 `rowid`（`Columns.Lead` / `Query.Lead`，取数写 `select rowid, *`）；视图和 WITHOUT ROWID 的表（必有主键）不走这条。INSERT 里 `DEFAULT` 的列直接从列清单去掉（三种引擎等价，SQLite 的 VALUES 里没有 DEFAULT 关键字）；UPDATE 里 SQLite 没有 DEFAULT，单元格选项里不提供（`Column.AddOnly`）。取消把 ctx 传给驱动，没有服务端可 KILL。手动事务模式下 `pragma`、`attach`、`detach` 和 `vacuum` 一样不补 BEGIN（事务里 pragma 静默无效、`journal_mode` 直接报错，审查时定）。树：文件 → Tables / Views → 列，没有 schema 层；表的行数不显示（没有估计值）。
- **SQL Server**（M18 F18.6，`github.com/microsoft/go-mssqldb`，纯 Go；用户 2026-10-20 定，排在 Windows 之后一起做）：`engine = "mssql"`，DSN 是驱动的 `sqlserver://user:pass@host:1433?database=x` 形式，`encrypt` / `TrustServerCertificate` 等参数原样传给驱动（本地容器是自签证书，要 `TrustServerCertificate=true`）。`Quote` 是 `[s].[t]`，`Arg(n)` 是 `@p<n>`。catalog 走 `sys.*`：`sys.schemas`（去掉 `sys`、`INFORMATION_SCHEMA`、`guest`、`db_*`）、`sys.tables` / `sys.views` 加 `sys.dm_db_partition_stats` 的行数估计、`sys.columns` / `sys.types` / `sys.identity_columns`、主键和唯一索引 `sys.indexes` / `sys.index_columns`、外键 `sys.foreign_keys`、函数 `sys.objects`（FN / IF / TF）加 `OBJECT_DEFINITION`、`sys.sequences`；`Databases` 查 `sys.databases` 里 `HAS_DBACCESS(name) = 1` 的、去掉四个系统库（`master` / `tempdb` / `model` / `msdb`），子 Conn 换库同 PG（DSN 的 `database` 参数）。**没有 search_path**：`UseSchema` 无事，query 和快速 SQL 用 `[schema].[table]` 全名，§8.6 的「当前 schema」是 `SCHEMA_NAME()`、只影响补全和不带前缀的表名。表的 DDL 没有服务端的 `SHOW CREATE`，按 catalog 拼最小的 `CREATE TABLE`（列、类型、NULL、主键）并注明是拼的；视图和函数用 `OBJECT_DEFINITION`。`Exec` 一批一次（驱动一次能发多条语句，多个结果集用 `NextResultSet` 收齐），`GO` 在客户端切开（§9）；取消把 ctx 给驱动（attention 包），在真库上验过才算；只读连接没有只读事务，靠 §13 的应用层拦截（同 Redis）；事务 `BEGIN TRAN` / `COMMIT` / `ROLLBACK`，收尾走共用的 `db.End`。类型的文本以集成测试为准：`datetime2` / `datetime` / `datetimeoffset` / `date` / `time`、`uniqueidentifier`（标准 GUID 文本）、`bit`（`true` / `false`）、`money` / `decimal`（文本）、`varbinary` / `image`（`<binary N bytes>`）、`xml` / `nvarchar(max)`（文本）。`CaseInsensitive` 为真（默认排序规则；分大小写的排序规则是已知上限）。不做：Windows 集成认证、Always Encrypted。
- **已知上限**：MySQL 中非 UTF-8 的 blob 显示为 `<binary n bytes>`；PG 的 bytea 按服务端返回的 `\x…` 文本显示。MySQL 的 BIT 列文本协议返回原始字节（bit(1) 的 0 / 1 是 `\x00` / `\x01`），按大端字节解码成数字显示，不能编辑（F5.4）。

### 8.2 连接模型

每个 session 的每个连接（`Conn`，§5「一个 session 多个连接」）用到时建两条：`Meta` / `Main`，这个连接上的 query 共用 `Main`（M5 起；原来是每个 session 两条，分工不变）。每条连接由一个 `db.Worker` 独占，用互斥锁保证同一时刻只跑一个请求，因为连接对象不能并发使用。`tea.Cmd` 本来就各在自己的 goroutine 里执行，所以 Worker 不再单开 goroutine 和 channel；请求的先后由 tab 的 `seq` 兜底（§8.3）。`Worker.Cancel()` 取消当前请求的 ctx。

**启动**：启动时的连接（`sqlmux <名字>` 指定的，不带参数时是第一个）M12 F12.5 起和其他连接一样在 Cmd 里连（先 `Main` 后 `Meta`，每条最多 10s），界面立刻进入、节点显示连接中；失败标在树上、进 toast，它的 tab 留着显示错误页（§14「恢复」）。原来是 `main` 里同步建连、两条都连上才进入界面、失败退出。其他连接在用到时（树里展开、query 选它、新 session）同样在 Cmd 里连。连接失败后在它的 query 里执行、query pane 按 `R`、表 tab 错误页上 `R` 都重拨（同 fetch）；query 里的这一次执行只重拨、语句不跑，连上后再执行一次才有结果（M12 节点 2 测试时定为第一版的行为，连上后自动重跑等有人要再做）；只有拨号还在进行时才 toast「<连接名> 还没连上」（审查时定）。

| 连接 | 用途 |
|---|---|
| `Main` | query 执行、保存修改、manual 事务模式或在事务中时的表格读取；持有事务状态 |
| `Meta` | schema 树、列属性、DDL 预览、计数、快速 SQL；auto 事务模式下的表格读取 |

- 拆成两条的原因：query 里跑长查询时，树、命令面板和表格浏览不会跟着卡住。
- manual 事务模式下，或者 `Main` 正在事务中时（用户在 query 里自己 begin 了），表格读取改走 `Main`，这样能看到自己还没提交的修改（F5.3）。代价：
  - 表格取数要排在这个连接 query 的执行后面，「表格浏览不会跟着卡住」在这时不成立；
  - 在事务中的取数包在 `SAVEPOINT sqlmux_page` 里，WHERE 写错或取消只撤到 savepoint，不让整个事务进入出错状态（F5.3 worker 加、审查认可）；不在事务里时不发 SAVEPOINT；
  - WHERE 跑在可写的 `Main` 上，调用了写数据的函数（如 `nextval`）就真的写了；只读本来就不是权限边界（§13）。计数仍在 `Meta` 上，看不到没提交的增删（ponytail）。
- **MySQL 的事务状态**（F5.4 审查时定，MySQL 8.4 上实测）：MySQL 拿不到 TxStatus，按执行过的语句记。begin / start transaction 记为在事务中；commit、rollback（不含 rollback to），以及手册「Statements That Cause an Implicit Commit」列出的语句（TEMPORARY 以外的 DDL、账号管理、LOCK / UNLOCK TABLES、ANALYZE / OPTIMIZE / REPAIR / FLUSH、RESET、再一次 BEGIN / START 等）执行后记为不在事务中；死锁（1213）时 MySQL 回滚整个事务，也记为不在事务中，并 toast「死锁，整个事务已回滚」。否则状态记错之后，手动模式不再补 BEGIN、取数和保存的 savepoint 报不存在，已经提交的写入还被报成「已回滚」。其余记不准的情况（如在存储过程里提交）是已知上限，代码里用 `ponytail:` 标出。不另用 `SET autocommit = 0`：隐式提交之后状态照样要自己记。
- **断线重连**（M8 F8.19，用户 2026-10-08：经 `tsh` 代理连公司的库，代理经常断开又自动恢复，sqlmux 要跟着重连；原来断开的连接不自动重连，MySQL `wait_timeout` 8 小时空闲断开后也要重启 sqlmux）：
  - **判定**：`Meta` / `Main` 上的一次请求以连接级错误失败——PG 是 `pgconn` 报的 `conn closed`、`broken pipe`、`EOF`、`connection reset`，或 `PgConn().IsClosed()`；MySQL 是 `driver.ErrBadConn`、`invalid connection`、同样的网络错误——就把这条连接标成断开。SQL 错误（语法、权限、约束）不算。
  - **重连**：Worker 在同一个 goroutine 里立刻重拨，用**当前**配置的 dsn 和密码来源（F8.19 审查时定：连接管理里改了名或 dsn 之后，旧 Spec 的 password_cmd 指着已删的钥匙串项、旧地址，会永远拨不通），engine 和 read_only 沿用建连时的（界面上的只读状态记在 Conn 上，§7.8 连接管理）；`password_cmd` / `${VAR}` 再取一次密码，超时同建连，成功后重放会话状态：建连参数本来就带 DateStyle 和只读（RuntimeParams，§8.2），query 选过的 schema / search_path 要再 `set` 一次（§8.6「Schema」记在 query 上，不是记在连接上，重放的是这条连接上最后一次设置的值）；正在进行的事务（手动模式的，或用户自己 `begin` 的）已经随连接丢了：事务状态回到空闲，**模式不变**（手动还是手动——改回自动的话用户的下一条 update 会被悄悄自动提交，更危险；F8.19 时定），toast「<连接名>：连接断开，事务已丢失」。**事务丢失时可能写的请求一律不跑**（F8.19 审查时定）：失败的那条 query 写语句、表格保存不重跑，返回「连接断开，事务已丢失，这条没有执行」；`commit` 没发出去时报「事务已丢失，没有提交」，**已发出、回复前断开的报「连接断开，提交结果未知：请先查一下数据」**（服务端可能已经提交，这时说「已丢失」会让用户整个重做一遍、变成双写）。表格保存的 COMMIT 和 query 里手写的 `commit` 都适用（M13 F13.1，审计发现 worker 看不见本请求里自己开的事务，把它报成了普通错误加「已回滚」）：提交结果未知时不拼「已回滚」，表格按事务丢失那条路自动重载（底下的值已经是库里的，修改标记保留在上面），toast「<连接名>：连接断开，提交结果未知：已刷新，请核对」（M13 F13.1 复审时定：重载就是用户本来要做的事）；`rollback` 两种结果都等于回滚，不区分；query 里手敲的 `commit`（含 `commit work` 等写法）和 `rollback` 同样按这两条处理，不走「这条没有执行」的固定文字（F8.19 复审时定）；新连接上 commit 只会得到一个 WARNING，界面上看起来像提交成功了，其实是半个事务，所以不跑。只读请求照常。事务丢失时同时重载目录和表格（同死锁的处理，手动模式下表格还显示着丢掉的未提交数据）。整个事务由用户重做。
  - **判定分三档**（F8.19 时定）：不是连接错误；连接丢了但请求没发出去（PG `pgconn.SafeToRetry` 且不是 `conn closed`——pgconn 读回复出错时会先关掉连接再报 `connLockError "conn closed"`，它的 SafeToRetry 恒为 true，可写语句可能已经在服务端提交、只是回复丢了，当「没发出去」重跑就是双写，F8.23 审查时在真库上复现；MySQL `driver.ErrBadConn`）——这档写语句也能重跑；实测只有代理 RST 断开才落在这档，空闲连接被 `pg_terminate_backend`、`KILL`、`wait_timeout` / `idle_session_timeout`、代理 FIN 断开后，下一次请求拿到的都是「已发出」，所以空闲断开后的第一条 update 会提示「请重新执行」，不自动重跑（要做到得每次请求前多一次探测往返，不值）；连接丢了、不知道服务端执行了没有（网络错误、`conn closed`、PG 的 57P01 / 57P02 / 57P03 和 08 类 PgError）——这档只有只读才重跑。请求前先问一下连接是否已关（PG `IsClosed`），已关就先重拨，取消 5 秒后断开的连接不让用户先吃一个错误。
  - **重试**：重连成功后，失败的那次请求只在**安全**时自动重跑一次：`Meta` 上的全部请求（catalog、取数、计数、补全的查询）和快速 SQL（只读事务，§9.6）都安全；query 的语句（读写都一样）只在「没发出去」时重跑：IsRead 看不出 `select nextval(…)`、`select setval(…)`、`select get_lock(…)`、用户自己写的会 insert 的函数这些副作用，Main 又不是只读连接，「已发出」的 select 再跑一遍就是双写（审查时在真库上实测）；表格保存按三档：没发出去的重跑（自动提交模式下的保存是一个完整事务，整体重跑是安全的），发出去了不知道执行没有的不重跑：错误栏显示原来的错误加「，已回滚」（保存自己 BEGIN 的事务随连接丢了，UPDATE 跑过也回滚了，所以不说「这条没有执行」），toast 同用户自己的事务丢失「<连接名>：连接断开，事务已丢失」并重载表格，修改标记还在、再按一次 `C-s` 是安全的（M13 F13.1 审查时定：worker 层分不清保存的 BEGIN 和用户的 begin，原来这一档 toast「已重连，请重新执行」）；事务丢失时按上一条一律不跑。一次执行里多条语句时，「没发出去」只对第一条成立：前面的语句已经自动提交，整个请求不能重跑。重跑再失败就按普通错误报，不再重连（每次请求最多重连一次）。Redis 控制台的例外见 §8.7「连接」：服务端标了 `readonly` 的命令「已发出」也重跑。
  - **重连失败**：树上的连接节点转成失败状态（`error` 色，§7.8），toast `<连接名>：<错误>`，tab 都留着；下一次请求再试一次拨号，所以 tsh 恢复之后用户随便操作一下就回来了。不做后台定时探活、不做指数退避：没有请求时断不断开没有影响。
  - 原来 §8.3 里「取消 5 秒后断开的连接不自动重连」和 MySQL 的空闲断开都由这条覆盖。
- `Meta` 建连后设为只读：PG 设 `default_transaction_read_only = on`（和 DateStyle 一样放在 RuntimeParams 里，效果等同 `SET SESSION CHARACTERISTICS AS TRANSACTION READ ONLY`），MySQL 执行 `SET SESSION TRANSACTION READ ONLY`。`Meta` 上只有读操作，写入只走 `Main`，所以 WHERE 里就算调用了会写数据的函数，也改不了数据。和 §13 一样，这是为了防误操作，不是权限边界。只读连接（`read_only = true`，§13）的 `Main` 也放同样的 RuntimeParams（M5 F5.3），`RESET` / `DISCARD ALL` 之后仍是只读。

### 8.3 取消、超时、过期响应

- **取消**：当前连接（§5，M5 起；原来是本 session）忙碌时（`Main` 上的 query 执行，或 `Meta` 上的表格取数），状态栏在模式块左边显示 ` busy · C-c 取消 `（`warn` 色，键位文字从 keymap 读），点击执行 Action `cancel`。`C-c` 在当前连接有请求执行中时取消它，空闲时照旧是连按两次退出（§6.8）。取消后 toast 显示「查询已取消」，tab 保留之前的数据，第一次打开就是空表。取消按连接算，不按 tab：手动模式下表格取数排在 query 的长查询后面时，在表格上按 `C-c` 取消的是 query 的语句，事务因此进入出错状态，只能回滚（同 psql 在事务里按 Ctrl-C；表格自己的取数在 savepoint 里，取消它不影响事务，§8.2）。按 tab 单独取消要给 Worker 的每个请求一个 ctx，暂不做。
  - PG：取消请求的 ctx。pgconn 默认的 `DeadlineContextWatcherHandler` 会在 context 取消时给连接设 deadline，连接随之断开。每个连接只有两条长连接，所以改用 `CancelRequestContextWatcherHandler`，由它发 CancelRequest，取消之后连接还能继续用。`DeadlineDelay` 取 5s：取消发出 5 秒后服务端还没停，就断开连接；断开后由「断线重连」（§8.2，M8 F8.19）在下一次请求时重连。
  - MySQL（M5）：`Query` 自己监听 ctx，取消时另开一条临时连接执行 `KILL QUERY <connection_id>`，id 在建连时记录。只取消 context 的话，驱动会关掉连接，但服务端上的查询会继续跑。KILL 发不出去或没生效时（库被删、连接数满、网络断）的兜底（M14 F14.6，原来是已知上限，Worker 一直锁到语句自己结束）：驱动拿到的 ctx 在 KILL 发出 5 秒后取消（同 PG 的 cancelGrace），驱动关掉连接，Worker 按 §8.2 断线重连，下一个请求重拨。快速 SQL 的 rollback 两个引擎都走 `db.End`（M13 F13.6）。
- **超时**：建连 10s。计数查询 3s，用 ctx 超时实现：取消由 `CancelRequestContextWatcherHandler` 发出，返回 `context.DeadlineExceeded` 后显示 `?`（用户按 `C-c` 取消返回的是 `context.Canceled`）。不用 `SET LOCAL statement_timeout`：那要 begin / set / count / rollback 四次往返，且必须在 Worker 同一把锁里执行，否则别的请求会插进事务，等于要给 Worker 加 Tx；ctx 的做法引擎无关，MySQL 也能用。计数用 `tea.Sequence` 排在取数之后，3s 从页面数据回来后才开始算；计数不算 busy，不显示忙碌提示。
- **过期响应**：每个 tab 维护一个递增的 `seq`，请求时带上。结果回来时 `seq` 已经不是最新的就丢弃，避免快速翻页时旧结果覆盖新结果。

### 8.4 元数据

| 内容 | PostgreSQL | MySQL |
|---|---|---|
| 表列表、行数量级（D-01） | `pg_class.reltuples` + `pg_namespace` | `information_schema.TABLES.TABLE_ROWS` |
| 列属性：类型、可空、默认值、主键 | `pg_attribute`（`format_type`、`attnotnull`、`pg_get_expr(adbin, adrelid)`）+ `pg_constraint` | `information_schema.COLUMNS` + `KEY_COLUMN_USAGE` |
| 所有列都不可空的唯一索引（用于定位行，见 §10.1） | `pg_index`，条件见表下 | `information_schema.STATISTICS` 中 `NON_UNIQUE = 0`，并要求每列的 `IS_NULLABLE = 'NO'` |
| 枚举值（§10.2） | `pg_type.typtype = 'e'` 时查 `pg_enum` | 解析 `COLUMN_TYPE`，如 `enum('a','b')` |
| DDL 预览（K-05） | 用 `format_type`、`pg_get_constraintdef`、`pg_get_indexdef` 拼出，末尾带 `comment on table / column … is '…';`（`obj_description` / `col_description`，M9 F9.8），不要求与 pg_dump 一致 | `SHOW CREATE TABLE` |

PG 的几处细节（M1 核对时对照 lazysql、pgtui 确认，两者各有问题）：

- **表列表**：`relkind IN ('r','p','v','m','f')` 且 `NOT relispartition`，分区的子表不列出，只列父表。系统 schema 用 `nspname !~ '^pg_'` 排除；`NOT LIKE 'pg_%'` 里的 `_` 是通配符，会误伤 `pgx_data` 这类 schema。
- **行数量级**：`reltuples < 0`（从没 analyze 过，或者是分区表的父表）时显示为 `?`。
- **列属性**：不用 `information_schema.columns`，它会漏掉没有权限的列，数组和枚举的类型也只显示成 `ARRAY`、`USER-DEFINED`。
- **主键**：列按 `unnest(conkey) WITH ORDINALITY` 的顺序排列。
- **可用于定位行的唯一索引**：`indisunique AND indisvalid AND indpred IS NULL AND 0 <> ALL(indkey)`；只看前 `indnkeyatts` 个键列（INCLUDE 列不算），并要求这些列都是 `attnotnull`。
- **索引、键、函数、序列**（M14 F14.3）：引擎接口加 `Indexes` / `Keys`（按表）和 `Functions` / `Sequences` / `FunctionDef`（整库）五个方法，不支持的返回 nil；PG 走 `pg_indexes` / `pg_constraint` / `pg_proc` / `information_schema.sequences`，MySQL 走 `information_schema.statistics` / `key_column_usage` / `routines`，SQLite 走 `pragma index_list` / `index_info` / `foreign_key_list`。树上的画法见 §7.8「对象节点」。
- **枚举值**：`array_agg(enumlabel ORDER BY enumsortorder)`，按列的 `atttypid` 关联。

catalog 按连接缓存（每个 session 的每个 `Conn` 一份，M5 起；原来按 session）。query 执行 DDL 后（由 §9.3 的判定得知）或用户执行刷新命令时，重新加载。

**加载时机**：Init 时在 `Meta` 上异步查两样，各一条查询：非系统 schema 的列表，以及这些 schema 里所有表的名字和行数估计。M17 F17.13 起 schema 列表照查全部（勾选浮层要列总数），表、视图、函数、序列的查询只查连接里 `schemas` 勾了的（§14；没写 = 全部），按名字 `n.nspname in (…)`（`Quote` 加引号）过滤——prod 上 `agentable` 2428 张表不勾就不拉。列属性、主键、唯一索引、枚举按表查，第一次打开这张表时才查，查过就缓存，刷新时清掉。这样大库启动时不用把整张 `pg_attribute` 读回来。

- **数据库列表**（M15 F15.1）：`Engine.Databases(ctx, c)`：PG 查 `pg_database`，去掉 `datistemplate`、`datallowconn = false` 的，再按 `has_database_privilege(datname, 'CONNECT')` 过滤；MySQL、SQLite 返回 nil（它们没有这一层，§5）。展开连接时随 catalog 一次读，`R` 刷新。M17 F17.13 起树只列连接里 `databases` 勾了的（§14），列表本身仍是全部（勾选浮层要列总数）。
- **SSH 隧道**（M15 F15.2，connections.toml 的 `ssh = "user@host"`，§14）：不用 `x/crypto/ssh`（agent、`~/.ssh/config`、ProxyJump 都得自己接），每次拨号起一个本机的 `ssh -W host:port user@host`（`-o BatchMode=yes -o ExitOnForwardFailure=yes`），它的 stdin / stdout 当 `net.Conn` 接到 pgx 的 `DialFunc` 和 mysql 的 `RegisterDialContext`；没有本地端口、没有清理竞态，连接关了进程退，重拨再起一个。要密码或口令的 ssh 在 TUI 里没法交互：BatchMode 下 ssh 失败，它的 stderr 原文进连接错误，文档里写「先 `ssh-add`」（worker 起草时提出、决策者同意：终端用户本来就用 ssh agent）。命令行的临时连接（URL）不支持 ssh；SQLite 忽略这个字段。

### 8.5 分页与计数

- **SQL Server**（M18 F18.6）：`OFFSET @p1 ROWS FETCH NEXT @p2 ROWS ONLY`，必须带 `ORDER BY`，没有排序列时 `ORDER BY (SELECT NULL)`；多取一行判断下一页照旧；计数 `COUNT_BIG(*)`。

**取数**：`SELECT * FROM t WHERE … ORDER BY … LIMIT n OFFSET m`。

- 每页默认 100 行，LIMIT chip 可选 100 / 500 / 1000。100 与 PRD 快速 SQL 的 limit 100 一致。
- 每次多取一行（`LIMIT n+1`），用来判断还有没有下一页。这样计数还没回来或者超时的时候，翻页也能正确停在最后一页。

- 默认按「行标识列」排序（§10.1：主键，没有主键时用所有列都不可空的唯一索引），保证分页结果稳定。
  - 两者都没有时，不加默认排序，用户可以通过 ORDER 指定排序列。
- 总是取全部列，列的显示与隐藏只在客户端控制，所以切换 COLS 不需要重新查询。已知上限：列很多或含大 JSON 字段的表会多传数据，以后可以改成只查可见列。

**计数**：在 `Meta` 上异步执行 `count(*)`。

- 超时则显示 `1/?`。
- 没有 WHERE 条件、且估计行数超过 100 万时，不做精确计数，用估计值显示 `~n`。

**已知上限**：OFFSET 翻到很深的页会变慢，以后可以改成 keyset 分页。

### 8.6 PG 的 schema 与 search_path

**背景**：

- PG 的一个数据库里可以有多个 schema，例如 `doraemon.public`、`doraemon.agentable`。
- SQL 里不带 schema 前缀的表名，会按连接的 `search_path` 设置依次查找，默认是 `"$user", public`。
- 所以，如果树里正在看的是 `agentable`，而 query 里写的是 `select * from tbl_xxx`，PG 默认只会在 `public` 里找这张表，结果报「表不存在」；更糟的情况是 `public` 里恰好有同名表，查到的就是另一张表。

**方案**：与 DataGrip 一致，每个 query 可以单独选择 schema。

- **入口**：query pane 标题的右侧显示 `<连接> · <库名>.<schema> ▾`（M5 F5.11 起加上连接；库名取连接的 database，记在 `Conn` 上），位于 `▶ run ↵` 旁边；连接那一段见 §7.8「query 标题」。
  - 点击它，或在 query 的 NORMAL 模式下按 `gs`，打开 schema 下拉框。
  - 下拉框列出当前库的 schema，系统 schema（`pg_catalog`、`information_schema` 等）不列出。顶部有过滤输入框，支持模糊匹配（§9.7）。
  - 下拉框照命令面板的做法：打开后输入直接进过滤框（模式为 COMMAND，§7.8），`C-n` / `C-p` / `↑` / `↓` 移动，`↵` 选中，`esc` 关闭；`j` / `k` 会被当成输入，所以不用。鼠标：点选，点浮层外部关闭，悬停 `row` 底。
  - 位置在入口（query 标题的 schema 按钮、查询条的 chip）下方、与入口左对齐，宽度取 max(最长 schema 名 + 边距, 入口所在栏的右边界 − 入口起点 x)，这样右边框和所在栏的右边框对齐；仍放不下时向左移。当前所在的 schema 用 `pk` 色标出（§7.3 里 pk 也用于 schema 值）。键位在 `[keys.dropdown]`，Action 为 `dropdown.up` / `dropdown.down` / `dropdown.select` / `dropdown.close`，LIMIT 下拉框共用（F1.2 时叫 `schema.*`，F1.4 改名；ORDER M12 F12.9 起是 COLS 式的浮层，§7.6），只在浮层里用，不带标题。
  - 组件放在 `ui` 里，M1 用于 ORDER（到 M12 F12.9 为止）、LIMIT，M3 给 query 选 schema。树不再用它：M1 F1.12 起 schema 是树里的节点（§7.8）。
  - 命令面板里也有对应的「Switch schema…」命令。
- **默认值**：新建的 query（`c`、引导页按钮、面板命令、`C-e`）一律用当前连接和这个连接的 Schema（§5、§7.8），不管这个 query_n 文件在 state 里记着什么，并把它写回 state；之后两者互不影响。state 里记下的连接和 schema 只在启动时恢复默认布局里的 query_1 时用（M6 F6.5 恢复布局时也用，§11）。
- **执行方式**：同一个连接上的 query 共用 `Main`，所以每次执行前比较一下（`Conn` 记下 `Main` 当前的 search_path，初始为建连时读到的值；一次执行里有 SET / RESET / DISCARD 语句时把记下的值作废，下次一定重新 SET。代价是用户在 query 里自己 `set search_path` 只持续到这次执行结束，以下拉框为准）：如果连接当前的 `search_path` 与这个 query 选择的 schema 不一致，先执行 `SET search_path TO <所选 schema>, <建连时的原始 search_path>`。
  - 原始路径在建连时用 `SHOW search_path` 读取，接在后面，这样装在 `public` 等 schema 里的扩展函数仍然能找到。设置时用 `set_config('search_path', $1, false)` 带参数的写法，不把原始值拼进 SQL：服务器上设了 `search_path = ''`，或者 DSN 里写了 `$user,public` 时，拼接会报语法错（快速 SQL 已经这样写）。
  - **只在事务外记住 search_path**（M3 审查时在真 PG 上复现）：
    - `Main` 处在出错的事务里时（pgconn 的 TxStatus 为 `E`），跳过 SET 直接执行，并清掉记下的值。否则 SET 本身报 25P02，按「SET 失败时后面的语句不执行」，用户的 `rollback` 永远执行不到，session 卡死到重启。
    - 做 SET 的那一刻或者执行结束时，只要有一个时刻连接处在事务里（TxStatus 不是 `I`），就不记这次的 SET，下次一定重新 SET。否则事务里做的 SET 被用户的 `rollback` 撤掉之后，记下的值还当它在，下一次悄悄按别的 schema 查表。只看结束时的状态不够：SET 做在事务里、这次执行又以 `rollback` 结束时，结束时已经在事务外，SET 却已被撤掉（M3 复审时在真 PG 上复现）。
  - **schema 被删掉之后**：PG 接受不存在的 schema，SET 照样成功，只是找表时跳过它。catalog 重新加载后，选中的 schema 已经不存在的 query 退回这个连接的 Schema，不弹提示，同树退回 `current_schema()` 的做法。
  - 已知上限：用 `select set_config('search_path', …)` 改 search_path 不会让记下的值作废（首词是 select），代码里用 `ponytail:` 标出。
- **影响范围**：query 的补全以它自己选择的 schema 为准。表格查询始终带 schema 前缀，不受影响。

MySQL 的 schema 就是 database（M5 F5.4 定，同 DataGrip）：树里 MySQL 连接下面一层列 database，相当于 PG 的 schema 层，不列 `information_schema`、`mysql`、`performance_schema`、`sys`；query 标题是 `<连接> · <database> ▾`，执行前和连接当前的 database 不同就先 `USE`。`USE` 是会话级的、不受事务影响，用不着上面「只在事务外记住」那一套；一次执行里有 `USE` 语句时把记下的值作废，下次重新 `USE`。PRD 原来写切换 database 会新建 session，M5 起不再这样（§18）。

### 8.7 键值引擎（Redis，M16）

Redis 不塞进 `db.Engine`（那是 SQL 形状的接口：Quote、Arg、Tables、TableColumns、Quick），也不用 `sqlkit`。它接进来的三处：`db.Conn`（连接和控制台）、`db.Source`（键 tab，§5「接口缝」）、树（本节「树」）。参考 RedisInsight 的 Browse 和 DataGrip 的 Redis 支持，目标是比它们展示更清楚、JSON 更友好、键盘能走完。

- **连接**（F16.1）：go-redis v9，`dsn = "redis://user:pass@host:6379/0"` / `rediss://`，用 `redis.ParseURL` 解析（db 号在路径里，`?db=` 也认，交给它）；`password_cmd` / `password_env` / 钥匙串和 SQL 连接一样，取到的密码盖过 URL 里的；`ssh` 同 §8.4 的隧道（go-redis 的 `Dialer`）。每个 session 的每个 Redis 连接仍是 `Meta` / `Main` 两条（§8.2 的分工：`Meta` 做 SCAN / TYPE / TTL / `COMMAND DOCS` / 键 tab 的读，`Main` 跑控制台和写回），每个 db 一对（同 F15.1 的 PG 数据库：子 `Conn`，URL 路径改成 `/N`，不用 `SELECT` 切库），底下各是一个 go-redis 的 `Conn`（单条连接，`PoolSize` 1，不用它的连接池：Worker 已经是一条连接一个互斥锁，连接态命令要落在同一条上）。`HELLO 3`，Redis 6.0 以下报「需要 6.0+」。`redis` 包实现 `db.Conn`（§5「连接缝」的 `db.Driver` 是它的 Connect / Broken / ErrorOf，`ErrorOf` 把 `WRONGTYPE …` 的第一个词当 Code）：`Exec(ctx, text, maxRows)` 把文本按行拆成命令（§9.8）逐条跑、每条一个 `Result`，`Query(ctx, cmd, args...)` 跑一条命令、参数原样作为命令的参数（Redis 的参数本来就都是字符串，`Val.Null` 不出现），`Close` 关连接；这样 `db.Worker` 的互斥、取消、`seq`、断线重连（F8.19）全部照用。回复摆成 `Result` 的规则在 §11「Redis 控制台」。`Info`：`INFO server` 的 `redis_version`、`redis_mode`，状态栏地址块照旧（§7.8）。测试按钮发 `PING`。只读连接：`Main` 不能在服务端设只读，靠 §13 的应用层拦截。**断线重跑**（F16.4 复审时定）：Redis 没有事务态要管（§8.7 表格的 MULTI 不是交互式事务），控制台的一次执行里每一行的命令都带服务端 `COMMAND INFO` 的 `readonly` flag（`GET`、`SCAN`、`TTL`、`DBSIZE`、`EVAL_RO` 都带）时，「已发出」也重跑，同 §8.2 的 `Meta` 请求；判定用正面证据，不用「不是写」：`PUBLISH` / `SPUBLISH`（flags 里没有 write 也没有 may_replicate）重跑就发两次，`EXEC` 写的是队列里的命令、新连接上重跑报 `EXEC without MULTI`、用户看不出事务可能已经提交，照「不是写」判定两个都会重跑，按 `@pubsub` / `@transaction` 逐个排除以后还会漏。`PING`、`INFO` 这类不带 `readonly` 的不重跑，照常「已重连，请重新执行」。SQL 的 query 不变。
  - **阻塞命令**（BLPOP、BRPOP、XREAD BLOCK、SUBSCRIBE、MONITOR …）会占住 `Main`：取消走 ctx，go-redis 取消不了已经发出的阻塞命令，所以取消时关掉这条连接重拨（F8.19 的路，事务状态对 Redis 不存在，不用报「事务已丢失」），同 §8.3 PG 的 5 秒断开。
  - **断线判定**：go-redis 的 `net.Error`、`io.EOF`、`redis: client is closed`；`LOADING`、`MASTERDOWN`、`READONLY` 这类服务端错误不算。重连没有会话状态要重放（db 在 URL 里）。
  - **不做**：Cluster、Sentinel（go-redis 有，等第二轮）；ACL 的用户名在 URL 里照传。
- **树**（F16.2，§7.8「Redis 的树」）：**只用 SCAN，永远不用 KEYS**。展开 db 节点时在 `Meta` 上 `SCAN cursor MATCH <模式> COUNT 1000` 循环到游标归零或扫够 `[redis] scan_keys`（默认 10000）个，每批键用一条 pipeline 的 `TYPE` 取类型；结果记在 `Conn.Catalog`（键名、类型、所在 db），按分隔符切成命名空间树。侧栏的 `/` 过滤改成重新扫（`MATCH`），同一预算；预算用完的提示是键列表末尾的一行（§7.8，db 节点的尾注仍是 `DBSIZE`）。`DBSIZE` 给 db 节点的总数。`tree.refresh` 重扫光标所在的 db。命名空间删除：先按同样的 MATCH 扫出键名（这时不设预算，用户确认的数字要是真的），再每 500 个一条 `UNLINK`，走 `Main`；`C-c` 能停，中途失败或取消都 toast 删了多少。
- **键 tab**（F16.3）：打开一个键是一个 `kind = "key"` 的 tab（§5）。查询条上方多一行**信息行**（信息行、MATCH 输入框、芯片行三行；F16.3 时定，原来写「在查询条的位置」）：顺序 TYPE（图标和类型名）、LEN（`STRLEN` / `HLEN` / `LLEN` / `SCARD` / `ZCARD` / `XLEN`）、TTL（人话化 `12m03s`，`-1` 显示「不过期」）、ENC（`OBJECT ENCODING`）、MEM（`MEMORY USAGE`，每页随 `Info` 取一次，一条便宜的命令）、RENAME、DELETE，窄了从右边裁；`R` 刷新整行；行上的操作：设 TTL（`EXPIRE` / `PERSIST`）、改名、删除，各自弹现成的输入框 / 确认框；`C-s` 写回表格的修改。表格按类型，各是一个 `db.Source` 实现：

  | 类型 | 列 | 翻页 | 过滤 / 排序 | 写回（一批包进 `MULTI` / `EXEC`） |
  |---|---|---|---|---|
  | string | 单格 `value`（`e` 开值编辑器，JSON 在里面排版） | 无 | 无 | `SET k v KEEPTTL`；没有新增行 |
  | hash | `field` / `value` | 长度 ≤ `[redis] load_all`（默认 5000）一次 `HGETALL`、本地翻页；超过用 `HSCAN COUNT 1000`，只能「下一页」 | 本地：多列排序、按值过滤、`MATCH` 模式；HSCAN：只有 `MATCH` | `HSET` / `HDEL`；改 field 是 `HDEL` + `HSET` |
  | list | `index` / `value` | `LRANGE` 按偏移 | 无（index 只读，`Generated`） | `LSET`；删除是 `LSET` 哨兵 + `LREM`（Redis 文档的做法）；新增 `RPUSH` |
  | set | `member` | 同 hash（`SMEMBERS` / `SSCAN`） | 同 hash | `SADD` / `SREM`；改成员是 `SREM` + `SADD` |
  | zset | `member` / `score` | 同 hash：长度 ≤ `load_all` 一次 `ZRANGE 0 -1 WITHSCORES`、本地翻页；超过用 `ZRANGE` 按偏移，`REV` 倒序 | ≤ `load_all` 时本地，同 hash；超过时没有过滤和排序，只有正 / 倒序 | `ZADD` / `ZREM` |
  | stream | `id` 加展平的字段（并集，缺的格子空） | `XRANGE` / `XREVRANGE` 按 ID 游标 | 无 | 只 `XDEL` |
  | RedisJSON | 值编辑器（`JSON.GET`；`TYPE` 给的 `ReJSON-RL`） | 无 | 无 | `JSON.SET k $`；没有新增行 |

  行的身份：hash 的 field、set 的 member、list 的 index、zset 的 member、stream 的 ID。写回前的乐观校验同 §10.3：`MULTI` 之前 `WATCH k`，`WATCH` 之后、`MULTI` 之前把要改的格子读回来逐格比 `Old`（WATCH 管不到读页之后、保存之前别人的改动），不符或 `EXEC` 返回 nil（键在 WATCH 后被别人改过）都报「行数据已变化」并重载。没有事务模式切换：Redis 的 MULTI 不是交互式事务，`Conn.Manual` 对它恒为自动。新增行走现有的新增行操作（`HSET` / `SADD` / `ZADD` / `RPUSH`）；string 和 JSON 没有新增行。格子里的 JSON 不按格高亮（`ui.Grid` 按列着色，按格要改绘制和缓存；F16.3 时定不做，第二轮候选），`e` 开值编辑器看排版后的。「以 JSON 查看」（`key.json`，§7.6 的 JSON 视图）：hash 整体变 object、list / set 变数组、zset 变 `[member, score]` 数组、stream 变 `{id, fields}` 数组，只读看，复制为 JSON 同一份。DDL 查看是重建它的命令（`redis.Key.Rebuild`，不在 `Source` 里：`HSET k f v …`、`SET k v EX ttl`，长的截到 `load_all` 并注明），复用 ddlview。二进制值按 §8.1 的占位符。布局文件记 `connection`、`db`、`key`（§14）。导出（`Each`）在 HSCAN / SSCAN 类型上是整键循环，按 `[redis] scan_keys` 同样的预算截断并提示，不另开配置。命令面板的「表」范围在 Redis 连接上这一版不列键（要 SCAN、有预算问题），键只从树和侧栏 `/` 进。
- **已知上限**：控制台里手敲 `SELECT n` 只改这条 Main 的 db，标题和树不跟着变（`ponytail:` 标出）；HSCAN / SSCAN 翻页只能向前、计数是 `-1`、本地排序不可用；stream 的字段并集按当前页算；`WATCH` 粒度是整个键，别人改了同一个 hash 的另一个 field 也会报「行数据已变化」；RedisJSON 只在装了模块的服务端上（`MODULE LIST`，没有时 JSON 类型不出现）。

### 8.8 外部执行器（exec，M17 F17.7 / F17.8）

本地连不上、只有一条命令行能到的库（用户公司的 prod：PG 经 `doraemonctl exec db` 借 pgAdmin 的 Query Tool 面板，Redis 经 `doraemonctl exec redis` 走 metaknow 的内部接口），用引擎 `exec` 接进来：`dialect` 定 SQL 方言或 Redis，`command` 是 argv 模板。

- **只换传输**：`internal/db/exec` 实现 `db.Conn`（`Exec` / `Query` / `Close`）；`dialect = "postgres"` 时 `postgres.Engine` 的 catalog、分页、`Quote` / `Arg` / `Same` / `Eq` 原样复用，只换 `Connect`（`Engine.Schemas(ctx, c Conn)` 这些本来只认 `Conn`）；`dialect = "redis"` 时树、键 tab 的读和控制台走它，`Query(cmd, args)` 一一对应。
- **调用**：每条语句一次子进程，`{sql}`（或 `{cmd}` / `{args...}`）替换后执行；`Query` 的 `$n` 参数由 exec 端内联成字面量（字符串按方言加引号，数字、布尔原样，NULL）；`Exec` 按语句拆开逐条调。取消 = 杀子进程；没有断线重连。exec 连接只在 connections.toml 里手写，连接管理器不列它（表单没有 `command` / `dialect` 字段；删、改名也在文件里做），连通性在连接时验：PG 跑一次 `select current_database()`，Redis 跑一次 `PING`，失败时 stderr 原文就是错误（F17.7 审查时定）。**没有数据库层**：命令连到哪个库由它自己定（pgAdmin 的面板开在哪个库就是哪个库），M17 F17.12 起（用户验收：层级要和别的 PG 连接统一）`Engine.Databases` 返回连接时 `select current_database()` 的那一个名字，树上是一个数据库节点、它是默认库、走连接本身的 Conn；另一个库就是另一条连接（原来返回空、树从 schema 开始）。命令能按库切换时再加 `{db}` 占位符（下一轮候选，§plan）。
- **输出约定**：stdout 一份 JSON，一条语句一项：`columns`（有序的列名）、`rows`（每行一个数组，值是 JSON 标量或嵌套，嵌套转成文本协议的 JSON 文本）、`n`（受影响或总行数，没有就省略）、`err`（这条语句出错时的文本）、`truncated`（true 时这条的行被命令截了：sqlmux 自己发的语句（catalog、分页、计数）由 exec 的 Conn 接着翻——把语句包成 `select * from (<sql>) as sqlmux_page limit 1000 offset <已收行数>` 一轮轮跑，页长取第一批被截时的行数，页不满或空页才停、一页和第一批一样长就接着翻，拼成一份结果（不看后续页的 `truncated`：doraemonctl 对正好 `--max-rows` 行的页不带这个键，worker 2026-10-18 实测；整倍数时多一次空页往返）；决策者 2026-10-18 在 staging 上实测：`agentable` schema 有 4144 张表，Tables 查询按 schema 排序，前 1000 行全是它的，`public` 的表一张都没列出来；最多翻到 100000 行，再多标成已截断；用户 query 的语句不翻，照 AutoLimit 的提示标已截断，F17.11）；多出来的字段忽略；Redis 一项就是回复本身（RESP 的 JSON 形）。退出码：数组出得来就是 0（逐条的错在 `err` 里），只有整个命令跑不了（面板没开、trans_id 失效、opencli 没装）才非 0，stderr 原文就是错误。doraemonctl 的 `-o sqlmux` 是第一个实现（2026-10-18 和它的会话定的口径）：PG 一次子进程约 500 ms（CLI 加浏览器 bridge 往返 250、SQL 120、探针 126），一页 count 加 select 约 1 s；不带 `--no-probe`（探针挡的是面板被切到别的库、编码坏了静默零行这两种事，连接时的一次检查护不住后面每次子进程）；sqlmux 自己写 LIMIT / OFFSET，argv 带 `--max-rows 1000`（pgAdmin 本身 1000 行墙）；Redis 的 argv 带 `--no-parse`（否则长得像 JSON 的字符串会被解成对象）。**已知上限（2026-10-18 用户验收时发现）**：metaknow 的 `/internal/cache` 把回复按 go-redis 的 `Cmd.String()` 字符串化（`scan` 回 `"[68992 [k1 k2 …]]"`，HGETALL 回 map 的 fmt 文本），数组和 map 不可逆，sqlmux 不拆；在它改成返回 `cmd.Result()` 的 JSON 之前，exec 的 Redis 连接只有标量回复（PING、DBSIZE、GET、TYPE）可用，树上的键列不出来。第一次调用或 trans_id 缓存失效时多一轮面板发现（几百 ms 到几秒），正常。**并发**：sqlmux 会同时起多个子进程（`Meta` / `Main` 两个 Worker 各一条、catalog 的几条查询、分页的 count 和 select），命令要自己承受并发；后端吃不住并发的由命令自己排队——doraemonctl 2026-10-18 起对 prod 的 `exec db` 按 pgAdmin session 加 flock 串行（之前并发打同一个面板会把它打坏，报「asyncio Lock is bound to a different event loop」，之后每条都报，只能在 pgAdmin 里关掉那个 Query Tool 面板再开一个），代价是总耗时 = 条数 × 约 0.5 s。sqlmux 不为 exec 单独限流。**下一轮候选**：一次子进程批多条语句（`{sqls...}`，doraemonctl 的 `exec db` 本来就收多条，实测 3 条分开 1349 ms、批着 699 ms），要改分页路径让 count 和 select 走一次。
- **只读**：`read_only` 对 exec 恒为 true；写回、WATCH、阻塞命令、流式导出都不做。每次查询一次子进程往返（几百毫秒到一两秒），PG 翻一页是 count 加 select 两次，浏览可以接受；**Redis 的树在 exec 方言下不对每个键发 `TYPE`**（§8.7 的树是一批 SCAN 后 pipeline 发 TYPE，exec 一条命令一次子进程，一批 1000 个键就是 1000 次），键节点一律画通用的 `key` 图标，打开键 tab 时再 `TYPE`（键 tab 本来就要读类型）；`scan_keys` 预算照旧（F17.8 审查时定）。
- **不内置 pgAdmin 的 REST**：那是它的内部接口，版本一变就坏，维护归 doraemonctl。
