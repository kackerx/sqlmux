# M24 新引擎的评估（2026-11-24）

用户 2026-11-24：书签往后排，下一期接新的数据库引擎，先评估 DuckDB、ClickHouse、Trino 的复杂度。三个 sonnet 子代理各起真容器或真编译做了最小探针；详细报告和探针在仓库外 `/home/kacker/proj/sqlmux-aud/engines/{duckdb,clickhouse,trino}.md`。工作量以 M18 F18.6（SQL Server 引擎，`internal/db/mssql` 约 1900 行含测试）为 1。

## 三个引擎是什么

- **DuckDB**：嵌入式的分析型数据库，「分析界的 SQLite」：一个文件或内存，直接查 Parquet / CSV。数据分析师、写 Python / 数据管道的开发者在本机用。
- **ClickHouse**：列式的分析型数据库服务器，开源、自建或云上（ClickHouse Cloud）。日志、埋点、监控指标这类大表的实时聚合，后端和运维团队常见。
- **Trino**（原 PrestoSQL；AWS Athena 和 Starburst 都基于它）：分布式 SQL 查询引擎，**自己不存数据**，通过 connector 去查背后的 Hive / Iceberg / S3 / PG 等，名字是 `catalog.schema.table` 三层。用户是数据平台和数据工程团队，平时用 DBeaver 或官方 CLI。

## 对比

| | ClickHouse | Trino | DuckDB |
|---|---|---|---|
| Go 驱动 | `clickhouse-go/v2` 纯 Go；或只用标准库 HTTP + `TabSeparatedWithNamesAndTypes`（服务端给文本，不加依赖，PR #20 的做法） | `trino-go-client` 纯 Go（v1.2 起要 go 1.27，现在 1.26.3：钉 v1.1.0 或升 go） | 官方 `duckdb-go` **只能 cgo**；纯 Go 的 purego 方案取消不了正在跑的查询（实测 15 s 跑满） |
| 对发布的影响 | 无 | 无 | 大：`CGO_ENABLED=0` 不再成立，goreleaser 要按平台拆、要老 glibc 容器和 mingw，windows/arm64 没有预编译库；二进制 37 MB → 110 MB |
| 树 | 库 → 表（同 MySQL） | catalog → schema → 表，catalog 正好映射成 `Databases`（同 PG） | 附加的库 → schema → 表 |
| 表格编辑 | **做不了原样的**：没有事务（实验性、要 Keeper），`ALTER … UPDATE` 异步、不报行数，排序键不唯一 | **做不了原样的**：只有自动提交能写，多数 connector 不支持 UPDATE，没有主键 | 能：有事务和主键，乐观校验照搬 |
| 取消 | 好（约 0.5 s 回来，服务端 1.5 s 内杀掉） | 好（2 s 内，服务端 `USER_CANCELED`，连接不丢） | 好（连接不丢） |
| 只读 | `readonly=2` 服务端强制 | 无事务，靠 §13 应用层拦截 | `access_mode=read_only`；同一文件一个进程写，sqlmux 读写打开会锁住用户自己的 Python / CLI |
| 测试环境 | 镜像约 300 MB，冷启动 1.5 s，空闲 150 MB，CI services 能起 | 镜像 2.4 GB，启动 7–9 s，要限内存（不限吃到 15 GiB，`--memory 2g` 空闲 0.8 GiB） | 不要容器（文件库，同 SQLite） |
| 只读 v1 的工作量 | 0.4–0.6（照 PR #20 改）；从零 0.9–1.1 | 0.4–0.5（照 PR #20）；从零 0.6–0.7 | 引擎 0.8，加发布改造 0.7–1.2，合计 1.5–2；之后每次发版都多一份成本 |
| 带编辑 | 再加一条无事务的保存路径（先 `count()` 恰好一行再改，失败只能报「已写入 N 行」） | 同 ClickHouse 那条，1.1–1.3 | 已含 |

PR #20（issue #15 的作者 odilovalisher17 写的，已关）里 ClickHouse 约 1300 行、标准库 HTTP、全部 Engine 方法和集成测试：cherry-pick 到当前 main 只有 6 个文件冲突加一处编译错误，修后单测全过；集成测试 ClickHouse 25.8 全过，26.10 有 3 处失败（错误帧多一行 tag、`primary_key` 文本带括号、一处保存语法错）。Trino 约 780 行加 410 行测试，但依赖 fork 里新增的共用层（Unatomic、Typer、Systemer），和 ClickHouse 混在一个 commit 里；没发 `PARAMETRIC_DATETIME` 头，时间被截成毫秒（一行修）。

## 决策者的建议（待用户定）

1. **M24 先做 ClickHouse 的只读 v1**，照 PR #20 改，release note 里致谢作者。「只读」只指表格不能改格子：照现有「无主键的表只读」那条走，不新开机制；query 里照样能跑 `INSERT` / `ALTER`（自动提交，同 Redis 的 `Conn.Manual` 恒为自动）。表格编辑等有人要再做，和 Trino 共用一条无事务的保存路径。
2. **Trino 排第二个节点，前提是有真实用户要**：照 PR #20 只读，量不大；但受众最窄，测试镜像 2.4 GB、要限内存，这台机器吃紧。
3. **DuckDB 这一期不进默认发布包**：它是唯一要 cgo 的，会改掉整个发布方式、二进制翻三倍。要尽早给 DuckDB 用户一条路，便宜的做法是 `exec` 引擎加 `duckdb` 方言、配一个把 `duckdb -json` 转成 §8.8 输出约定的脚本（用户自己装 duckdb CLI，只读）；真有人用再考虑单独的 cgo 构建包。

## 最强的反方理由

- **对 ClickHouse 先做**：sqlmux 的卖点是改格子、事务保存，ClickHouse 恰好两样都做不到；用户第一眼看到的是「表只读」。另外 25.8 到 26.10 就有 3 处行为差异，自己解析 HTTP 要一直跟版本。
- **对 DuckDB 往后排**：DuckDB 在开发者里可能是三个里最火的，也是唯一能完整支持编辑的；如果用户收到的反馈主要是 DuckDB，按工作量排序就是在做没人要的那个。
- **对整体**：GitHub 上的证据只有 issue #15 一个人（就是 PR #20 的作者），没有 👍；顺序应以用户在别处收到的反馈数为准。

## 拿不准、没验证的

ClickHouse Cloud 的 HTTPS 和 SSH 隧道；Trino 的 Kerberos / OAuth2 和 Hive / S3 connector、真实集群上的延迟（本机单节点每条查询 20–100 ms，深翻页 `OFFSET 5000000` 要 32 s）；DuckDB 在 macOS 和 linux/arm64 上的运行；PR #20 在 26.10 上那处保存语法错的根因。
