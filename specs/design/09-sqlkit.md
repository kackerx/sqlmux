# sqlmux 技术方案 · §9 SQL 处理（`internal/sqlkit`）

> 这是技术方案的一章，各章的索引和阅读规则在 [`../tech-design.md`](../tech-design.md)；章节编号（§N.M）全仓库通用，不随文件改。

## 9. SQL 处理（`internal/sqlkit`）

### 9.1 词法扫描器

扫描一遍即完成，不构建 AST，按方言处理（M14 F14.1 起有 SQLite 方言：标识符引号 `"`，格式化用 sql-formatter 的 `sqlite`，补全的关键字表共用 PG 的；分句照 SQLite 的 complete.c 跟踪 `CREATE [TEMP] TRIGGER … BEGIN … END;`，中间的 `;` 不切，这是 SQLite 唯一的多语句结构，审查时定）：

- **PG**：`'…'`（`''` 转义）、`E'…'`、`$tag$…$tag$`、`"标识符"`、`--` 注释、可嵌套的 `/* */`。0x80 以上的字节算标识符字符，同 scan.l 的 `ident_start` / `ident_cont`（M5 F5.3 审查时定：否则 `✓$a$` 这类写法分句和 PG 不一致，只读判定会被绕过）。
- **MySQL**：`'…'` 和 `"…"`（反斜杠转义）、`` `标识符` ``、`#` 注释、`-- `（后面必须带空格）、不可嵌套的 `/* */`。
- **输出**：token 的类型（关键字、标识符、字符串、数字、运算符、标点、注释、空白）、字节偏移、行号。

风险控制：

- 只做词法分析，代码量小，已知的边界情况都用表驱动测试覆盖。
- 执行前，query 会高亮即将执行的语句范围（§9.2），用户能看到实际要跑的是哪一段。
- 读写判定用于提示和应用层拦截，兜底的保护在数据库层（§13）。

### 9.2 分句、可执行标记与执行前高亮（C-01、C-02）

- **分句**：在括号深度为 0 的 `;` 处切分，得到每条语句的范围。已知上限（F5.4）：MySQL 存储过程、触发器、事件的复合语句体会在体内的 `;` 处被切开，`DELIMITER`（mysql 命令行和 DataGrip 都支持的客户端命令）也不认，原样发给服务端报错，所以这些在 query 里建不出来；`DELIMITER` 留作以后的 feature。**SQL Server**（M18 F18.6，方言 `MSSQL`）：单独成行的 `GO`（大小写不限，后面可带次数）是客户端的分批符，在那里切开、不发给服务端，批内再按 `;` 分句；标识符 `[name]`（也认 `"name"`），字符串 `'…'` 和 `N'…'`，注释同 PG；格式化用 sql-formatter 的 `transactsql`；补全关键字表共用 PG 的（同 SQLite）；`CaseInsensitive` 为真。
- **光标所在语句**（按行算，M3 审查时定）：
  - 光标所在的行上有某条语句的起点时，取这一条，不管光标是不是在行首的缩进里；
  - 紧贴在语句上方、中间没有空行的注释行，算作下面那条语句的一部分，因为这种注释通常是在说明下一条；
  - 否则取范围包含光标的那条；光标停在两条语句之间的空行上时，取前一条。
- **显示**：
  - 每条语句的起始行，在 gutter 显示 ▶ 标记，可以点击执行（C-02）。
  - query 处于 NORMAL 时，光标所在语句的范围用 `row` 底色标出，紧贴在上方、算作这条语句的注释行也一起标出（执行的文字仍从第一行代码开始）；处于 VISUAL 时显示选区。这就是按 ↵ 会执行的内容。

### 9.3 读写判定

1. 第一个有效关键字是 SELECT、SHOW、EXPLAIN、TABLE、VALUES、DESC、DESCRIBE 之一时，判为读。
2. 以 WITH 开头的，继续往后扫描，见第 6 条。
3. `EXPLAIN ANALYZE` 后面跟写语句时判为写，因为它会真正执行；`EXPLAIN (ANALYZE, BUFFERS) …` 这种括号写法里的 ANALYZE 也算。
4. PG 的 `SELECT … INTO 新表` 会建表，判为写。
5. 带行锁子句的 SELECT（`FOR UPDATE` / `FOR SHARE` / `FOR NO KEY UPDATE` / `FOR KEY SHARE`，以及 MySQL 旧写法的 `LOCK IN SHARE MODE`）判为写，不论在不在 WITH 里：它会加锁，PG 的只读事务也拒绝它。
6. WITH 之后在任何括号深度出现 INSERT、UPDATE、DELETE、MERGE 都判为写：CTE 里的写语句在括号里，只看顶层会漏掉。EXPLAIN 括号写法里的 `ANALYZE false` / `off` / `0` 不算 ANALYZE。
7. 其余一律判为写。

这个判定用于提示和应用层的拦截（快速 SQL 的写语句，§12；只读连接，§13）；判为读却会写数据的（比如调用了写数据的函数），靠数据库层的只读兜住（§13）。

### 9.4 自动 LIMIT（F-05）

- 对 SELECT、WITH…SELECT、TABLE、VALUES 这类读语句：顶层没有 LIMIT 或 FETCH 时，先去掉末尾的 `;`，再追加 `\nLIMIT n`（query 为 `max_rows + 1`，快速 SQL 为 101）。前面加换行，防止语句末尾的 `--` 注释把 LIMIT 注释掉。多取一行是为了判断是否还有更多，界面上显示 `100+`。
- 不用子查询包一层来加 LIMIT，因为 MySQL 可能丢掉派生表里的 ORDER BY。
- 其他语句由 `Exec` 的 `maxRows` 截断。

### 9.5 格式化（C-03 gq）

**做法**：用 goja（纯 Go 实现的 JS 引擎）在进程内运行 [sql-formatter](https://github.com/sql-formatter-org/sql-formatter) 的独立打包文件。

- sql-formatter 使用 MIT 许可，支持 postgresql、mysql 等方言，在 JS 生态中广泛使用。
- 打包文件 `sql-formatter.min.js` 固定版本，放在 `internal/sqlkit/` 下，用 `go:embed` 嵌入。

**可行性验证**（2026-09-23 spike，版本 sql-formatter 15.8.2，打包文件 312KB，darwin/arm64）：

| 项 | 结果 |
|---|---|
| 初始化（解析打包文件） | 113ms |
| 单次格式化 | 首次 55ms，之后 13–21ms |
| 内存 | 进程 RSS 峰值约 30MB |
| 二进制体积 | spike 增加约 8MB（空程序 1.6MB → 9.6MB）。M3 实测（F3.9，审查时）：不 strip 13.8MB → 27.4MB，strip 后 9.7MB → 19.4MB。增量比 spike 大，是因为 goja 用反射调方法，链接器因此给整个二进制保留所有导出方法（pgx 的代码段从 718 涨到 5040 个符号），空程序看不出这一点 |
| 正确性 | PG 的 `$$…$$`、`E'…'`、`::` 转换、`filter (where …)`、`$1` 参数、注释，MySQL 的反引号、`#` 注释、`limit 5, 10` 均输出正确 |

**兼容处理**：goja 的正则引擎不认识长 Unicode 属性名。加载前把 `\p{Alphabetic}`、`\p{Mark}`、`\p{Decimal_Number}` 替换为 `\p{L}`、`\p{M}`、`\p{Nd}`。sql-formatter 15.9.0 里共 5 处：`\p{Alphabetic}` 3 处（15.9.0 的数字正则里多了一个前瞻 `(?![\w\p{Alphabetic}])`，用了两次），`\p{Mark}`、`\p{Decimal_Number}` 各 1 处；测试按名字逐个断言次数，升级后次数一变就失败；对 SQL 标识符而言，二者的差别可以忽略。

**运行方式**：

- **`gq` 是操作符**（M3 审查时定）：`gq{移动}` 格式化移动碰到的那几条语句（从第一条到最后一条），`gqq` 和 `gqgq` 是当前语句，VISUAL 下按选区。否则 vim 用户习惯的 `gqap`、`gqip` 会把 `a`、`i` 当成进入 INSERT。编辑器识别这个操作符，产生「格式化这个范围」的效果，由 app 执行。
- 第一次格式化时才初始化 VM，之后一直复用。goja 不是线程安全的，所以用一把互斥锁保护。
- 语言按 query 所在连接的引擎选择；`tab_width` 原样传给 sql-formatter；`keyword_case` 同时设 `keywordCase` 和 `dataTypeCase`（PG 的类型名本身就是关键字，15.x 把它分成了单独的选项，默认 preserve），`functionCase` 保持 preserve。
- **5s 超时**：goja 跑 sql-formatter 时耗时增长比输入快得多，100 行的 insert（2KB）要 3s，200 行要 6.9s，17KB 要 122s，同样的包在 node 里只要 28–146ms，时间都花在 goja 的 Unicode 正则上（M3 审查时实测）。所以和 `formatprg` 一样 5s 超时，用 `vm.Interrupt` 打断后 `ClearInterrupt`，同一个 VM 还能继续用；toast「格式化超时（5s）」，缓冲区不变。多语句（M13 F13.19）逐条格式化再拼回，每条从上一个 `;` 之后切到自己的 `;`、语句前的注释随这条一起交给 sql-formatter，拼回的结果和整段一次格式化一致；5s 是一次 `gq` 的总时长，每条拿剩下的时间（审查时定）。已知上限：一两百行以上的语句用内置格式化会超时，这时配 `formatprg`（如 `pg_format -`）。
- 格式化失败时给出 toast 提示，缓冲区保持不变。输出为空也按失败处理（toast「格式化失败：没有输出」），不能把用户的 SQL 删成只剩 `;`。
- 配置了 `formatprg`（例如 `"pg_format -"`）时，改为调用外部命令：从 stdin 输入，读取 stdout 输出，与 vim 的 formatprg 一致。

### 9.6 WHERE 条件（Q-01）

不做简写语法。输入框里直接写完整的 SQL 条件表达式，例如 `deleted_at is null and status <> 'cancelled'`。

- **拼接方式**：输入原样放进 `SELECT * FROM t WHERE (\n<输入>\n) ORDER BY … LIMIT … OFFSET …`；计数查询用同一个表达式。
  - 外面加一层括号，是为了防止表达式里的 `OR` 和后面拼上的子句发生优先级错乱。
  - 括号内侧各加一个换行，这样输入末尾的 `--` 注释不会把右括号和后面的 ORDER / LIMIT 一起注释掉（lazysql 有这个问题）。输入为空时不加 WHERE。
- **只允许一条语句**：
  - PG 的表格查询走扩展协议，协议本身就拒绝多条语句；MySQL 不开启 `multiStatements`。
  - 发送之前，扫描器如果在顶层发现 `;`，直接提示错误，不发送：在 pane 底部的错误栏显示「WHERE 里不能有 ;」（F3.20 起，此前在内容区第一行）（M3 F3.5 起；在此之前由 PG 的扩展协议报错）。这样可以防止类似 `1=1; drop table t` 的输入连带执行其他语句。
- **历史 / 收藏**：保存输入原文。
  - 下拉框的高度（M10 F10.1，用户要看到更多候选）：行数取「data pane 内容区底边放得下的」和「原来的 min(条目数, 8, 窗口剩余)」中的大者，再不超过条目数，所以 pane 矮时不比现在少、可以照旧盖到结果区；下面放不下、上面更宽时翻到输入框上方（CompleteBox 的既有规则，§10.2），同样撑到条目数、在输入框上一行结束，不挡输入；不加配置项。补全列表仍是 8 行（§9.7）。候选行的布局（M16 F16.11，用户 2026-10-17 要像 nvim-cmp）：左边一列种类图标（`CompleteItem.Icon`，WHERE 历史的 star / history 本来就在这一列；名字和颜色在 §7.7），然后是文字，右边的 dim 说明照旧（列的类型、keyword、Table…）；ascii 下图标是一个字母。
- **补全**：见 §9.7。

### 9.7 模糊匹配与补全（query、WHERE）

**匹配算法直接用 fzf 的**：`github.com/junegunn/fzf/src/algo`（MIT 许可），用的就是 fzf 的 `FuzzyMatchV2`，得分和高亮位置都与 fzf 一致。

- **统一使用**：项目里所有的模糊匹配（命令面板、COLS 过滤、树过滤、WHERE 历史、补全）都用它，不再引入 sahilm/fuzzy。
- **已验证**（spike）：
  - 单独引入这个包，只会带进 fzf 的 `util` 以及 uniseg、go-isatty、go-shellwords、x/sys，测试程序只有 1.7MB。
  - 输入 `tord` 时，`t_order` 排在第一位。
- **封装**：这个包并不是 fzf 承诺稳定的库 API。所以固定版本，并封装在 `internal/ui/match.go` 一个文件里，以后升级只需要改这一处。
- **大小写**：smartcase，与 fzf 相同。
- **扩展语法**：列表类的过滤支持 fzf 扩展语法的常用部分：空格分隔多个词（AND）、`'精确`、`^前缀`、`后缀$`、`!排除`。

**补全的上下文判断**：根据光标前的 token 决定给哪类候选，只做词法判断，不做语法分析（`sqlkit.CompletionContext`）。

| 光标所在位置 | 候选 |
|---|---|
| FROM / JOIN / UPDATE / INTO / TABLE 之后 | 表名（含视图）；输入 `schema.` 后，只列该 schema 下的表 |
| `INSERT INTO 表 (` 之后，以及这个括号里的逗号之后 | 这张表还没写进括号的列，按表里的列顺序；第一项是「全部列」（M5 F5.13，见下面「INSERT 的列」） |
| `别名.` 或 `表名.` 之后 | 这张表的列。别名从当前语句的 FROM / JOIN 子句中解析，例如 `from t_order o` 中 `o` 指 `t_order` |
| query 的其他位置 | 依次为：当前语句引用到的表的列、其余表名、SQL 关键字。关键字包括查询、DML、DDL 的开头和子句（`insert into`、`update`、`set`、`delete from`、`values`、`returning`、`create table` 等；M5 F5.12 补齐，原来只有查询用的，用户验收时发现 insert / update / delete 补不出来），清单参照 lazysql 的 `builtinKeywords`，MySQL 另有 `replace into`、`on duplicate key update` |
| WHERE 输入框 | 当前表的列名（附带类型）和常用关键字（`and` `or` `is null` `in` `like` `between` 等）。在 `列 =`、`列 <>`、`列 in (` 之后，枚举列和布尔列给出可选值 |

- **排序**（M12 F12.6 起；原来先按候选组顺序、再按 fzf 得分，用户 2026-10-11：打 `sele` 时 `pap_secretary_problem_event` 排在 `select` 前面不对）：先按匹配的紧密程度分层，层内再按候选组顺序，组内按 fzf 得分。层：① 和输入完全相同（原有规则）；② 输入是候选开头的前缀（`sele` → `select`，`inse` → `insert into`）；③ 输入是候选某个词首开始的前缀（`sec` → `pap_secretary_problem_event`）；④ 其余模糊匹配。候选组顺序在语句开头处（光标前这条语句里还没有别的 token）改成 关键字 → 表 → 列：那里打的几乎都是关键字；别处照上表的顺序。
- **数据来源**：候选全部来自 session 的 catalog 缓存，在本地匹配，不发请求。某张表的列第一次用到时，才从 `Meta` 连接获取。

**交互**（与 fzf 以及常见编辑器的补全一致）：

- 输入标识符字符时自动弹出候选列表，随输入实时重排，匹配到的字符用 `match` 色的前景标出，不加底色（与命令面板相同，§7.3）。
- 每一项右侧标注类别：表、视图、列的类型、关键字。列还会标出所属的表。
- **输入的第一个字符必须落在候选的词首**（M2/M3 验收时用户要求 `evt` 能匹配 `mt_event`，取代 F1.14 的「首字符相同」）：词首是候选的开头，或 `_`、`.`、`-`、`$` 之后的字符，以及小写到大写的切换处；空格、字母后面的数字不算，所以 `nu` 不出 `is null`。fzf 给出的对齐首字符不在词首时，从词首重新对齐一次再判断。其余字符照 fzf 模糊匹配。不分大小写；取值位置去掉引号后比较，前缀为空时不过滤。`evt` → `mt_event`、`tord` → `t_order` 都能匹配，`x` 仍不会弹出 `max`、`exists`。VS Code 的补全也是按词首过滤的。补全整串都不分大小写，不用 fzf 的 smartcase：SQL 的关键字和不加引号的标识符本来就不分大小写，用大写写 SQL 的人输入 `STA`、`T_OR` 也要有补全（tester 在 F1.14 实测）。命令面板、树的过滤、COLS 这些列表不受影响，照旧是完整的 fzf 匹配和 smartcase。
- 弹出时第一项就是选中的，用 `select` 底（M1 用户反馈）。`Tab` / `C-n` / `↓` 移到下一项，`S-Tab` / `C-p` / `↑` 移到上一项，到头绕回：最后一项再往下回到第一项，第一项再往上到最后一项。
- **`↵` 只接受**（M8 F8.1，取代 M1–M5 的「文字不变就执行」和 F5.12 的「只有补空格算接受」：那两条留下了 `id↵` 换行、WHERE 里直接执行的洞）：列表开着时 `↵` 一律接受选中项，不执行、不换行，就算文字一个字都没变；想换行或执行再按一次 `↵`。列表没弹出时 `↵` 照常执行。已知的代价：输入的是某个候选的前缀，比如别名 `as ma`，`↵` 会补成 `max`，要执行得先 `esc`；`insert into t (` 之后「全部列」选中，`↵` 一定插入全部列，想换行要先 `esc`（F5.13）。
- **接受后补空格**（F5.12 起关键字补，M8 F8.1 起按「后面自然接什么」）：关键字、表名、列名后面接的是下一个词，补一个空格，光标落在空格后面；光标后面已经是空白时只把光标移过去；**光标后面紧跟 `)` `,` `;` `.` 时也不补**（F8.1 审查时定：要接的东西已经在那里了，开着自动配对在括号里补列名每次都会多出 `status )`）。值（`null`、`true`、`false`、`is null`、`is not null`）、函数（接 `(`）、枚举值（接右引号）、INSERT 列清单里的列（接 `,` / `)`）不补。WHERE 执行和记历史前去掉首尾空白（`paid↵↵` 不记成 `paid `）；query 里留下的行尾空格不管。
- **列表只在输入文字时弹出**（§9.7 开头的「输入时自动弹出」，F8.1 审查时重申）：进 INSERT 的 `i` / `a` / `A` / `o`、方向键这些不改文字的键不弹；原来也弹，只是那时 `↵` 文字不变就执行所以无害，现在会把 `A↵` 变成只补一个空格、`i↵` 变成 `where id id = 1`。`C-n` 手动唤起时列出该位置的全部候选（`select ` 还没有 `from` 时是表和关键字，`where ` 后面列在前、关键字也在），不因为 F8.4 的「只给列」少掉东西。
- **和输入的词完全相同的候选排第一**（不分大小写，F5.12 审查时定）：排在分组顺序之前，否则打完 `set`、`to` 这类短关键字按 `↵`，会被模糊匹配到的表名（`schema_migrations`、`t_order`）换掉。
- 快速 SQL 的面板里，`Tab` / `S-Tab` 在列表开着时移动候选，关着时照常切换范围。
- `esc`：快速 SQL（没有 vim 模式的单行输入框）里，第一次关闭候选列表，第二次才退出输入（WHERE 在 F3.39 起同 query：一次 `esc` 关列表并回 NORMAL），与 COLS 下拉「先清空，再关闭」的规则一致。query 里照 nvim-cmp 的做法，`esc` 关掉列表的同时退出 INSERT：query 里几乎每敲一个标识符都会弹出列表，「打完一个词按 esc」是 vim 用户最常见的动作，分两步会让后面的 `k`、`0` 都被当成文字打进去（M3 F3.10 实测）。
- 鼠标：悬停即移动选择，点击即接受。

**WHERE 补全的细节**（M1 F1.5；上下文判断放在 `sqlkit.WhereContext(text, pos)`。M3 起它和 `CompletionContext` 建在同一个扫描器上，但仍是两个函数：WHERE 的「取值模式」是 WHERE 独有的，合并后重复的只剩几行）：

- 先跳过字符串、带引号的标识符和注释，再看光标前面：光标前是标识符的一部分，就是列名 / 关键字模式；光标前是 `列 =`、`列 <>`、`列 in (`（含 in 列表里逗号之后），后面可以跟半截值（`'d`、`tr`），就是取值模式，只针对枚举列和布尔列。写之前先看 lazysql 的 `sql_lexer.go` / `sql_context.go`。
- **弹出时机**：列名 / 关键字模式在输入标识符字符时弹出，前缀为空不弹（刚敲了空格）；取值模式到了取值位置就弹，前缀可以为空，所以输入 `status = ` 就直接列出枚举值。
- **候选**：列名（附类型）在前；关键字在后：and、or、not、is null、is not null、in、like、ilike、between、true、false、null；取值模式下枚举值带引号（`'done'`）、按 enumsortorder 排，布尔值为 true / false。先按组，组内按 fzf 分数。
- **接受**：用候选替换光标前的前缀，取值模式下替换半截值（`'d` → `'done'`）。**M8 F8.1 起**（用户：打完 `id` 按 `↵` 还换了行 / WHERE 里直接执行了）：列表开着时 `↵` 一律只接受，不执行、不换行，就算文字没变也一样，上面那条「跨过右引号算文字没变、照常执行」的例外作废；补空格按「后面自然接什么」：关键字、表名、列名补一个空格（光标后已是空白就只移过去），值、函数、枚举值、INSERT 列清单里的列不补。
- **空前缀的列表不预选**（M8 F8.17，用户：接受 `where` 补了空格之后还得再敲一个空格才看到列名）：前缀为空时自动弹出的列表（F8.4 的列名、`别名.` 后的列名；`from ` 后的表名不自动弹，只有 `C-n`）**不预选任何一项**，`↵` 照常换行 / 执行，`Tab` / `C-n` / `↓` 才选中第一项，打了字母过滤后第一项自动选中、`↵` 接受（同 nvim 的 `noselect`）；所以接受 `where` / `select` / `and` 等词补出空格后列表立即弹出，不再区分「空格是打的还是补的」。例外：`insert into t (` 弹出的「全部列」仍预选（F5.13，用户要的就是 `↵` 一下全加上）；WHERE 取值位置（`status = `、`paid = `）的枚举 / 布尔值列表也仍预选第一项（F8.17 审查时定：缺值的条件执行一定报错，`↵` 在这里没有别的有意义的结果，F1.5 / F1.14 的验收不变）。
- **空前缀的列名补全**（M8 F8.4）：光标前一个词是 `select`、`where`、`and`、`or`、`on`、`set`、`having`、`by`，或者在 select / set / order by 列表的 `,` 后面，且语句里找得到表（`from` / `update` / `delete from` / `insert into` / `join`，含别名）时，打了空格就弹出这些表的列（光标紧跟空白才弹，`a,` 不弹；F8.17 起接受关键字补出的空格也弹，列表不预选所以 `where↵↵` 仍是换行），前缀为空、不含「全部列」项，多表的排法同打了字母之后、不另加 `表.列` 候选；找不到表不弹；触发词还有 `distinct`（`select distinct `、`count(distinct `），`by` / `on` / `set` 只看前一个词。快速 SQL 和 query 共用补全，行为一致。WHERE 输入框在 `and ` / `or ` 后面时同样，空输入框不弹（删空再 `↵` 是清除过滤）。已知上限：`between 1 and ` 也弹（要的是值，打数字就关了），MySQL 的 `on duplicate key update ` 和 `update a = 1, ` 不弹。lazysql 的 completer 前缀为空直接返回 nil，没有可借的。
- **列表组件** `ui.Complete`：画在输入框下方、光标所在列，最多 8 行；每行是候选（匹配字符高亮）加右侧 dim 注释（列的类型、「关键字」、「值」）。不复用下拉框，因为补全没有过滤框。键位在 `[keys.complete]`：`Tab` / `C-n` / `↓` 下一项，`S-Tab` / `C-p` / `↑` 上一项。`↵` 和 `esc` 不绑，交给输入框按上面的规则处理：绑在 complete 作用域里的键会被这个作用域吃掉，没法在「没选过」时交回输入框去执行。鼠标：悬停即选中，点击即接受。

**INSERT 的列**（M5 F5.13，用户验收时要求：`insert into t_log(` 之后要能看到有哪些列，也能一下全加上）：

- **位置**：`insert into <表> (` 的括号里，光标在 `(` 之后或逗号之后（可以带半截列名）；表名可以带 schema、带引号，PG 和 MySQL 都算。到了这个位置就弹出，前缀可以为空，和 WHERE 的取值模式一样；所以敲 `(`（自动配对成 `(|)`）或 `, ` 就直接列出来。
- **候选**：这张表里还没写进括号的列，按表里的列顺序，右侧注释照常是类型；不是 `generated always` 的才列（`generated always as identity` 和生成列插不进去，写了会报错）。前缀为空时第一项是「全部列」：显示成剩下的列用 `, ` 连起来，放不下的用省略号，右侧注释「全部 N 列」；有前缀后这一项不出现，其余照 fzf 匹配。
- **接受**：单个列插入列名，不补空格（后面接 `,` 或 `)`）；「全部列」把剩下的列用 `, ` 连起来一次插进去，光标停在最后一列后面，正好在 `)` 前。列名和其他补全一样，需要引号的（PG 的大写、空格、保留字，MySQL / SQLite 的空格和保留字）加引号插入，显示的仍是原名（M15 F15.11，`sqlkit.Dialect.NeedsQuote`）。保留字每个方言一份表，照 `pgReserved` 的做法：PG 是 `pg_get_keywords()` 的 R 和 T 类（PG 17）；MySQL 是 8.4 `information_schema.KEYWORDS` 里 `RESERVED = 1` 的 262 个（只给这些加，不按 sqlkit 的关键字表加：那张表少 `rank`、`condition`、`option`、`int` 等 149 个保留字，又多 `comment`、`first`、`view` 等 80 个非保留字）；SQLite 是 sqlite.org 的关键字表（147 个，全加引号无害；关键字表漏了 `autoincrement`、`isnull`、`notnull`、`raise` 等）。单测各方言挑几个只在保留字表里的词和几个只在关键字表里的词。

**WHERE 的 `C-r` 历史 / 收藏下拉**：也按当前输入模糊过滤，体验与 shell 里 fzf 的 CTRL-R 相同。

- **打开**：在 WHERE 输入框的 INSERT 下按 `C-r`（Action `where.history`，绑在 `[keys.input]`，只在 WHERE 输入框里有效果）、NORMAL 下按 `/`（F3.39，`[keys.wherenormal]`），或点击 WHERE 行右端的 `▾`（§7.8）。
- **过滤**：不另设过滤框，直接用 WHERE 输入框里的文字做 fzf 过滤，边输入边过滤。
- **列表**：历史在前、收藏在后（M8 F8.14，用户要历史在上；原来收藏在前），两组都非空时中间一行 `sep` 色的横线，组内按时间从新到旧；每项前面画图标 `star` / `history`，不画组标题（F3.31）；底部一行提示（移动 · 应用 · 收藏 / 取消收藏 · 关闭，键位从 keymap 读）；WHERE 行右端的 `▾` 前面写打开下拉的键（`history` 图标 + `where.history` 的键，`dim` 色，这一段也能点开下拉；输入框留得下 16 列才显示，放不下只留 `▾`，F8.14 审查时定）；`icons = "ascii"` 时也不画，`*` / `h` 和右侧的说明已经能区分。打开时列出全部，不按输入框里已有的文字过滤，开始输入后才过滤。右侧说明：收藏项显示附带的 ORDER / LIMIT，跟默认值不同时才显示（`status ↓ · 500`）；历史项显示时间（`09-24 14:05`）。
- **按键**（`[keys.where]`，Action `where.up/down/apply/star/close`）：`C-n` / `Tab` / `↓` 下一项，`C-p` / `S-Tab` / `↑` 上一项，到头绕回（F3.31）；`↵` 应用，把 WHERE、ORDER、LIMIT 都换成这一项的，从第 1 页开始执行；`C-e`（`where.edit`，M9 F9.1）只把这一项的 WHERE 文字填进输入框、INSERT、光标在末尾，ORDER / LIMIT 不动、不执行，方便改个值再执行；`C-f` 收藏 / 取消收藏（下拉没开、焦点在 WHERE 输入框时也是它：收藏输入框里的条件连同当前 ORDER / LIMIT，已有就取消，toast「已收藏」/「已取消收藏」，M9 F9.2）；`esc` 关闭，输入框里的文字保留。表格上按 `C-r`（`where.history` 绑在 `[keys.grid]`，M9 F9.4）先进 WHERE 输入再打开下拉。
- **历史的规则**：每次 `↵` 执行一条非空的 WHERE，就连同当时的 ORDER / LIMIT 记一条；内容相同的去重并挪到最前面；每张表最多 100 条（F3.31，原为 50），满了丢最旧的。快速 SQL 的历史仍是 50 条。收藏不限数量。

### 9.8 Redis 控制台的词法与补全（M16）

不用 `sqlkit`：Redis 的控制台是一行一条命令，词法是 redis-cli 的 `sdssplitargs`（`internal/db/redis`，不另开包）。

- **拆参数**（照 redis-cli）：空白分隔；`"…"` 里认 `\n` `\r` `\t` `\b` `\a` `\xHH` 和 `\"`；`'…'` 里只认 `\'`；引号后必须紧跟空白或行尾，否则整行报「引号没有闭合 / 引号后有字符」，这一行不发。`#` 开头（前面只有空白）的行是注释，空行跳过。一行一条命令，不认 `;`。
- **执行范围**（§11「Redis 控制台」）：`↵` 跑光标所在行；VISUAL 选区跑选到的每一行；`▶ run` 同 query。每行一条请求，逐条跑，出错就停。
- **高亮**：命令名（首词，和子命令一起，如 `CONFIG GET`、`JSON.SET`）来自 `COMMAND`（连接时在 `Meta` 上取一次，缓存在 `Conn.Catalog`），用 `keyword` 色；字符串和注释沿用 `sql_string` / `comment`；数字 `number`。认不出的首词不着色。
- **只读与危险**（§13）：只读连接拦的命令按 §13（`write` / `may_replicate` flag、非 `readonly` 的 `@scripting`、`@admin`、服务端没列出的），ACL 分类 `@dangerous` 的是危险命令；两份名单都来自服务端，不手写；没拿到（没权限执行 COMMAND）时全按写处理。
- **补全**（F16.5，`COMMAND DOCS`，Redis 7 起；6 没有 DOCS 时只补命令名）：首词位置补命令名（含 `JSON.` 这类模块命令），第二个词位置如果首词有子命令（DOCS 里的 `subcommands`）补子命令；之后按 DOCS 的 `arguments` 定位当前参数：`key` 类型的参数位置在 `Meta` 上 `SCAN 0 MATCH <前缀>* COUNT 1000` 补键名（MATCH 是扫完再筛，COUNT 100 一轮常常一个都筛不出；F16.4 验收时定，照实现）（去抖 150ms，同一个前缀只扫一次；结果按连接缓存，控制台跑过写命令（`lang` 判为写的那条）或树上对这个 db 按 `R` 后清掉重扫，不然 `SET newkey` 之后补过的前缀永远补不出它；F16.4 审查时定）；`HGET` / `HSET` / `HDEL` / `HMGET` 的 field 位置、键已知且长度 ≤ `load_all` 时 `HKEYS`；`oneof` / `pure-token` 的参数位置补那几个 token（`EX` / `PX` / `NX` / `XX`）。排序照 §9.7（F12.6）。候选左侧的种类图标同 SQL 的补全框（§7.7，F16.11）。签名提示用 F15.3 的浮层：DOCS 的 `arguments` 展开成 `SET key value [NX | XX] [GET] [EX seconds | …]`，当前参数高亮；可选块和 `multiple` 的参数按 DOCS 的 flags 标 `[]` 和 `…`，不做 redis-cli 那种完整的 hint 文法，认不出就整段 dim。
- 已知上限：参数定位是线性的，`multiple` 之后的可选 token 定位可能偏；DOCS 没有 `key_specs` 的模块命令不补键名。

