# M3 console 与结果区 · 任务清单

- **状态**：第二轮改进 F3.25–F3.39 todo（2026-09-30），通过后直接开工 M4，M2、M3、M4 一起验收。F3.1–F3.24 全部 passed，M3 完整回归在 9a795c4 上全绿（50 个脚本 1314 项 e2e，e2e 2702236），等用户与 M2 一起验收；F3.1–F3.11 完整回归在 e8f8e8c 上全绿（1104 项 e2e）。用户验收 M2 / M3 时提了 13 条意见（2026-09-29），能现在做的整理成改进项 F3.12–F3.24；多连接 session、console 的事务控件排进 M5。改进项全部通过后，M2、M3 一起验收。
- **目标**：
  - vim 编辑器写 SQL；
  - 执行语句，结果显示在底部结果区；
  - 支持格式化、补全和 schema 选择；
  - 表格和 console 可以放在同一个 pane 的不同 tab 里，新 tab 显示引导页（M1 用户验收时加）。
- **范围**：
  - PRD：C-01~C-07
  - tech-design：§5、§8.6、§9.1–§9.5、§9.7、§11、§15
- **依赖**：M1、M2 全部 passed。
- **不在 M3 做**：
  - 快速 SQL 依赖 M3 的部分（写语句的判定与提示、`C-t` 送到结果区、`C-e` 在 console 中打开、首词判断换成读写判定与自动 LIMIT）：M4 F4.3。M3 只在 sqlkit 里提供这些函数，不动快速 SQL。
  - `result_split = console`：不做（§11）。
  - MySQL 的连接和 console：M5。M3 只做 sqlkit 这一层的方言（词法、格式化的 golden）。
  - 用户在 console 里 `begin` 之后不提交：M5 的 manual 事务模式时处理（§11 已知上限）。
- **完成标准**：
  - F3.1–F3.39 全部 passed；
  - 编辑器的差分测试与 nvim 的结果完全一致；
  - 用户验收通过后，打 tag `m3`（`m2` 同时打）。
- **审查节点**：
  1. F3.3 之后：F3.1–F3.3，编辑器（除块选择），纯逻辑，有 nvim 差分兜底（已通过）；
  2. F3.5 之后：F3.4–F3.5，块选择与 sqlkit（已通过）；
  3. F3.8 之后：F3.6–F3.8，console tab、混放与引导页、执行与结果区（已通过）；
  4. F3.11 之后：F3.9–F3.11，格式化、补全、schema 下拉（已通过）；
  5. F3.17 之后：F3.12–F3.17，输入与按键类的改进（已通过）；
  6. F3.24 之后：F3.18–F3.24，表格与目录树的改进（已通过）；
  7. F3.39 之后：F3.25–F3.39，第二轮改进，一个节点（用户要求拉长审查节点）。
- **M1 / M2 的 e2e**：默认布局加入 console 后，⟨1⟩ 的宽度从占满变成 5/9，M1 / M2 脚本里依赖 data pane 宽度、`C-l` 焦点的地方可能失效。tester 在 `lib.sh` 里加一个开头先关掉 ⟨2⟩ 的辅助函数，不逐条改断言。

任务文件的格式和状态约定见 [`../plan.md`](../plan.md)。

---

## F3.1 编辑器核心与差分测试框架 · 状态：passed（1887d22；e2e 43bb04b）

- **依赖**：M0
- **涉及**：`internal/editor`

**开发**
- [x] 接口：`New(text)`、`Feed(key)`（按键用 keymap 的记法字符串，editor 不 import keymap）、`Lines()`、`Cursor()`、`Mode()`、`Pending()`、`Selection()`、`SetHeight(n)`、`Top()`，另返回这一步的效果：寄存器变了、要执行的 ex 命令、内容变了。editor 不 import ui 和 bubbletea（§4）。
- [x] 位置为行加字节列，与 nvim 的 `getpos` 相同；显示列按 `ansi.GraphemeWidth`，Tab 按 `tab_width` 对齐。
- [x] 缓冲区、§11 表里的全部移动、INSERT / REPLACE、次数前缀；不折行，横向跟着光标（§11「不做」）。
- [x] INSERT：Tab 补到下一个 `tab_width` 的倍数（等同 `expandtab ts=sw=tab_width sts=0`），`↵` 继承缩进，带次数的插入（`3ix<Esc>`），方向键照 nvim 断开撤销步。
- [x] 撤销：每步保存整份 `[]string` 快照和光标，没改过的行共享字符串；不设上限，用 `ponytail:` 标出大文件会变慢。
- [x] 差分框架（§15）：`cases.txt` 每条是「`## 标题`、按键、初始文本（█ 标光标）」；golden 记录结果文本（█ 标光标）、无名寄存器的内容与类型、topline；生成器用 `feedkeys(keys, 'xt')` 和 §15 列的选项，文件头记 nvim 版本。

**验收**
- [x] 差分用例覆盖移动、INSERT / REPLACE、次数、撤销，结果与 nvim 全部一致。
- [x] 运行 `go test ./internal/editor` 不需要安装 nvim。

## F3.2 操作符、文本对象、VISUAL 与寄存器 · 状态：passed（1887d22；e2e 43bb04b）

- **依赖**：F3.1
- **涉及**：`internal/editor`

**开发**
- [x] §11 表里的操作符、文本对象、VISUAL / VISUAL LINE、简写（`dd x J ~ r p gc` 等）。操作符的双写形式（`dd cc yy >> << gcc guu gUU g~~`）走「同一个操作符再按一次 = 作用于当前行」的通用规则，不逐个写。
- [x] 无名寄存器带类型；yank 或删除后产生「写剪贴板」的效果，`p` 只读内部寄存器（§11「寄存器」）。

**验收**
- [x] 差分用例覆盖每一个操作符与文本对象的组合类别，结果与 nvim 全部一致。

## F3.3 搜索与命令行 · 状态：passed（1887d22；e2e 43bb04b）

- **依赖**：F3.2
- **涉及**：`internal/editor`

**开发**
- [x] `/ ? n N * #`，RE2、smartcase；找不到时产生「找不到：<pat>」的效果（app 用 toast 显示）；`n` / `N` 绕回时不提示。
- [x] `:` 命令行：`:{n}`、`:s`（范围只做当前行、`%`、`'<,'>`；分隔符可以是任意非字母数字字符；标志 `g i`；`\1`、`&`）；其余命令作为 ex 效果交给 app（§11「命令行」）。
- [x] 命令行的状态放在 editor 里，差分用例能直接覆盖 `/foo<CR>`、`:%s/a/b/g<CR>`。

**验收**
- [x] F3.1–F3.3 的差分用例合计至少 200 条，覆盖每一类操作，结果与 nvim 全部一致。
- [x] RE2 与 vim 语法不同的写法（`+`、`?`、`|`、`()`）有单测。

## F3.4 块选择（VISUAL BLOCK） · 状态：passed（1887d22；e2e 43bb04b）

- **依赖**：F3.2
- **涉及**：`internal/editor`

**开发**

实现 §11 表格中「VISUAL BLOCK」一行及其补充说明的全部内容：

- [x] `I`、`A`、`$A`、`c`：先在第一行输入，按 esc 后复制到其余各行；
- [x] `d`、`x`、`y`；`y` 复制时，寄存器类型为块；`p`、`P` 按块粘贴；
- [x] `r`、`~`、`u`、`U`、`>`、`<`、`gc`；`o`、`O` 切换选区的对角。
- [x] 模式 VisualBlock，状态栏显示 V-BLOCK；`<C-q>` 也当作 `<C-v>`（nvim 的 `nv_visual` 就是这样处理的），所以 §11 说的 Windows 改绑 `C-q` 不用额外配置；v、V、C-v 互相切换同 nvim。
- [x] 给绘制的接口：`Selection()` 语义不变，另加 `Block()` 返回上下行和左右显示列，`$` 延伸时右边为 `math.MaxInt`。
- [x] 寄存器的块类型记为 `"\x16{宽度}"`（`getregtype` 的写法），内容是各行用 `\n` 连起来的文字，同 `getreg`。
- [x] 块下按 `:` 照其他 VISUAL 预填 `'<,'>`，按行执行。清单外的块操作不做：`D C S R X Y s J`、块里的文本对象、块下的 `p`。

**验收**
- [x] 差分用例覆盖上面列出的全部操作，结果与 nvim 一致，包括短行上的 `I` / `A` / `c`、块边界落在 Tab 上或宽字符中间的情况。

## F3.5 sqlkit：词法、分句、读写判定、自动 LIMIT · 状态：passed（1887d22；e2e 43bb04b）

- **依赖**：M1
- **涉及**：`internal/sqlkit`

**开发**
- [x] 扫描器：在现有的 `Tokens` 上扩展成 `Scan(s, dialect)`，输出 token 的类型、字节偏移和行号；方言为 `sqlkit.PG`、`sqlkit.MySQL`；`WhereContext`、`SelectLike` 改用它。先参考 lazysql 的 `sql_lexer.go`，提交说明里写明。
- [x] token 类型加 Keyword，按单独一个文件里的关键字表判断；补全用的关键字从这张表里取常用的部分。
- [x] 分句（§9.2）：语句从第一个不是空白、也不是注释的 token 开始，到 `;` 之前为止；光标在第一条语句之前时取第一条。PG 14 的 `BEGIN ATOMIC … END` 里的 `;` 会被切开，用 `ponytail:` 标出。先看 usql 的 `stmt/`。
- [x] 读写判定（§9.3，含 `SELECT … INTO`、括号写法的 `EXPLAIN (ANALYZE …)`）。先看 lazysql 的 `validation.go`。
- [x] 自动 LIMIT `AutoLimit(stmt, n)`（§9.4）。
- [x] WHERE 的 `;` 检查接进 WHERE 的执行（§9.6）。

**验收**
- [x] 表驱动的单元测试覆盖以下情况：
  - `$tag$…$tag$`、`E'…'`、可嵌套的 `/* */` 注释；
  - MySQL 的反引号、`#` 注释、`-- `（后面必须跟空格才算注释）；
  - 包含写语句的 CTE；`EXPLAIN ANALYZE` 与 `EXPLAIN (ANALYZE, BUFFERS)` 后面跟写语句；`SELECT … INTO`；
  - 语句已有 LIMIT 或 FETCH 时，不再追加 LIMIT；末尾带 `--` 注释时追加的 LIMIT 仍然生效。
- [x] e2e：WHERE 输入 `1=1; drop table t_log` 后执行，pane 第一行显示「WHERE 里不能有 ;」，没有发出查询。

## F3.6 console tab · 状态：passed（374d987；e2e 4ac9bd0）

- **依赖**：F3.3、F3.5
- **涉及**：`internal/ui`（console.go）、`internal/app`（consoleTab）、`internal/config`

**开发**
- [x] 默认布局（§5）：⟨1⟩ 是空 pane（F3.7 起显示引导页），⟨2⟩ 是 console_1，宽度 5:4，初始焦点在 ⟨1⟩。
- [x] 文件、自动保存、`:w` / `C-s` / `:q` / `:wq`（§11「文件」）。自动保存的去抖用 `tea.Tick` 加序号；写入失败 toast「保存失败：<err>」。配置加 `tab_width`。
- [x] 连接名用作目录名，config 里校验连接名：不允许 `/`、`\\`、以 `.` 开头（§14）。
- [x] 横向滚动照 nvim 默认的 `sidescroll=1`、`sidescrolloff=0`；编辑器加 `SetWidth(n)`、`Left()`；差分结果在 leftcol 不为 0 时加一行 `left: N`，补几条长行的用例。
- [x] 绘制（`ui/console.go`）：每行 `▶`（1 列，`focus` 色；上次执行出错的语句为 `error` 色，缓冲区一有改动就清掉）+ 行号（宽 max(3, 位数)，右对齐，`dim` 色，光标行用 `fg`）+ 1 个空格 + 文本。NORMAL 下光标所在语句的范围用 `row` 底，VISUAL 选区用 `select` 底。
- [x] 高亮：关键字 `keyword`，数字 `number`，字符串 `sql_string`，注释 `comment`，标识符后面紧跟 `(` 的用 `func`，其余 `fg`（§7.3）。
- [x] 光标用终端光标：NORMAL / VISUAL 为块，INSERT 为竖线，REPLACE 为下划线。
- [x] 状态栏：模式块显示 `NORMAL`、`INSERT`、`VISUAL`、`V-LINE`、`V-BLOCK`、`REPLACE`、`COMMAND`（V-LINE、V-BLOCK 用 VISUAL 的颜色，REPLACE 用 INSERT 的颜色）；VISUAL 的附加信息为 `4 行 · ↵ run` 或 `12 字符 · ↵ run`，V-BLOCK 为 `3 行 × 4 列 · ↵ run`，键位文字从 keymap 读；待输入序列这一块也显示引擎正在等的键（`2d`、`f`），相当于 showcmd；console 聚焦时不显示 `行,列`。
- [x] `/`、`?`、`:` 的输入行在内容区最后一行（§11「命令行」）。`:`、`;` 从 `[keys.normal]` 挪到 `[keys.grid]`、`[keys.tree]`（`[keys.landing]` 在 F3.7），console 里交给编辑器（§6.8）。
- [x] 编辑器在等后续按键时跳过 keymap（§6.4）。
- [x] 鼠标：单击定位光标（INSERT 下仍是 INSERT，VISUAL 下回到 NORMAL）；在文本区拖动从按下处进入字符 VISUAL；滚轮每格 3 行，最多滚到最后一行在顶部，光标夹回视图内；单击 ▶ 执行那一条语句（`console.run <行>`，F3.8 接上）。
- [x] 粘贴照 nvim 的 `vim.paste`：NORMAL 下贴在光标后面（同 `p`），贴完光标停在最后一个贴进去的字符上；INSERT 下插在光标处；VISUAL 下用粘贴内容替换选区；模式不变；整段算一个撤销步；CRLF 换成 LF；粘贴不弹补全。
- [x] 「在 $EDITOR 中编辑」：Action `console.external`，默认不绑键；依次取 `$VISUAL`、`$EDITOR`、`vi`，用 `sh -c` 执行；先写盘，再用 `tea.ExecProcess` 打开，回来后重新载入，算一个撤销步。
- [x] 支持 `[map.console.normal]`、`[map.console.visual]`。
- [x] 代码里写死的 `doraemon.public ▾` 去掉，F3.11 再画真实的按钮。

**验收**
- [x] e2e：
  - 输入 SQL 后，高亮颜色正确；每条语句的第一行显示 ▶；光标所在语句的范围被高亮；
  - 拖动能选中文本；`f<Space>x` 删掉的是空格本身（`f<Space>` 跳到空格上），不会关掉 pane；
  - console 里 `;` 重复 f/t，`:` 打开编辑器的命令行，`:3` 跳到第 3 行；
  - 文件保存到隔离后的 `XDG_DATA_HOME` 下，权限 0600；重启后 console_1 还是上次的内容；
  - 在 NORMAL 下粘贴，内容作为文本贴在光标后面，不会被当作命令执行；
  - 状态栏的模式、VISUAL 附加信息、showcmd 正确；
  - 连接名里带 `/` 时启动报错。
- [x] golden：console 的高亮、gutter、语句范围、VISUAL 选区、命令行。
- [x] 补回 M0 里因 F1.1 去掉假 console 而删掉的 e2e（tester 在 F1.1 结论里列出），和 schema 下拉框无关的部分：
  - f0.2：data:console = 5:4；console 未聚焦的边框 / 标题色；`▶ run ↵` 的样式与各宽度下的退让；no_wasted_room；
  - f0.3：改绑 `console.run` 为 `R` 后标题显示 `▶ run R`；
  - f0.8：点击 `▶ run` 先让 console 获得焦点；悬停 `▶ run` 为 warn 底、移开恢复；指针在 console 上滚动只滚 console、焦点不变；
  - f0.12：nerd 下 console 图标 U+F489，ascii 下为 `>`。

## F3.7 pane 混放 tab 与引导页 · 状态：passed（374d987；e2e 4ac9bd0）

- **依赖**：F3.6
- **涉及**：`internal/app`（Pane、Tab、openTarget、作用域）、`internal/ui`（tab 栏、引导页）

**开发**
- [x] 去掉 PaneKind（§5「表格和 console 可以放在同一个 pane」）：侧栏就是 `win.Tree`，结果区用 `win.Result`（F3.8 加上），其余都是普通 pane；`Tab` 加 `Console *consoleTab`，`Data` 和 `Console` 都是 nil 的是引导 tab；按 PaneKind 下标的并行表删掉。
- [x] 作用域按当前 tab 的类型（§6.4）。
- [x] 图标：去掉 `data`，表 tab 一律用 `table`，console tab 用 `console`，引导 tab 不画图标（§7.7）。tab 栏每项写成 ` 1:<图标> t_order* `，引导 tab 写成 ` 2:新 tab `。
- [x] 引导页：内容区中间两行按钮 ` <table 图标> 打开表 ` 和 ` <console 图标> 新建 console `，后面跟 `dim` 色的键位，悬停 `select` 底。`[keys.landing]`：`t` → `tab.table`「打开表」，`c` → `console.new`「新建 console」，另加 `:`、`;`（§6.8）。
- [x] `+` 新开一个引导 tab 并切过去；M1 的 `newTabIn` 标记删掉。
- [x] 「打开表」：打开面板的表范围，选中的表总是开在这个引导 tab 里，替换它，不去找已经打开的同一张表。从树或面板正常打开表时，目标 pane 的当前 tab 是引导 tab，也替换它。
- [x] 「新建 console」：目标 pane 的当前 tab 是引导 tab 时就地换成 console（`c`、点击、面板都一样）；否则在焦点所在的普通 pane 新开 console tab，焦点在树上时用 openTarget 选出的 pane。打开表同理：当前 tab 是引导 tab 时，`↵`、`t`、`C-t` 都替换它（§5）。
- [x] 查询条第二行放不下时的让位顺序（§7.8「查询条」）：统计最先让位，保存结果和错误优先于 chip 和按钮。
- [x] openTarget 的「data pane」改成「普通 pane」（不含结果区）；焦点不在普通 pane 上时优先取最近聚焦过、当前 tab 不是 console 的 pane；当前 tab 是 console 时新开 tab（§5）。
- [x] 引导 tab 名为「新 tab」，不画图标，ascii 下也不写类型词；没有 tab 的 pane 标题只有 `⟨n⟩`；引导页和空 pane 的 tab 栏不显示键位提示。`x` / `:q` 关引导 tab 不确认。
- [x] `:wq` 在表 tab 上先保存、成功后才关闭（§11「文件」）。

**验收**
- [x] e2e：
  - 同一个 pane 里开一个表 tab 和一个 console tab，来回切换时标题图标、`▶ run`、按键作用域跟着变；
  - 当前 tab 是 console 时从树按 `↵` 打开表，console 还在，表在新 tab 里；
  - 点 `+` 出现引导页，点「打开表」选一张表后开在这个 tab 里，按 `c` 开出 console；
  - 分割出的新 pane、没有 tab 的 pane 都显示引导页。
- [x] golden：引导页；混放后的 tab 栏。
- [x] tester 改写 f1.6 里依赖「`+` 聚焦树的过滤框」的用例，f0.12 里检查 `data` 图标的断言跟着改。

## F3.8 执行与结果区 · 状态：passed（374d987；e2e 4ac9bd0）

- **依赖**：F3.7
- **涉及**：`internal/app`（结果区、日志）、`internal/ui`（结果 tab、工具行）、`internal/db`、`internal/config`

**开发**
- [x] 执行的单位：光标所在的语句，或选区里的文字（字符、行选区）；块选区执行它覆盖到的那几整行，同 V-LINE。用 sqlkit 分句；逐条执行、遇错就停（§11「执行」）。
- [x] 行数上限与截断显示（§11）；配置加 `result_height`、`[console] max_rows`。
- [x] 执行中：同一个 console 再按 `↵` 忽略；别的 console 在 Worker 的锁上排队，占位照样显示；状态栏 busy，`C-c` 取消 `Main`。
- [x] 结果区的出现与关闭：第一次执行时把根节点包进纵向节点，比例取 window 记住的值（初始 `1 − result_height`）；关闭时记下比例，日志保留在 window 上，结果 tab 全部丢掉（包括固定的）。
- [x] 标题 `⟨3⟩ <result 图标> console_1 #42`，右侧 `3 行 · 8ms` 和 5 个按钮（重跑 `result.rerun`、转置、固定 `result.pin`、导出 `result.export`、关闭 `result.close`），放不下时的舍弃顺序见 §11「工具行」；新图标 `result`、`pin`、`export`、`close`（§7.7）。
- [x] tab：第一个是「日志」；结果 tab 名 `console_1 #42`，多个结果为 `#42`、`#42·2`……；序号 `Session.RunSeq` 每次执行加 1；固定的画 `pin` 图标。
- [x] 替换规则：同一个 console 的未固定结果 tab 整组替换，新的一组放在原来那组第一个的位置；没有结果集时保留上一次的，切到日志（§11）。
- [x] 占位：内容区第一行 `dim` 色 `执行中 · 3s · C-c 取消`，每秒刷新。
- [x] 日志：每条一行 `14:05:12  console_1  <语句第一行>  3 行 · 8ms`，非查询写 `UPDATE 3 · 2ms`；出错为 `error` 色的 `ERROR: <Message>`，DETAIL / HINT 另起行；取消记「已取消」；新的在下，自动滚到底；日志 tab 上 grid 的 `j` / `k` / `gg` / `G` 和滚轮滚动日志。
- [x] 出错：切到日志，出错语句的 ▶ 变红，前面成功的照常出 tab。取消：切到日志，▶ 不变红，toast「查询已取消」。
- [x] 焦点：执行后留在 console，结果区切到这次的第一个结果 tab。
- [x] 结果表格只读：`hjkl gg G 0 $ T`、滚轮、点击可用，`↵` / `i` 不做事，`x` 等同 `q`；`[keys.result]` 加 `R` → `result.rerun`。
- [x] 重跑：用这组结果当时的 SQL，作为来源 console 的一次新执行。
- [x] 导出 CSV 写到当前目录的 `console_1-42.csv`，toast 显示路径（§11）。
- [x] DDL 成功后重新加载 catalog（§11）。

**验收**
- [x] 集成测试：多条语句中第二条出错时，第一条已经提交、第三条没执行；取消后 `Main` 还能继续用。
- [x] e2e：
  - 第一次执行时，在底部出现结果区；
  - 再次执行时替换原有的结果 tab，序号加 1；按 `P` 后再执行，会新开一个 tab；
  - 选中多条语句执行时，产生多个结果 tab；执行 DDL / DML 只写进日志；
  - 执行出错时，切到日志 tab，并出现红色的 ▶；
  - 返回 1001 行的查询显示 `1000+ 行`；
  - `create table` 之后树里出现这张表；
  - 导出的 CSV 文件在当前目录。
- [x] golden：结果区标题与按钮在几种宽度下的舍弃；日志 tab。

## F3.9 格式化 · 状态：passed（e8f8e8c；e2e 8921890）

- **依赖**：F3.6
- **涉及**：`internal/sqlkit`（format，embed sql-formatter）

**开发**

§9.5 的全部内容，另加：

- [x] sql-formatter 用 15.9.0，文件放在 `internal/sqlkit/sql-formatter.min.js`，旁边放它的 MIT LICENSE；spike 的几项检查在 golden 里重跑。
- [x] 范围：NORMAL 下是当前语句，不含 `;`，`;` 留在原处；VISUAL 下字符选区就是选中的文字，V-LINE、V-BLOCK 是覆盖到的整行，格式化后退出 VISUAL。整次格式化算一个撤销步，结果和原文相同时不记撤销步。格式化后光标照 vim 的 `gq` 停在格式化文字的最后一行（行首第一个非空白字符）。
- [x] `keyword_case` 只接受 `lower` / `upper` / `preserve`，其他值启动报错；`formatprg = ""` 用内置的。失败时 toast「格式化失败：<错误>」。
- [x] 在 Cmd 里异步执行，回来时缓冲区已经变过就丢弃结果。
- [x] `gq` 做成编辑器的操作符：`gq{移动}`、`gqq` / `gqgq`、VISUAL 下按选区；从 `[keys.console]` 去掉 `gq`（§9.5）。
- [x] 内置格式化 5s 超时，`vm.Interrupt` 打断；`keyword_case` 同时设 `dataTypeCase`；输出为空按失败处理（§9.5）。
- [x] 选项：`language` 为 postgresql，`keywordCase` 取 `keyword_case`（默认 lower），`tabWidth` 取 `tab_width`；配置加 `keyword_case`、`formatprg`。
- [x] Unicode 属性名的替换按名字逐个断言次数（15.9.0 为 Alphabetic 3、Mark 1、Decimal_Number 1，§9.5），升级打包文件后次数一变测试就失败。
- [x] `formatprg`：`sh -c`，stdin 输入、stdout 输出，5s 超时；失败时 toast 显示 stderr 的第一行，缓冲区不变。

**验收**
- [x] 格式化的 golden 测试通过，PG 和 MySQL 各准备若干条语句。
- [x] 单测断言 VM 只建一次（调两次 Format，是同一个 VM）；「首次格式化小于 300ms」挪进 `BenchmarkFirstFormat`，默认的 `go test ./...` 不断言墙钟时间（审查时实测：负载一高就误报）。
- [x] `gqap` 格式化光标所在段落的语句，不进入 INSERT；超时时 toast、缓冲区不变。
- [x] 配置 `formatprg` 后，改用外部命令格式化；格式化失败时，缓冲区内容不变。

## F3.10 console 的补全 · 状态：passed（e8f8e8c；e2e 8921890）

- **依赖**：F3.5、F3.6
- **涉及**：`internal/sqlkit`（补全上下文）、`internal/app`

**开发**
- [x] `sqlkit.CompletionContext(text, pos, dialect)` 建在扫描器上，参考 lazysql 的 `sql_context.go`（别名、CTE、子查询层级、`schema.`），用例参考它的测试；`WhereContext` 保留为单独的函数（§9.7）。
- [x] 候选按 §9.7 的表分组：表名取这个 console 选的 schema（F3.11 之前取树当前的 schema），输入 `schema.` 后只列那个 schema 的表；CTE 名算作表；列在第一次用到时从 `Meta` 获取，与打开表共用列缓存。
- [x] 快速 SQL 的补全也改用 `CompletionContext`。
- [x] 按键：`C-n` 手动唤起；`esc` 关掉列表并退出 INSERT（同 nvim-cmp，WHERE 和快速 SQL 仍是两步）；`↵` 接受后文字有变化就接受，没有变化就换行（§9.7）。
- [x] 自动弹出：输入标识符字符且前缀不为空时；或者刚输入 `.`、前面的限定名能解析时（别名 / 表名 → 列，schema 名 → 表），前缀为空也弹。输入非标识符字符、方向键、离开 INSERT、没有匹配时关闭；粘贴不弹。接受算作输入，属于这次 INSERT 的撤销步。
- [x] 缓存里没有的列从 `Meta` 取（与打开表共用），取回时 console 还在 INSERT、光标还在原处就重新补全；每次补全每张表最多取一次。插入的只是表名，不带 schema、不加引号（需要加引号的表名是已知上限）。

**验收**
- [x] 上下文判断有单元测试。
- [x] e2e：
  - 输入 `select * from t_o` 时，候选中出现 `t_order`；
  - 输入别名加 `.` 后，列出这张表的列；
  - CTE 的名字会出现在候选中；
  - 快速 SQL 里 `select o. from t_order o` 的 `o.` 后面列出 t_order 的列。

## F3.11 schema 下拉框（PG） · 状态：passed（e8f8e8c；e2e 8921890）

- **依赖**：F3.8
- **涉及**：`internal/app`（consoleTab.Schema、dropdown）、`internal/db`

**开发**
- [x] console 标题上的 `<库名>.<schema> ▾`，库名记在 Session 上；`gs` 或点击打开下拉框，复用 `app/dropdown.go`，加一种 dropSchema，位置和样式照 §8.6。
- [x] search_path 的比较与重设（§8.6「执行方式」）：SET 放在同一次 `Worker.Run` 里、排在语句前面，不进日志、不占结果 tab；只在不一致时 SET；这次执行里有首词为 set / reset / discard 的语句就把记下的值作废。`Main` 处在出错的事务里（TxStatus 为 `E`）时跳过 SET、清掉记下的值，执行结束时还在事务里就不记这次的 SET（§8.6「只在事务外记住 search_path」）。SET 本身失败（比如连接出了问题）按执行出错处理：日志记这条 SET 的报错、切到日志、不标红 ▶，后面的语句不执行。
- [x] 重跑用来源 console 此刻选的 schema，console 已关就用它关闭时的选择。`gs` 绑在 `[keys.console]`，Action `console.schema`「切换 schema」。console 的补全用它选的 schema，快速 SQL 仍用树的。
- [x] console 里 INSERT 下 `C-n` 手动唤起补全写在 console 的按键处理里、没进 keymap，用 `ponytail:` 标出，要能改键时再加 console 的 INSERT 作用域。
- [x] 新 console 默认用树当前的 schema，之后两者互不影响。
- [x] MySQL 的 console 不显示这个下拉框（M5 时生效）。

**验收**
- [x] 集成测试与 e2e：
  - console 选择 `agentable` 后，`select * from <agentable 中的表>` 能执行成功；
  - 两个 console 分别选择不同的 schema，交替执行，结果都正确；
  - console 里自己 `set search_path` 后，下一次执行照下拉框的选择重新设置。
- [x] 补回 f0.2 里和 schema 下拉框有关的 e2e：160 与 200 宽的 console 标题（对象名 + `<库名>.<schema> ▾` + `▶ run ↵`）；各宽度下下拉框按钮的退让顺序（`▶ run` > 下拉框 > `↵`）。

---

以下为 M2 / M3 用户验收的改进项（2026-09-29）。用户的 13 条意见里，第 11 条（console 的事务模式与提交 / 回滚）和第 12 条（一个 session 多个连接）排进 M5，见 m5-workspace/task.md 开头。

## F3.12 补全按词首匹配 · 状态：passed（d0d1d2b；e2e e42b702）

- **依赖**：F3.10
- **涉及**：`internal/app`（补全的候选过滤）

**开发**
- [x] 取代 F1.14 的「首字符相同」：输入的第一个字符落在候选的词首就算匹配（开头，或 `_ . - $` 之后，或小写到大写的切换处），其余照 fzf（§9.7）。WHERE、快速 SQL、console 共用。
- [x] fzf 只给得分最高的一种对齐，它的首字符不在词首时，从每个首字符相同的词首起，用模式的其余部分再匹配一次后面的字符，匹配上就保留，高亮按这次的位置画。
- [x] 词首只算上面列的几种，空格、字母后面的数字都不算：`nu` 只补出 `null`，不出 `is null`。

**验收**
- [x] 单测：`evt` → `mt_event`、`tord` → `t_order`、`ev` → `t_event` 匹配；`x` 不匹配 `max`、`exists`。
- [x] e2e：快速 SQL 输入 `select * from evt` 出现 `t_event`（seed 里没有 `mt_event`）。

## F3.13 自动配对括号与引号 · 状态：passed（d0d1d2b；e2e e42b702）

- **依赖**：F3.6
- **涉及**：`internal/editor`（INSERT 下的配对、导出的规则函数）、`internal/app`（editInput）、`internal/config`（`autopairs`）

**开发**
- [x] 按 §7.9「自动配对」：console 的 INSERT、WHERE 输入框、快速 SQL；配对条件、跳过右括号、退格成对删除；配置 `autopairs`。
- [x] 编辑器里做成一个选项，nvim 差分测试的生成器和比对都关掉它：导出字段 `AutoPairs`，和 `TabWidth` 并列，零值是关，app 按配置打开。配置 `autopairs` 在顶层，布尔值，类型不对时照现有规则启动报错。
- [x] 带次数的 INSERT 重放时照样配对：`3i(<Esc>` 得到 `((()))`；`o` / `O` 带次数时每个重复出来的行各配一对，换行前先把光标移到行尾：`3o(<Esc>` 得到三行 `()`（reviewer 实测原来右括号全堆到最后一行）。粘贴在所有地方都原样插入，不配对、不跳过（WHERE 和快速 SQL 的粘贴原来逐字符走了配对）。REPLACE 模式和 `:` / `/` 命令行不配对。成对删除只管 BS / `C-h`，`C-w` / `C-u` 照旧。
- [x] 配对和删词的规则写成 editor 导出的纯函数，WHERE 和快速 SQL 在 `app.editInput` 里调用。ui 不 import editor，否则测试会循环 import（editor 测试 → keymap → config → ui）。
- [x] 接受补全时，插入的内容以引号结尾、光标后面正好是同一个引号，就把这个引号一起替换掉：WHERE 里 `status = '█'` 接受 `'done'` 得到 `status = 'done'█`，而不是 `'done''`。

**验收**
- [x] 单测覆盖配对、不配对（`don't`、光标后是字母）、跳过、成对删除。
- [x] e2e：WHERE 输入 `status = '` 后得到 `status = '█'`，接着输入 `done'` 得到 `status = 'done'█`；console 里输入 `count(` 得到 `count(█)`，一次 `u` 撤掉这次 INSERT 的全部内容；`autopairs = false` 时不配对。

## F3.14 输入框删词 · 状态：passed（d0d1d2b；e2e e42b702）

- **依赖**：F3.6
- **涉及**：`internal/app`（editInput）、`internal/editor`（INSERT 和命令行的 `M-BS`、导出的切词函数）

**开发**
- [x] 所有单行输入框加 `C-w`、`C-u`，所有能输入文字的地方把 `M-BS` 当作 `C-w`（§7.9「删词」）。
- [x] 单行输入框都经过 `app.editInput`，`C-w`、`C-u`、`M-BS` 加在这一处；`C-w` 的切词用编辑器命令行里 `C-w` 的那一份，导出后两处共用。
- [x] 单元格刚进入编辑、文字还是全选时，`C-w` / `C-u` 和 BS 一样，清空全部文字。
- [x] console 的 INSERT 和 `:` / `/` 命令行里，`M-BS` 完全等同于 `C-w`：INSERT 下先断开撤销步，带次数重放时记成 `<C-w>`。NORMAL / VISUAL 下 `M-BS` 什么也不做。

**验收**
- [x] 单测：`C-w` 的词划分和 vim INSERT 下一致（`select foo.bar|` → `select foo.`、`a  |` → 空）。
- [x] e2e：WHERE、面板、单元格编辑、console INSERT、console 的 `:` 命令行里，`C-w` 和 `M-BS` 都能删掉前一个词；`C-u` 删到行首。

## F3.15 选项浮层用 Tab 选择 · 状态：passed（d0d1d2b；e2e e42b702）

- **依赖**：F2.3、F2.4
- **涉及**：`internal/app`（options、segments 作用域）、`internal/keymap/default.toml`

**开发**
- [x] 按 §10.2「键盘」：非时间列的选项浮层是作用域 `options`，`Tab` / `S-Tab` / `C-n` / `C-p` / `↑` / `↓` 移动并绕回；时间列的浮层是作用域 `segments`，`Tab` / `S-Tab` 切段、`↑` / `↓` 加减、`C-n` / `C-p` 在选项行里移动。`cell` 里只剩编辑文字的键。
- [x] `cell.up` / `cell.down` 以后只用来加减时间的段，id 不改（用户的 config.toml 里已经写着这些 id），去掉给非时间列移动选项的分支。
- [x] 浮层被 ▾ 收起时，两个浮层作用域都不生效，`Tab` / `↑` / `↓` 在单元格里不做事。时间浮层总有「◷ 现在」这一项，所以只要展开着，`segments` 就生效。
- [x] keymap 认可的作用域名加上 `segments` 和 `keyhelp`，供配置校验用。

**验收**
- [x] e2e：paid 列编辑时 `Tab` 选中 true、再 `Tab` 到 false、`S-Tab` 回来，`↵` 应用；created_at 列 `Tab` 仍是切段。

## F3.16 VISUAL 选区颜色 · 状态：passed（d0d1d2b；e2e e42b702）

- **依赖**：F3.6
- **涉及**：`internal/ui`（主题 token、console、Input）

**开发**
- [x] 新增主题 token `visual`（默认 #2d3f76），console 的三种 VISUAL 选区和输入框的「全选」都用它（§7.3）。

**验收**
- [x] golden：console 的 VISUAL 选区用 `visual` 色；主题里写 `visual` 后生效。
- [ ] 通过后决策者在用户的 ristretto 主题里加 `visual = "#6c6a6d"`（与当前行同色，用户要求）。等验收版本重建时再加：用户手上的旧版本不认这个 token，加了会启动报错。

## F3.17 `?` 键位帮助 · 状态：passed（d0d1d2b；e2e e42b702）

- **依赖**：F3.6
- **涉及**：`internal/app`（which-key 浮层）、`internal/keymap/default.toml`

**开发**
- [x] 按 §6.5「键位帮助」：`?` 在表格、树、引导页、结果区打开当前上下文的全部键位（作用域 `keyhelp`），前缀成组、按下往下一层，`esc` 关闭；`<leader>?` 在哪里都能打开，console 里用它。
- [x] 内容：打开时那个上下文合并后的键树，也就是 resolver 用的那一份，含用户映射，按绑定顺序列出。keymap 导出一个按前缀列出子节点的函数，which-key 也改用它。console 里只列 keymap 的绑定（↵ 执行、`gs`、normal、global），vim 本身的键不列。
- [x] 样子照 which-key 浮层：靠底、占满宽度、分列排列；上边框显示当前前缀，根一层显示打开帮助的那个键（从 keymap 读 `keyhelp.open`，默认 `?`）；前缀项和 which-key 一样画成 `g → …`。
- [x] 按来源分组（用户验收时要求，要能看出一个键属于哪个作用域、生效的是哪一层配置）：每一层的键按它来自的配置表分组，组标题就是表名，如 `[map.grid.normal]`、`[keys.grid]`、`[keys.normal]`、`[keys.global]`，组的顺序同 §6.4 的解析顺序。只列生效的绑定，被高层遮住的键不在低层的组里重复出现。前缀项放在贡献它的最高一层的组里，进入下一层后照样分组。which-key 浮层和它共用这套画法。
- [x] 已知上限，用 `ponytail:` 标出：用户在不同的表里造出跨表的歧义键（如 `[map.grid.normal]` 的 `xx` 和 `[keys.grid]` 的 `x`）时，`x` 归在建出这个节点的那一组，`xx` 这一层进不去。默认键位没有这种情况，`ambiguities()` 也只查同一张表。
- [x] 所有列出来的 Action 都有标题（§6.7「标题」），没有标题的补上，加单测。
- [x] 按键：按前缀往下一层，`<BS>` 回到上一层；按到一个绑定时先关掉帮助，再照原来的上下文执行它，等于当场按了一遍「前缀 + 这个键」；列表里没有的键不做事；`esc` 关闭；点击一项等于按下它，点浮层外面关闭。global 的键（`C-p` 等）照常生效，先关掉帮助。打开时模式块显示 COMMAND。
- [x] 滚动：放不下时 `C-d` / `C-u` 滚半屏（nvim which-key 的默认键），滚轮一次滚 1 行，不加滚动提示。
- [x] 键位：`[keys.grid]`、`[keys.tree]`、`[keys.landing]` 各加 `"?" = "keyhelp.open"`，结果区落到 grid 上照样能用；`[keys.normal]` 加 `"<Leader>?" = "keyhelp.open"`；`[keys.keyhelp]` 绑 `<Esc>`、`<BS>`、`<C-d>`、`<C-u>`。

**验收**
- [x] e2e：在表格里按 `?` 列出 `hjkl`、`go`、`gl` 等和它们的名字，包括用户配置里加的键，它们出现在对应的组标题下（如 `[map.grid.normal]`）；按 `g` 进入 `g` 这一层；console 里 `<leader>?` 能打开，`?` 仍是反向搜索。

## F3.18 从树和面板打开表一律新开 tab · 状态：passed（9a795c4；e2e 2702236）

- **依赖**：F3.7
- **涉及**：`internal/app`（openTable、openTarget）

**开发**
- [x] 按 §12「表」和 §7.8「打开已有的表」：`↵` 在表已经开着时切过去（多个时列出选择），没开就在目标 pane 新开 tab，不替换当前的表 tab 或 console tab；`C-t` 总是新开；引导 tab 照旧被替换。
- [x] 原来那条「当前 tab 有修改或者是 console 时才新开」的规则一起去掉。

**验收**
- [x] e2e：焦点在 ① 的表 tab 上时，从树按 `↵` 打开另一张表，① 多了一个 tab，原来的表 tab 还在；焦点在 console 上时同样新开。

## F3.19 工作区的 pane 节点 · 状态：passed（9a795c4；e2e 2702236）

- **依赖**：F1.12
- **涉及**：`internal/app`（workspace）

**开发**
- [x] 按 §7.8「工作区节点」：pane 节点显示 `pane-<n>`；在 pane 节点上 `↵` / 单击是展开折叠，在 tab 节点上才聚焦过去。
- [x] pane 节点不画图标，图标那一格照引导 tab 的做法留空，让名字和别的节点对齐。结果区的节点也叫 `pane-<n>`，n 用它的 ⟨n⟩。
- [x] 别的 window 里的 pane 节点，`↵` 也是展开 / 折叠；没有 tab 的 pane 节点没有子节点，`↵` 不做事。

**验收**
- [x] e2e：单击 pane 节点只展开 / 折叠，焦点和点树上别的节点一样到树上，不跳到那个 pane；单击 tab 节点，焦点到对应的 pane 和 tab。

## F3.20 错误栏 · 状态：passed（9a795c4；e2e 2702236）

- **依赖**：F3.8
- **涉及**：`internal/ui`（错误栏）、`internal/app`（表格的取数与保存、console 执行）

**开发**
- [x] 按 §7.8「错误栏」：表格的取数 / WHERE 报错、保存失败、console 的执行报错都显示在出错 pane 的底部；`×` 和 NORMAL 下的 `esc` 关闭（`pane.error.close`）；同类操作下一次成功时自动消失。
- [x] 表格出错时照旧画着上一次的数据（§7.6）；查询条右侧只留成功的提示。出错后 ORDER、LIMIT、PAGE 退回到画着的数据用的值，失败的 WHERE 留在输入框和请求里，`R` 能重试。
- [x] console 里 NORMAL 下有错误栏时 `esc` 关它，VISUAL 下 `esc` 照旧退出 VISUAL。
- [x] 位置：console 的报错把 PG 的 Position（语句里第几个字符）换算成 console 文件里的位置，显示成「位置：第 3 行第 12 列」；表格取数的报错不显示位置（SQL 是拼出来的，对不上用户写的 WHERE），用 `ponytail:` 标出上限。
- [x] 行：第一行 `[SQLSTATE] <Message>`，下面依次是 DETAIL、HINT、位置，一项一行；DETAIL 自带的换行照拆；每行放不下时右边截短加 `…`，不折行；超过 6 行时第 6 行画成 `…`。
- [x] 保存失败的第一行：数据库报错为 `[23505] id = 42：<Message>，已回滚`（中间截短照 §10.3），下面照样接 DETAIL / HINT；行数据变了或行不存在、「WHERE 里不能有 ;」这类不是数据库返回的，没有 SQLSTATE，只有文字。保存被取消（「已取消，已回滚」）仍在查询条右侧，不进错误栏。
- [x] 错误栏占 pane 内容区最下面的几行，表格 / console 的可见高度相应变矮，不盖住内容。
- [x] 一个 tab 同时只显示一条错误，后来的替换先前的；取数成功只清取数的错误，保存成功只清保存的错误，console 每次开始执行就清掉上一次的，编辑单元格不清。错误栏属于 tab，切走不显示，切回来还在。
- [x] `[keys.normal]` 加 `"<Esc>" = "pane.error.close"`：console 的 VISUAL 下照旧交给 vim；编辑器有待定键（含等着的次数）时按键直接给编辑器，不经过这个绑定。`×` 点击执行 `pane.error.close <pane id>`。
- [x] 结果区 `R` 重跑出错时，错误显示在来源 console 的 tab 上；来源 console 已关就只记日志。

**验收**
- [x] e2e：WHERE 写错，表格下方出现错误栏，表格还是原来的数据，`esc` 关掉；保存失败，错误栏显示「id = …：…，已回滚」，修好再保存后自动消失；console 执行出错，console 下方出现错误栏，带 SQLSTATE 和位置。
- [x] golden：错误栏（含 DETAIL / HINT 多行、截短）。

## F3.21 字段的前置校验 · 状态：passed（9a795c4；e2e 2702236）

- **依赖**：F2.3、F2.4
- **涉及**：`internal/app`（单元格编辑）、`internal/ui`（输入框的波浪线、提示框）

**开发**
- [x] 按 §10.7：各类型的检查、提示文字、波浪下划线、提示框的位置；不合法时 `↵` 与点击别处不提交，`esc` 放弃这次输入。
- [x] 合法写法以 PG 17 的输入函数为准（worker 实测）：int 接受前后空格、`+5`、`0x1F`、`1_000`，`010` 读成 10；bool 接受唯一前缀（`tr`、`ye`、`of`，单个 `o` 不行）和前后空格；numeric、real、double 都接受 `inf`、`+Infinity`、`1_000.5`、`0x10`（real / double 的 `0x` 补上 `p0` 再解析），float8 的 `1e400` 提示「超出 float8 的范围」，float4 同理；numeric(p,s) 按 s 位舍入后整数部分超过 p−s 位、或者写 Infinity，提示「超出 numeric(10,2) 的范围」；uuid 接受带花括号、不带连字符、每 4 位一个连字符（只有一边花括号不行）。时间的特殊词按类型分：time 只认 `now`、`allballs`；date / timestamp 认 `epoch`、`infinity`、`today`、`now` 等。集成测试把每条写法拿去 `select '<写法>'::<类型>` 核对。
- [x] 日期 / 时间的校验另用一套比 §10.2 分段宽的 ISO 规则（§10.7 表）：`2026-09-01` 给 timestamp、`2026-09-01 10:00`、`2026-09-01T10:00:00`、`10:00` 给 time、`24:00:00` 都算合法；分段仍只认输出格式。PG 还认的其他写法（如 `2026/09/20`、月份名）会被挡住，用 `ponytail:` 标出上限，用户碰到再放宽。
- [x] 提示框和选项浮层同时出现时上下叠放：提示框紧贴输入框，选项浮层（或时间浮层）接在它下面；翻到上方时顺序反过来，提示框仍紧贴输入框。
- [x] 不合法时所有离开编辑的途径都挡住：`↵`、点别处、滚轮、`C-p`、`C-s`、换焦点的键都不做事，提示框一直显示；只有 `esc` 退出，`C-c` 等同 `esc`。
- [x] 豁免条件是「文字和进入编辑时一样」，不是「空文字」：清空一个非空的数字格要提示。
- [x] `esc` 回到进入这次编辑之前的值：这一格原来就改过的，回到改过的值，不是数据库里的原值。校验函数是 app/edit.go 里的纯函数，单测按类型逐条覆盖。

**验收**
- [x] 单测覆盖表里每一类的合法与不合法写法，包括整数范围、`now`、带花括号的 UUID。
- [x] e2e：amount 列输入 `10d`，出现「不是有效的数字」、`↵` 不提交；改成 `10` 后 `↵` 提交；`esc` 回到原值。

## F3.22 撤回一格的修改、改动行号标黄 · 状态：passed（9a795c4；e2e 2702236）

- **依赖**：F2.1
- **涉及**：`internal/app`（grid.revert）、`internal/ui`（行号颜色）、`internal/keymap/default.toml`

**开发**
- [x] `[keys.grid]` 加 `r` → `grid.revert`（§10.1）。
- [x] 有修改的行号用 `warn` 色，保存失败的那一行 `error` 色优先。
- [x] 结果区的表格不能编辑，`r` 不做事。

**验收**
- [x] e2e：改两格后在其中一格上按 `r`，只有这一格恢复；有修改的行号是黄色，全部撤回后恢复。

## F3.23 查询条的工具按钮与自动刷新 · 状态：passed（9a795c4；e2e 2702236）

- **依赖**：F3.20
- **涉及**：`internal/ui`（querybar）、`internal/app`（自动刷新、停止）

**开发**
- [x] 按 §7.8「工具按钮」：三组七个按钮的顺序、底框、默认颜色、放不下时整组舍去；新图标 `row_add`、`row_delete`、`auto_refresh`、`stop`（§7.7），颜色都能在 `[icon]` 里覆盖。
- [x] 自动刷新：下拉框选间隔，触发条件和有修改时暂停（§7.8「自动刷新」）。
- [x] 停止：这个 tab 有请求在跑时可点，等同 `C-c`。
- [x] `+` / `−` 按钮在 F3.24 接上，这里先画出来、不可点。
- [x] 第二行放不下时的让位顺序：先让统计，再依次让视图组、查询组、数据组，然后 COLS、PAGE、LIMIT、ORDER；保存结果的优先级不变。
- [x] 打开自动刷新下拉框的 Action 是 `grid.refresh.auto`「自动刷新」，没有默认键，面板里能执行；用通用 dropdown，选项：关 / 2s / 5s / 10s / 30s / 60s。按钮让位了、从面板打开时，下拉框锚在 pane 查询条的右端，右边对齐 pane 的右边框（同 schema 下拉框）。
- [x] `[icon]` 里写的 `fg` 只替换按钮「亮着」时的颜色：停止是有请求在跑时的 `error`，自动刷新是开着时的 `warn`；停止空闲时照旧 `dim`，自动刷新关着时用 `info` token。
- [x] 「有请求在跑」指这个 tab 的取数、计数或保存还没回来，dataTab 记一个在途计数。
- [x] 每个 tab 一条 `tea.Tick` 链，改了间隔旧链作废；每次触发检查：是某个可见 pane 的当前 tab、没有未保存的修改、没有在编辑单元格、没有请求在跑，有一条不满足就跳过这一轮。满足时和 `R` 一样重取当前页和计数，但不清掉保存结果那行提示。有修改时按钮照样是 `warn` 色带间隔，不加「已暂停」的提示。

**验收**
- [x] golden：工具按钮在几种宽度下的样子（含自动刷新开着、有请求在跑）。
- [x] e2e：开 2s 自动刷新后，另一条连接改了数据，2s 内表格跟着变；有未保存的修改时不刷新；慢查询时点停止能取消。

## F3.24 新增与删除行 · 状态：passed（9a795c4；e2e 2702236）

- **依赖**：F3.22、F3.23
- **涉及**：`internal/app`（dataTab 的新行与删除标记）、`internal/db/postgres`（DELETE / INSERT 生成）、`internal/ui`（行号 `+` `−`、删除线）、`internal/keymap/default.toml`

**开发**
- [x] 按 §10.6：`o` / `+` 新增行，`dd` / `−` 标删除，`r` 与再按 `dd` 的撤回规则；`[keys.grid]` 加 `o` → `grid.row.add`、`dd` → `grid.row.delete`。
- [x] 保存：同一个事务里 DELETE → UPDATE → INSERT，每条恰好 1 行；计数规则；先看 lazysql 的 `ExecutePendingChanges`，提交说明里写明。
- [x] 新行记在「第几页、排在页里第几行后面」上，改 WHERE / ORDER / LIMIT、刷新之后仍放回这个位置；页号超出现有页数或位置超出本页行数时，挂到最后一页的末尾，不能看不见。
- [x] 新行里没改过的格用 `dim` 色画 `<default>`（同 `<null>`），改过的格照「已修改」的样式画；转置时新行变成一列，操作不变。
- [x] 删除标记按行标识记，翻页、刷新后这一行出现时照样标着；标删除的行上原有的修改先留着，保存时丢掉，取消删除后恢复。新行上 `r`、`dd` 都直接去掉这一行（§10.1）。
- [x] SQL：`DELETE FROM t WHERE <行标识>`，行标识的比较和 UPDATE 共用一段（复合键用 AND 连起来）；INSERT 只写改过的列，选了 DEFAULT 的写 `DEFAULT`，一格都没改写 `DEFAULT VALUES`；Tag 必须是 `DELETE 1` / `INSERT 0 1`，否则整体回滚。
- [x] 标了删除的行不能进入编辑（不提示，先按 `r` 取消删除）；光标在新行上时状态栏的行显示行号栏的标记 `+`。
- [x] 保存失败的文字：INSERT 没有行标识，写成「新增的第 2 行：<错误>，已回滚」（按这个 tab 里新行的显示顺序数），Tag 不是 `INSERT 0 1` 时写成「新增的第 2 行没有插入，已回滚」；DELETE 影响行数不是 1 时写成「id = 42 的行不存在，已回滚」，数据库报错照 UPDATE 写成「id = 42：<错误>，已回滚」。

**验收**
- [x] 集成测试（自建库）：新增一行只填部分列，其余取默认值；一格都不填时 `DEFAULT VALUES`；删除一行；同一次保存里新增、修改、删除混在一起；其中一条失败时整体回滚。
- [x] e2e：`o` 新增一行、填两格、`C-s` 后重新加载能看到这一行；`dd` 标删除、再按 `dd` 取消；标删除后保存，这一行没了。

---

以下为第二轮改进（2026-09-30，用户试用 F3.12–F3.24 后提出）。用户决定：这一轮通过后不停下来验收，直接开工 M4，M2、M3、M4 一起验收；审查节点拉长，这一轮只有一个节点。命令面板里 tab 的层级显示排进 M4 F4.3。

## F3.25 console 里的 `?` 打开键位帮助 · 状态：todo

- **依赖**：F3.17
- **涉及**：`internal/keymap/default.toml`

**开发**
- [ ] `?` 改绑在 `[keys.normal]`，去掉 grid / tree / landing 里各自的 `?`：所有 pane 的 NORMAL 下都打开键位帮助，console 的 NORMAL 下不再是 vim 的反向搜索（用户：不用反向搜索，`/` 照旧），VISUAL 下和 `d?` 这类操作符后面的 `?` 仍交给 vim；`<leader>?` 照旧（§6.5、§6.8）。帮助面板里 `?` 归在 `[keys.normal]` 组下。

**验收**
- [ ] e2e：console 的 NORMAL 下按 `?` 打开键位帮助；INSERT 下 `?` 照常输入。

## F3.26 工具按钮重新分组 · 状态：todo

- **依赖**：F3.23
- **涉及**：`internal/ui`（querybar）

**开发**
- [ ] 按 §7.8「工具按钮」：四组 `+ −` ｜ 保存 刷新 ｜ 自动刷新 停止 ｜ 转置；一组共用一块 `sep` 底，组内按钮之间不再各留底色的空隙，转置不画底色；悬停时只亮那一个按钮。让位顺序：统计 → 转置 → 自动刷新 / 停止 → `+ −` → 保存 / 刷新 → COLS、PAGE、LIMIT、ORDER。

**验收**
- [ ] golden：几种宽度下的四组按钮，含悬停、自动刷新开着、有请求在跑。

## F3.27 SQL 的表名、列名、运算符高亮 · 状态：todo

- **依赖**：F3.5、F3.6
- **涉及**：`internal/ui`（主题 token、console、WHERE 输入框）、`internal/app`（catalog 查名字）

**开发**
- [ ] 新增主题 token `sql_table`、`sql_column`、`sql_operator`（§7.3）。console 和 WHERE 输入框共用一套高亮：关键字（含 `and` `or` `in` `is` `null`）、字符串、数字、注释、运算符（`=` `<>` `!=` `<` `>` `<=` `>=` `||` `::` 等）、表名、列名。WHERE 输入框原来不高亮，聚焦与否都画。
- [ ] 表名、列名只按词法加 catalog 缓存判断，不做语法分析：标识符和缓存里某张表同名就是表名，和当前语句引用到的表（WHERE 里是当前表）的某一列同名就是列名；别名、CTE 名不着色，用 `ponytail:` 标出。列还没取回时不着色，取回后下一帧就有。
- [ ] 细节（worker 提议，已定）：表名算会话里所有 schema 的表，`s.t` 只给 `t` 着色；列名只算这条语句引用到的表（按 `CompletionContext` 找，WHERE 用当前表）的列，引用到的表列没缓存时发一次和补全相同的取列请求，每张表一次；不加引号的不分大小写，带引号的按原样；既是表名又是列名时按表名；sqlkit 的运算符 token 全部用 `sql_operator`，含 `select *` 的 `*`。

**验收**
- [ ] golden：console 里 `select * from t_order where user_id = 'df'` 和 WHERE 里 `user_id = '2' and status = 'running'` 各部分的颜色；主题里写这三个 token 后生效。

## F3.28 `SPC r` 显示 / 隐藏结果区 · 状态：todo

- **依赖**：F3.8
- **涉及**：`internal/app`（结果区）、`internal/keymap/default.toml`

**开发**
- [ ] Action `result.toggle`「显示 / 隐藏结果区」，`[keys.normal]` 绑 `<Leader>r`：隐藏时结果 tab 都保留，再按一次原样回来；隐藏着再执行 SQL 时自动显示；还没有结果区时不做事。焦点在结果区时隐藏，焦点回到上一个聚焦的 pane。
- [ ] 隐藏时结果区从布局里拿掉，pane 和 tab 保留，工作区和面板都当它不存在；隐藏中还在跑的 SQL 跑完照常放进 tab，结果区仍隐藏，只有新的一次执行才让它出来；被 zoom 的正是结果区时先取消 zoom；隐藏状态不持久化（M6 布局文件再说）。

**验收**
- [ ] e2e：执行一条 SQL 后 `SPC r` 结果区消失、console 变高，再按回来且结果 tab 还在；隐藏时再执行，结果区自动出现。

## F3.29 `SPC b` 展开树时定位到当前 tab · 状态：todo

- **依赖**：F1.12、F3.19
- **涉及**：`internal/app`（tree.toggle）

**开发**
- [ ] `SPC b` 把树展开时，焦点移到树上，光标落在当前 tab 对应的节点（同 nvim-tree 的 find_file）：表 tab 是 schema 下的表节点，沿途折叠着的父节点展开；console、引导 tab 是工作区里的 tab 节点。收起树时照旧。
- [ ] 定位的是焦点所在 pane 的当前 tab（焦点在结果区就是结果区的当前 tab）；沿途展开的父节点记成手动展开过；树正在过滤、目标被滤掉，或者表还不在树上（catalog 没加载完）时光标不动。

**验收**
- [ ] e2e：焦点在 t_order 的 tab 上，树收起后 `SPC b`，树展开、焦点在树上、光标在 t_order 节点；在 console 上按则光标在工作区的 console 节点。

## F3.30 工作区的缩进与日志图标 · 状态：todo

- **依赖**：F3.19
- **涉及**：`internal/app`（workspace）、`internal/ui`（图标）

**开发**
- [ ] tab 节点比 pane 节点多缩进一级（现在两者看起来同层，§7.8「工作区节点」）。
- [ ] 结果区的「日志」tab 加图标 `log`（§7.7），工作区节点和结果区的 tab 栏都画，名字和其他 tab 对齐。

**验收**
- [ ] golden：工作区 window → pane → tab 三级缩进，日志 tab 带图标且对齐。

## F3.31 WHERE 历史 / 收藏下拉 · 状态：todo

- **依赖**：F1.5
- **涉及**：`internal/app`（where 下拉）、`internal/config`（state.json）、`internal/keymap/default.toml`

**开发**
- [ ] `[keys.where]` 加 `Tab` / `↓` 下一项、`S-Tab` / `↑` 上一项，到头绕回（和 `C-n` / `C-p` 相同）。
- [ ] 打开时列出全部收藏和历史，不按输入框里已有的文字过滤；开始输入后才过滤。每一项前面画图标：收藏 `star`、历史 `history`（§7.7），不再画「收藏」「历史」两个组标题（有图标就不配文字）；收藏在前，组内从新到旧。
- [ ] 历史每张表最多 100 条（原来 50），满了丢最旧的；去重仍把相同的挪到最前。收藏不限。快速 SQL 的历史仍是 50 条，拆成两个常量。
- [ ] 图标：`log` nf-fa-file_text_o（U+F0F6，ascii `L`）、`star` nf-fa-star（U+F005，ascii `*`）、`history` nf-fa-history（U+F1DA，ascii `h`）。「开始输入」指打开后输入框的文字第一次变化，之后按整个输入框过滤。

**验收**
- [ ] e2e：`C-r` 打开后不输入就能看到收藏和历史，带各自的图标；`Tab`、`↓`、`S-Tab`、`↑` 都能移动；写满 101 条后最旧的一条不见了（单测即可）。

## F3.32 整行复制粘贴与复制高亮 · 状态：todo

- **依赖**：F3.24、F3.1
- **涉及**：`internal/app`（grid 的 yank / paste）、`internal/editor`、`internal/ui`（`yank` token）、`internal/keymap/default.toml`

**开发**
- [ ] 表格照 vim 的行：`yy` 复制整行（系统剪贴板里是 TSV），`yl` 复制单元格（原来 `yy` 的功能），`p` 在光标下面粘贴成一个新行（同 `o` 的新行，§10.6），各格的值取复制的那一行；行标识列里有默认值的（serial / identity）填 `<default>`，其余照抄。没复制过行时 `p` 不做事。结果区只读，`p` 不做事，`yy` / `yl` 照常。
- [ ] 复制高亮（同 nvim 的 `vim.hl.on_yank`）：表格的 `yy` / `yl`、console 里所有的复制（`yy`、`y{移动}`、VISUAL 下 `y`），被复制的范围用新 token `yank`（§7.3，默认黄底）闪 150ms。
- [ ] 细节（worker 提议，已定）：
  - `yy` 写进剪贴板的是可见列、按显示顺序的 TSV，不带表头；NULL 和 `<default>` 写成空；值里有 Tab、换行或 `"` 时加双引号、内部 `"` 写两遍（encoding/csv，分隔符 Tab）。
  - 给 `p` 用的行另存一份：所有列（含隐藏列和未保存的修改），记下是哪张表，在别的表里 `p` 不做事；`yl` 不动它。`p` 只粘一行，忽略次数，光标位置和只读提示同 `o`。
  - 源行里本来是 `<default>` 的格仍是 `<default>`，NULL 照抄。catalog 读 `attidentity`，把 identity 列的默认值记成 `generated by default as identity` / `generated always as identity`，所以 identity 列的选项浮层也会有 DEFAULT。generated 列照抄，保存时报错，用 `ponytail:` 标出。
  - 复制高亮：表格 `yy` 闪这一行的可见格，`yl` 闪这一格；console 只在 `y` 操作符（含 `"+y`）时闪，删除不闪，按字符 / 行 / 块各闪各的范围。写剪贴板仍用 OSC 52。

**验收**
- [ ] e2e：表格 `yyp` 多出一行，除自增主键是 `<default>` 外和原行相同，`C-s` 后存进去；`yy` 时那一行闪一下黄色；console 里 `yy` 同样闪一下。

## F3.33 新行里用 Tab 切换字段 · 状态：todo

- **依赖**：F3.24、F3.32
- **涉及**：`internal/app`（单元格编辑）

**开发**
- [ ] 在还没保存的新行里编辑时，`Tab` / `S-Tab` 提交这一格，移到下一个 / 上一个字段并直接进入编辑，不用先退出再按 `↵`（用户：`o`、`yyp` 之后逐格填写）。新行里的选项浮层和时间浮层不再用 `Tab`：选项用 `↑` / `↓` / `C-n` / `C-p`，时间的选项行用 `C-n` / `C-p`，段用 `↑` / `↓` 加减、点击切段。已有的行照旧，`Tab` 仍在浮层里选择（F3.15、F3.34）。
- [ ] 最后一个字段上 `Tab`、第一个字段上 `S-Tab`：提交这一格并退出编辑，光标留在那一格，不绕回（免得覆盖已经填好的格）。字段只算可见列，转置与否都一样，视图跟着滚动。不合法的值照 §10.7 挡住，不能切走。
- [ ] 新作用域 `[keys.newrow]`（§6.4）：`<Tab>` = `cell.field.next`「下一个字段」、`<S-Tab>` = `cell.field.prev`「上一个字段」，只在编辑新行的格时生效，叠在 options / segments 之上；keymap.Context 的浮层能叠两层，没绑的键从下面那层来。

**验收**
- [ ] e2e：`o` 之后输入一格、`Tab`，直接在下一格编辑；`S-Tab` 回到上一格；`C-s` 后整行存进去。

## F3.34 时间浮层的 Tab 走到选项 · 状态：todo

- **依赖**：F3.15
- **涉及**：`internal/app`（segments）

**开发**
- [ ] 时间浮层里 `Tab` / `S-Tab` 依次走过各段，再走到选项行的每一项（◷ 现在、∅ NULL、DEFAULT、↺ 原值，有才有），到头绕回；停在选项上时 `↵` 应用它，`↑` / `↓` 不做事，段不再高亮（§10.2）；S-Tab 在第一段上到最后一个选项；用 `C-n` / `C-p` 选中选项也算停在选项上；输入文字时回到原来那一段，点击某一段就到那一段。

**验收**
- [ ] e2e：created_at 编辑时连按 `Tab` 走过六段后选中「◷ 现在」，`↵` 填入当前时间；有默认值的时间列能选中 DEFAULT。

## F3.35 悬停 tab 显示关闭按钮 · 状态：todo

- **依赖**：F1.6
- **涉及**：`internal/ui`（tab 栏）、`internal/app`（命中表）

**开发**
- [ ] 指针悬停在某个 tab 上时，名字后面的标记位置显示 `×`（图标 `close`），点击等于对这个 tab 执行关闭（同 `x`，有修改时照样确认）；点 tab 的其他部分照旧切过去。结果区的 tab 同样，「日志」tab 不能关、不画 `×`。右键菜单不做。
- [ ] `×` 画在标记的位置（当前 tab 替换 `*`，上一个 tab 替换 `-`），没有标记的 tab 用名字后面的空格，悬停时 tab 栏不移位；指针停在 `×` 上时 `select` 底；点 `×` 关的是那一个 tab，不一定是当前 tab，当前 tab 不变。

**验收**
- [ ] golden：悬停时的 `×`；e2e：点 `×` 关掉那个 tab，有修改时弹确认框。

## F3.36 表格的 `{N}G` 跳到第 N 行 · 状态：todo

- **依赖**：F1.3
- **涉及**：`internal/app`（grid）

**开发**
- [ ] 同 nvim：`{N}G`、`{N}gg` 跳到第 N 行，N 是行号列上显示的行号；不在当前页时翻到它所在的页；超出总行数时到最后一行。转置视图里对应第 N 个字段，不翻页；结果区的表格、日志同样。新行 `+` 不参与编号。目标是第 (N−1)/limit 页的第 (N−1)%limit 行；翻页照 PAGE 输入框的规则：总页数知道（精确或 `~` 估计）时夹到最后一页，不知道（计数中或 `?`）时直接去那一页，超出末尾就是空页；精确计数时 N 超过总行数到最后一行。不带次数的 `gg` / `G` 照旧是本页的第一行 / 最后一行。

**验收**
- [ ] e2e：`15G` 光标到第 15 行；LIMIT 10 时 `15G` 翻到第 2 页第 5 行。

## F3.37 打开表的目标 pane 与「同一 pane 里唯一」 · 状态：todo

- **依赖**：F3.18
- **涉及**：`internal/app`（openTable、openTarget、palette）

**开发**
- [ ] 目标 pane 改为最近聚焦过的普通 pane，console 所在的 pane 也算（不再跳过当前 tab 是 console 的 pane）；焦点在树上时同样取最近聚焦过的那个（§12「表」）。
- [ ] 同一个 pane 里一张表只开一个 tab：目标 pane 里已经有这张表就切过去（不重新取数），没有就在目标 pane 新开，别的 pane 里开着也不管（用户：用不同的 pane 对比同一张表）。`C-t`、树里的 `t`、`+` 的标记同样遵守；「选择 tab」列表不再需要，去掉（§7.8「打开已有的表」）。从引导 tab 打开的表在这个 pane 里已经开着时，切过去并关掉引导 tab（tester 提问后定，取代 F3.7 的「总是替换引导 tab」）。
- [ ] `C-t`、树里的 `t`、中键和 `↵` 打开表的效果相同，action 和键位保留，面板底栏只留 `↵ 打开`；目标 pane 永远不是结果区；「选择 tab」的面板模式和相关代码删掉；列节点切到已开的 tab 时光标照旧移到这一列。`+` 的标记从 F3.7 起已经没有了，§7.8 那段过时的已改。

**验收**
- [ ] e2e：焦点在 ② console 时从树打开 t_order，在 ② 新开 tab；① 已开着 t_order 时从 ② 再打开，② 也开一个；在 ① 里再打开 t_order 只切过去，① 里不会有两个。

## F3.38 console 的系统剪贴板寄存器 · 状态：todo

- **依赖**：F3.2
- **涉及**：`internal/editor`（寄存器）、`internal/app`（剪贴板）

**开发**
- [ ] 编辑器支持 `"+` 和 `"*`（等同）寄存器，读写系统剪贴板：`"+y{移动}`、`"+yy`、VISUAL 下 `"+y`、`"+p` / `"+P`、`"+d`、`"+x`，照 vim 的规则。其余具名寄存器照旧不做（§11）。nvim 差分测试里不测这两个寄存器。
- [ ] 无名寄存器不再写系统剪贴板（同 vim 默认的 `clipboard=`，否则 `Y` 和 `yy` 没区别）；表格的 `yy` / `yl` 照旧写。`"+p` 用 OSC 52 查询读取（tea.ReadClipboard），终端不回应就什么都不做，没做 pbpaste / xclip 后备，用 `ponytail:` 标出；tmux 是否回应开工时实测。
- [ ] 通过后决策者在用户的 config.toml 里加 `[map.console.normal] Y = '"+yy'` 和 `[map.console.visual] Y = '"+y'`（用户要求用 `Y` 复制到系统剪贴板）。

**验收**
- [ ] 单测：`"+yy` 之后剪贴板里是这一行，`"+p` 粘出剪贴板的内容。
- [ ] e2e：VISUAL 选中后 `"+y`，系统剪贴板里是选中的文字（OSC 52 时查 tmux 的 buffer）。

## F3.39 WHERE 输入框的 vim 模式 · 状态：todo

用户 2026-09-30 提出：WHERE 输入框和 console 一样用 vim 模式，好在 NORMAL 下用 `dd`、`ciw` 这类操作改条件。

- **依赖**：F3.1–F3.3、F3.13、F3.14、F3.31
- **涉及**：`internal/app`（WHERE 输入）、`internal/editor`（单行用法）、`internal/keymap/default.toml`

**开发**
- [ ] WHERE 输入框改用 console 的 vim 编辑器，只有一行（§7.8「WHERE」）：
  - 从表格按 `/` 或点击进入时是 INSERT，光标在末尾（同现在）；INSERT 下补全、自动配对、`C-w` / `C-u` / `M-BS`、`C-r` 历史照旧，`↵` 执行。
  - INSERT 下 `esc` 回到 WHERE 的 NORMAL（补全列表开着时一次 `esc` 关列表并回 NORMAL，同 console）；NORMAL 下再 `esc` 回表格，没 `↵` 过的改动丢掉，输入框恢复成生效的条件（同现在）。
  - NORMAL 下编辑器支持的移动和操作都能用：`i a I A`、`x s S C D`、`cc` / `dd`（清空这一行）、`ciw caw diw daw ci' ci(` 等文本对象、`u` / `C-r`、`p` / `P`、`yy`；换行类的（`o` `O` `J`）不做事，`j` / `k` 不动，`↵` 执行。
  - NORMAL 下 `/` 打开历史 / 收藏下拉（这里不需要搜索）。
  - 模式块照 console 显示 NORMAL / INSERT。
  - `C-c` 照旧等同 `esc`（它也是取消查询、连按退出的键），清空条件用 NORMAL 下的 `dd` / `cc`，或 INSERT 下的 `C-u`。
- [ ] WHERE 的 NORMAL 用哪个作用域名、撤销历史按什么范围记，开工时由 worker 提议。F6.6 关掉 vim 模式后，WHERE 回到现在的普通输入框。

**验收**
- [ ] e2e：`/` 进 WHERE 输入、`esc` 到 NORMAL、`dd` 清空、`i` 输入新条件、`↵` 执行；NORMAL 下 `ciw` 改一个词；NORMAL 下 `/` 打开历史下拉；NORMAL 下 `esc` 回表格，没执行的改动丢掉。

