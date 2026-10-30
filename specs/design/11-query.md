# sqlmux 技术方案 · §11 Query 与 result

> 这是技术方案的一章，各章的索引和阅读规则在 [`../tech-design.md`](../tech-design.md)；章节编号（§N.M）全仓库通用，不随文件改。

## 11. Query 与 result

**编辑器**（`internal/editor`）：覆盖 nvim 的基础编辑操作，行为以 nvim 为准。不做宏和 `.` 重复。预计 2000–3000 行加测试，其中块选择约 300–500 行；这是整个项目里最大的一块自研代码。

**支持的操作**（都支持次数前缀）：

| 类别 | 内容 |
|---|---|
| 模式 | NORMAL、INSERT、REPLACE（`R`）、VISUAL（`v`）、VISUAL LINE（`V`）、VISUAL BLOCK（`C-v`） |
| 移动 | `h j k l`、`w b e ge`、`W B E gE`、`0 ^ $`、`gg G {n}G`、`f F t T ; ,`、`%`、`{ }`、`H M L`、`C-d C-u C-f C-b`、`zz zt zb`、`* #` |
| 搜索 | `/ ?` 回车，`n N` 跳转。正则用 Go 的 RE2 语法（接近 vim 的 `\v`），smartcase。**高亮与计数**（M15 F15.9，用户验收：query 和 JSON 视图里搜索都没有匹配高亮，要和 nvim 一样）：回车后所有匹配用 `search` 底、当前匹配用 `search_current` 底（§7.7；nvim 的 `Search` / `CurSearch`，hlsearch），`n` / `N` 跟着换当前匹配；高亮一直留着，NORMAL 下 `esc` 清掉（同 LazyVim 把 `esc` 映成 `:noh`，nvim 本身要 `:noh`），下一次搜索或 `n` / `N` 再亮；值编辑器和 JSON 视图里 NORMAL 的 `esc` 是关闭 / 回表格，高亮亮着时第一下 `esc` 只清高亮、再按才关，同 §6.9 `vim = false` 有选区时的两下 `esc`，`q` 照旧直接关（审查时定）。回车和 `n` / `N` 之后命令行那一行留着显示模式，右端是匹配序号 `[1/2]`，绕回去了前面加 `W`（nvim `shortmess` 没有 `S` 时的 search count，search.c `update_search_stat`，上限 99 的写法照它），下一个不是 `n` / `N` 的键按下时这一行消失；这一行盖住文字区的最后一行，所以带计数时编辑器按少一行滚动，光标不会被盖住。值编辑器和 JSON 视图是同一个编辑器，一样 |
| snippet 与 Select 模式 | M15 F15.8（用户验收：结构模板的占位符要像 nvim 的 snippet：选中着就能直接打字替换，`Tab` / `S-Tab` 跳上下一个占位符）。照 nvim 的 `vim.snippet`（runtime/lua/vim/snippet.lua）做最小的一份：插入带 tabstop 的文字时第一个 tabstop 在 **Select 模式**下选中（模式块显示 SELECT；`:help Select-mode`，按 nvim 0.12.4 实测：可打印字符、粘贴、`↵` 替换选区并进 INSERT；Backspace / Del / `C-h` 删掉选区、回 NORMAL（不进 INSERT），删掉的文字进寄存器；`esc` / `C-c` 回 NORMAL；方向键移动选区的另一端（`←` 缩短、`→` 加长）；不做 `gh` / `C-o` 这些进出 Select 的键），当前这个以外所有有宽度的 tabstop 用 `visual` 底标出（填过的也标，nvim 的 `SnippetTabstop` 默认就链到 `Visual`，行为相同；空的 tabstop 如 `VALUES ()` 的括号里没宽度、不标）；snippet 活着时 INSERT 和 SELECT 下 `Tab` 跳到下一个、`S-Tab` 回上一个（跳到的那个进 Select 模式；补全列表开着时 `Tab` 先归补全）；最后一个上按 `Tab` 光标跳到 snippet 文字的末尾（`vim.snippet` 的 `$0`）、进 INSERT、snippet 结束，不插 Tab，第一个上 `S-Tab` 无事；INSERT / SELECT 下光标落到任何一个 tabstop 之外（`o` 开新行、`A` 到行尾之后打字都算）、或文字被整个替换时 snippet 结束，NORMAL 下移动光标不结束，`esc` 不结束（审查时照 nvim 0.12.4 的 snippet.lua `setup_autocmds`（CursorMovedI）改：原来写的「移出模板文字的范围」在整个 query 就是模板时永远出不去，`o` 开新行后 `Tab` 会跳回 tabstop）。tabstop 的来源：结构模板的 `<name>` 这些占位符（§7.8）、F15.3 的 `(全部列) VALUES ()` 骨架；补全里别的项不带 tabstop。`vim = false` 时同样选中、打字替换、`Tab` 跳 |
| 进入 INSERT | `i a I A o O`、`s S` |
| 操作符 | `d c y`、`> <`、`gu gU g~`（大小写）、`gc`（切换 `--` 注释，同 nvim 0.10 起内置的行为）。操作符可以接任意移动或文本对象，例如 `dw`、`c$`、`y2j`、`gUiw` |
| 简写 | `dd cc yy`、`D C Y`（`Y` 等同 `y$`，与 nvim 默认一致）、`x X`、`r{字符}`、`~`、`J`、`p P`、`>> <<`、`gcc` |
| 文本对象 | `iw aw`、`iW aW`、`i( a(`（`ib ab`）、`i[ a[`、`i{ a{`（`iB aB`）、`i' a'`、`i" a"`、`` i` a` ``、`ip ap` |
| VISUAL | 可以用移动和文本对象扩展选区（如 `viw`、`vi(`）；`o` 切换选区的两端，`gv` 重新选中上次的选区；选区上可用 `d c y x > < u U ~ J gc` |
| VISUAL BLOCK | 按显示列选出一个矩形；`$` 把选区延伸到每行行尾；`o` / `O` 切换对角。可用的操作：`I` / `A` 在每行的块首 / 块尾插入（输入先显示在第一行，按 esc 后复制到其余各行；`A` 会给较短的行补空格）；`c` 修改整块；`d` / `x` 删除整块；`y` 复制整块，寄存器记为块类型；`p` / `P` 按块粘贴；`r{字符}`；`~ u U`；`> <`；`gc` |
| 撤销 | `u` 撤销、`C-r` 重做、`U` 撤销当前行上的全部修改，与 nvim 一致。想把 `U` 当成重做（Helix 的习惯），映射一行 `U = "<C-r>"` 即可 |
| 命令行 | `:{n}` 跳到第 n 行；`:s` 与 `:%s` 替换，支持选区范围 `'<,'>` 和标志 `g i`，替换串中的 `\1`、`&` 按 vim 的写法；`:w`、`:q`。`/`、`?`、`:` 的输入行画在 query 内容区的最后一行（tab 栏上方），模式显示为 COMMAND；VISUAL 下按 `:` 预填 `'<,'>`。`{n}` 和 `s` 由编辑器执行，其余交给 app 按面板的 ex 别名（q、qa、w）执行，都不认识时 toast「不支持的命令：xxx」。搜索找不到时 toast「找不到：<pat>」（对应 nvim 的 E486），正则编译失败时 toast「正则有误：<err>」，错误里不带内部加的 `(?i)` 前缀；没有上一个模式时（`n`、`/<CR>`、`:s//x/`）toast「没有上一个模式」（E35）；从没进过 VISUAL 就用 `'<,'>` 时 toast「没有选区」（E20）；`:s` 的范围超出文本（行号 < 1、大于最后一行，或 `'<` / `'>` 指向的行已被删掉）时 toast「范围无效」（E16），一行都不改，不夹到文本以内。只有行号的 `:{n}` 超过最后一行时夹到最后一行、不报错，是负数时报「范围无效」、光标不动。细节照 nvim（M3 审查时定）：`/x/` 里没转义的 `/` 结束模式，后面的 search offset 忽略、不支持（SQL 里的除号 `/a/b` 也按此处理）；`3:` 这类带次数的冒号换成 `:.,.+2`；替换文字支持 `&`、`\1`、`\t`（Tab）、`\r`（断行），单独的 `~`（上一次的替换文字）不支持 |
| INSERT 下 | Backspace、`C-w`、`C-u`（照 nvim 的默认映射 `<C-G>u<C-W>` / `<C-G>u<C-U>`，先断开撤销步）、方向键；`↵` 换行并保持上一行的缩进（上一行以 `;` 结尾时缩进为 0，`o` / `O` 同；M8 F8.2）；Tab 按 `tab_width` 插入空格。模糊补全见 §9.7：输入时自动弹出，也可以用 `C-n` 手动唤起（`C-p` 在列表没开时是全局的命令面板） |
| 寄存器 | 无名寄存器（带字符 / 行 / 块类型）只留在程序里；F3.38 起只有 `"+` / `"*` 读写系统剪贴板（同 vim 默认的 `clipboard=`，原来每次 yank、删除都写系统剪贴板）：读写的思路同 nvim 的 clipboard provider，先本地工具、再 OSC 52（用户问「为什么 nvim 里可以复制」后定；顺序不完全同 nvim，两个工具都装了才有差别）：依次找 `pbcopy` / `pbpaste`（macOS）、`wl-copy` / `wl-paste`、`xclip`、`xsel`，找到就直接调它，不经过终端，所以 tmux 的 `set-clipboard` 设成什么都不影响；都没有时（比如 SSH 到没有图形界面的机器）才用 OSC 52 写、OSC 52 查询读，终端不回应就什么都不做。工具找到了但执行失败时也退回 OSC 52，不提示。`wl-*` 要 `WAYLAND_DISPLAY`、`xclip` / `xsel` 要 `DISPLAY` 有值；参数照 nvim，但去掉 `-quiet` / `--nodetach`（一次性调用，工具自己 fork 常驻进程拿着 selection）。程序里所有写读系统剪贴板的地方（表格 `yy` / `yl`、快速 SQL 的 `C-y`、`"+` 寄存器）都走这一处。`"+P`、VISUAL 下的 `"+y` / `"+d` / `"+x` 照 vim 的规则；VISUAL 下的 `"+p` 不做，因为 VISUAL 的 `p` 本身就不做（见下面「不做」）。其他具名寄存器（如 `"ayy`）整条命令不执行。表格没有寄存器，`yy` / `yl` 照旧直接写系统剪贴板 |

**寄存器**：除了无名寄存器，只做系统剪贴板 `"+` / `"*`（F3.38，用户要复制到系统剪贴板），读写方式和表格的复制相同；复制时被复制的范围闪一下 `yank` 色（F3.32）。

**不做**：宏（`q` `@`）、`.` 重复、具名寄存器（`"a`–`"z`）、标记与跳转列表、折叠、句子与标签类文本对象（`is` `as` `it` `at`）、`:g` 等其他 ex 命令；也不做 `gJ`、`{count}%`、VISUAL 下的 `p`、`i<` / `a<`、incsearch（hlsearch M15 F15.9 起做了），以及 nvim 0.10 起的默认映射 `v*` / `v#`（在 VISUAL 下搜索选中的文字）。不折行（nowrap），横向跟着光标滚动，`H M L`、`C-d` 等按可见行数计算，scrolloff 为 0。

**块选择的补充说明**：

- 块的边界落在 Tab 或宽字符中间时，按 nvim 的规则处理，由差分测试兜住（§15）。
- `C-v` 在 macOS 和 Linux 的终端里都能收到。Windows Terminal 默认把它当作粘贴，需要在终端设置里释放这个键，或者把块选择改绑到 `<C-q>`（与 Windows 版 vim 的做法相同）。

**实现结构**：按 vim 的语法组织，即「[次数] 操作符 [次数] 移动或文本对象」，由一个小状态机解析等待中的按键。

- 移动和文本对象都是「位置 → 范围」的函数，范围分为按字符（含端点或不含端点）和按行两种。
- 操作符作用在范围上。
- 因此每新增一个移动或文本对象，都能自动和所有操作符组合，不用逐个组合去写。

**其他要求**：

- **撤销的粒度**：一条 NORMAL 命令，或一次完整的 INSERT，算一个撤销步骤。实现上保存整份快照；缓冲区是 `[]string`，几 MB 以上的文件会变慢，SQL 文件一般遇不到。
- **粘贴**：支持 bracketed paste，粘贴内容一律作为文本插入。否则在 NORMAL 下粘贴，会把文本当作命令执行，可能误删内容。位置照 nvim 的 `vim.paste`：NORMAL 下贴在光标后面（同 `p`），INSERT 下插在光标处，VISUAL 下替换选区。
- **键位映射**：按 §6.6 配置。`[map.query.normal]` 和 `[map.query.visual]` 只对 query 生效，例如 `L = "5l"`。用户映射会覆盖编辑器内置的同名 vim 键（如 `J` 合并行），与 nvim 的 `nnoremap` 行为一致。

**完整 vim 的出口**：命令面板中的「Edit in $EDITOR」命令，通过 `tea.ExecProcess` 暂停 TUI，用外部编辑器打开当前文件，退出后重新载入。默认不绑定键位。

**文件**：

- 每个 query tab 对应 `$XDG_DATA_HOME/sqlmux/queries/<名字>.sql`，默认名 `query_<n>`（M5 F5.11 起不再按连接名分目录；M8 F8.11 起可以用 `query.rename` 改名，改名就是改文件名，名字规则同连接名，目标已存在时拒绝；state.json 的 `queries` 和布局文件都按名字记，布局文件的 `n` 改成 `name`，旧的 `n` 读成 `query_<n>`）。新建时 n 取**没有文件、也没在任何 session 里打开**的最小值（M8 F8.21，用户 2026-10-08：关掉 query_3 再新建又是 query_3、带着旧内容，像 DataGrip 那样应该是 query_4，关掉的文件留在列表里随时能再开；原来只看「没被打开」，所以关掉就被复用；两个 session 不能打开同一个文件，自动保存会互相覆盖）；关 tab 不删文件，在树的「Queries」节点里删掉文件后这个编号才会被新建的用上。启动时的 query_1 文件已经存在就载入，所以每次启动都带着上次写的 SQL。state.json 记着每个文件最后用的连接和 schema（§14），只在启动时恢复默认布局里的 query_1（以后 M6 恢复布局）时用；新建的 query 一律用当前连接（§8.6「默认值」）；记下的连接已经不在 connections.toml 里时用当前连接。旧版本 `queries/<连接名>/` 下的文件不写迁移代码。
- **签名提示与值补全**（M15 F15.3、F15.10、F15.11，用户 2026-10-11：INSERT 的 VALUES 里不知道第几个值对应哪个字段，DataGrip 有字段名 hint）：`sqlkit.ValueContext(sql, offset)` 识别光标所在的「列上下文」（表、列清单、第几个、种类 insert / update / where / call；括号深度、字符串和注释用 `CompletionContext` 同一套扫描器：`INSERT … VALUES (…)` 按语句里的列清单，没写列清单按 catalog 的列顺序，第几个值按括号深度 1 的逗号数，多行 VALUES 每个括号重新计数，`INSERT … SELECT` 不提示；`UPDATE … SET col = `；`WHERE` / `ON` / `HAVING` 里 `col = ` / `IN (` 的右边；用户定义的函数的调用括号里，签名来自 F14.3 Functions 节点的同一份 catalog，内置函数没有）。**签名提示**（F15.3）：光标所在行的上一行（第一行时是下一行）画一行浮层，列出字段和类型，当前字段高亮、两边 `dim`，放不下只显示当前字段前后几个、两头 `…`；光标移动跟着换，出了上下文就消失，不用唤起也不用关；`[query] hints = false` 关掉；不做行内虚拟文本。**值补全**（F15.10）：在这些位置弹补全：枚举列列出枚举值（带引号）、布尔列 `true` / `false`、可空列 `NULL`、有默认值的列 `DEFAULT`（这两项只在 INSERT / UPDATE 里：条件里 `col = null`、`in (null)` 永远不成立，补全不教人写它，F15.10 审查时定）、时间列 `now()` / `CURRENT_TIMESTAMP`，候选和单元格编辑的选项浮层用同一份 catalog；其余列不弹。**同类小项**（F15.11）：`INSERT INTO t ` 后的补全给 `(全部列) VALUES ()` 骨架，VALUES 的括号里一个 tabstop（§11「snippet」），列清单是普通文字；`UPDATE t SET` 后的列补全只列 UPDATE 和 SET 之间第 0 层的表（MySQL 的多表 `update t1 join t2 on … set` 两张都列，排法同 F8.4 的多表；PG 的 FROM 在 SET 后面，不算）；补全插入的标识符需要引号时加引号（规则在 §9.7「接受」，各方言一份保留字表）。候选但没定：`SELECT` 列表里 `*` 展开成全部列名；`K` 看光标下标识符的类型和注释。
- **Redis 控制台**（M16 F16.4）：Redis 连接上的 query tab 就是控制台，文件、编辑器、结果区、翻页、历史（`history.jsonl` 的 `sql` 字段记那一行）、`C-r` 历史下拉都照用；标题的 `DB` 段是 `db0 ▾`，下拉列 `INFO keyspace` 里的 db（同 PG 的库段），选了就换到那个 db 的子连接（§5），不发 `SELECT`。SQL 管线换成 `redisLang`（§5「语言缝」）。词法、执行范围、高亮、补全在 §9.8。**回复摆成表**（照 `Result`，结果区的表格不变）：标量一格一列 `value`（整数、状态回复 `OK`、bulk 都是）；数组一列 `value`，一行一个；RESP3 的 map 是 `field` / `value` 两列；等长的二维数组按列摆（`ZRANGE … WITHSCORES` 在 RESP2 下是平的数组，按 DOCS 的 `reply_schema` 认不出就两两一行 `member` / `score`，认不出的平数组照一列摆）；更深的嵌套在格子里是 JSON 文本并高亮；nil 是 NULL；`Tag` 是命令名加回复类型（`HGETALL · 12 fields`）；错误进错误栏（`-ERR …` 原文）。整份回复在内存里，按现有 500 行翻页、「全部」不重跑（没有游标可以续，`maxRows` 截断并标 `Truncated`）。RESP 协议版本：go-redis 默认 RESP3（`Protocol: 3`），map 和 set 由它解出来。
- **查询历史**（M14 F14.2）：query 里跑的每条语句（成功、失败、取消都记；表格取数、快速 SQL、整次没跑的（如 `set search_path` 失败）不记）追加到 `$XDG_DATA_HOME/sqlmux/history.jsonl`，一行一条 JSON：`{"at","conn","schema"?,"sql","truncated"?,"tag"?,"took_ms","error"?}`，`tag` 是引擎的命令标签（`SELECT 3`、`UPDATE 1`），`error` 是消息第一行，取消是 `"canceled"`；`O_APPEND` 一次 write、不加锁，语句超过 64 KB 截断并记 `truncated`。`[query] history_max`（默认 1000，必须大于 0）启动时截掉最旧的（临时文件加 rename）。INSERT 下 `C-r`（`query.history`「查询历史…」，作用域 `queryinsert`，§6.4）或命令面板打开：现成的下拉框加一种 kind，只列当前连接的，语句一行化后取前 300 字给 fzf 匹配（`ponytail:` 标出），注释是 `月-日 时:分 · SELECT 3 · 12ms` 或 `失败: <错误第一行>`，`↵` 把原文（带换行）粘贴到光标处。
- 最后一次修改后 1s 自动保存（去抖），写临时文件再 rename，目录 0700、文件 0600（§13）；`:w`、`C-s`、关闭 tab、退出时立即写；没改过就不写。写入失败用 toast 提示。**文件在别处改过**（M13 F13.2，两个 sqlmux 开着同一个文件时后写的整份覆盖前一个；同 vim 的 W11）：读和写时记下文件的 mtime 和大小，写之前比对，不同就拒写、toast「文件在别处改过，没有保存；再保存一次会覆盖它」，这次操作中止（`:w`、`C-s`、关 tab / window / session、`:qa` 和两次 `C-c`、打开布局、外部编辑器、重命名都是），同一操作再来一次就覆盖；自动保存永不覆盖，只提示一次，之后静默不写，到退出时再拦一次。不做重读磁盘版本的出口（vim 的 `:e!`、DataGrip 的 File Cache Conflict），用户要文件里的就关掉这个 tab 再开。文件存在但读不出来时（是目录、权限为 000 等），toast「读取失败：<err>」，这个 query tab 不打开（pane 显示引导页），程序照常启动；不能用空内容打开，否则自动保存会把那个读不出的文件覆盖掉。`:q` 关闭 tab 不弹确认，内容已经写盘。`:wq` 是先 `:w` 再 `:q`：query 上等同 `:q`；表 tab 上先保存修改（§10.3），保存成功后才关闭，失败时 tab 留着、查询条显示原因，不弹丢弃确认。query 文件写不进去时，`:q`、关 pane、退出都不执行，toast「保存失败：<err>」。

**执行（C-02）**：

- 在 NORMAL 或 VISUAL 下按 `↵`、点击 gutter 的 ▶、点击标题栏的 `▶ run` 都可以执行。有选区时执行选区（块选区执行它覆盖到的那几整行，同 V-LINE），执行后退出 VISUAL、光标不动，同 vim 的操作符；要再跑一次用 `gv` 重新选中，或点结果区的重跑。否则执行光标所在的语句。点 gutter 的 ▶ 执行那一行开始的语句，不移动光标。
- 每次最多取 `query.max_rows` 行（默认 500，M10 F10.4 起；原来 1000），读语句用自动 LIMIT（§9.4），其余靠 `Exec` 的 `maxRows` 截断；截断时显示 `500+ 行`。
- **翻页**（M10 F10.4，照 DataGrip 的 `501-1,000 / 1,001+`）：只对被自动 LIMIT 的语句有效（顶层没写 LIMIT / FETCH 的读语句）。结果区聚焦时 `]` / `[`（`result.page.next` / `result.page.prev`，`[keys.result]`）在这个 tab 所属 query 的连接上重跑同一条语句，追加 `LIMIT max_rows+1 OFFSET 页号×max_rows`（走 §9.4 的追加，不包子查询），结果替换 tab 的内容；下一页只在截断时有效，上一页只在第 2 页起有效，其余按了无事；自己写了 LIMIT 的和写语句没有翻页。标题和日志的行数文字：第 1 页照旧 `500+ 行`，之后 `501–1000+ 行`（还有更多）/ `1001–1203 行`（最后一页）。`R` 重跑回第 1 页，固定的 tab 同样能翻。翻页只换这一个 tab：只跑这一条语句（结果 tab 记着语句序号、offset、是否被自动 LIMIT），同一次 run 的兄弟 tab 和固定的 tab 不动；取回期间保留旧内容、不弹占位、按键忽略，tab 名不变，完成后写一行日志（同执行语句）。顶层带 OFFSET 的语句不翻页（否则拼出两个 OFFSET）；同一次 run 里跟在 SET / RESET / DISCARD / USE 之后的语句也不翻页：翻页只重跑这一条，前面改过的 search_path、变量、超时都不在了，`set search_path to s2; select * from t` 翻页会读到另一张 t（保守起见不解析 SET 改的是什么，`set statement_timeout` 后面的也不翻）；快速 SQL 送到结果区的结果（`C-t`，不属于任何 query）不翻页。手动事务模式下翻页就是在事务里再执行一条，同执行语句。已知上限：没有 ORDER BY 时两页可能重叠或漏行，DataGrip 一样。
- 在所选连接的 `Main` 的一次 `Worker.Run` 里逐条执行，每条是单独的简单协议请求：自动事务模式下各自自动提交；手动模式下（F5.3）每条语句前 `Main` 不在事务里就先 BEGIN（照 psql 在 AUTOCOMMIT off 时的 SendQuery：一次执行里 commit 之后的语句也在新事务里），psql `command_no_begin` 列出的语句不补 BEGIN（事务控制、VACUUM、CREATE / DROP DATABASE、带 CONCURRENTLY 的、ALTER SYSTEM、DISCARD ALL 等，按 psql 源码定），出错后事务停在出错状态，由用户回滚。query 有执行在跑时不能关（关 tab、pane、window），toast「正在执行，结束后再关闭」，同换连接的规则：否则执行回来后连接停在事务里，上面却没有 query 了。遇到错误就停，后面的不执行。不一次发出整段：PG 简单协议里一次发送的多条语句属于同一个隐式事务，一条出错全部回滚，也没法知道错在哪一条、把哪个 ▶ 标红。
- 首个关键字是 create / alter / drop / rename（MySQL 的 `RENAME TABLE`，F5.4；PG 的改名是 `ALTER … RENAME`）的语句成功后，重新加载这个 query 所在连接的 catalog、清空列缓存（同 `tree.refresh`）。
- 用户在自动模式下自己 `begin`：`Main` 进了事务，这个连接上表格的保存也进这个事务、不自动提交（§10.3）；状态栏显示「事务中」（§7.8），提交、回滚同手动模式（M5 F5.3 起，原来是已知上限）。

**结果区（C-04 至 C-07，已确认）**

参考 DataGrip、DBeaver 的做法：所有结果集中显示在底部一个全宽的区域里，内部用 tab 区分。这样做有两个原因：

- 表格最需要的是宽度。PRD 原方案是在 query 旁边分割，结果只能占到屏幕的一半，向右分时甚至只有四分之一。
- 结果多了只是 tab 变多，pane 数量不变，布局不会越用越乱，这正是 C-05 想要解决的问题。

```
┌⟨0⟩ schema─┐┌⟨1⟩ data · t_order ─────────┐┌⟨2⟩ query · query_1 ─── ▶ run ↵┐
│ t_order   ││ WHERE deleted_at is null    ││ select * from mt_task              │
│ t_user    ││ …                           ││ where status = 'running';          │
│ …         │└─────────────────────────────┘└────────────────────────────────────┘
│           │┌⟨3⟩ result · query_1 #42 ─── 3 rows · 8ms  [重跑][转置][固定][导出][×]┐
│           ││ id    biz_type   status    created_at                                 │
│           ││ 689   goal       running   09-21 10:02                                │
│           ││ …                                                                     │
│           ││ 1:日志  2:query_1 #42*  3:query_1 #40（已固定）                    │
└───────────┘└───────────────────────────────────────────────────────────────────────┘
```

**出现与位置**：

- 某个 window 里第一次执行 SQL 时，在主区域（schema 树右侧）的底部新建一个全宽的 result pane，默认占 40% 的高度。
- 拖动边界或用 `SPC J` / `SPC K` 调整高度后，按 window 记住。
- 它是一个普通 pane，可以缩放（`SPC z`）、关闭（`SPC x`）；关闭后，下次执行时会重新出现。
- 每个 window 只有一个结果区，这个 window 里的所有 query 共用。

**tab 的组织**：

- **日志 tab**：第 1 个 tab 固定为「日志」，不能关闭。
  - 每执行一条语句就追加一行，内容是：时间、来源 query、语句的第一行、结果（行数、影响行数或错误）、耗时。
  - 执行出错时，自动切到日志 tab，显示完整的错误信息；同时，query 里出错语句的 ▶ 标记变成红色。
- **query 的结果 tab**：每个 query tab 在结果区里有自己的结果 tab，标题形如 `query_1 #42`。
  - 同一个 query 再次执行时，替换它原有的结果 tab，序号加一。C-05 的复用从 pane 级别改到了 tab 级别。
  - 一次执行多条语句时，每个结果集各占一个 tab（`#42`、`#42·2`……），下次执行时整组替换。
  - 非查询语句（如 `UPDATE 3`）只写进日志，不单独开 tab。一次执行完全没有结果集时，保留上一次的结果 tab，结果区切到日志。
- **执行中**：结果 tab 先显示占位内容（执行中、已用时、`C-c` 取消；M19 F19.2 起「执行中」后面跟转圈，§7.8），完成后再替换成结果。这一做法参考了 pgtui 的 `result_tabs`。
- **重跑**：用结果 tab 记下的连接和 schema 执行（M5 F5.11），来源 query 已经关掉或换了连接都照样；结果进它原来那一组；这时可能出现两组同名的结果（比如新开的 query_1 也有一组），靠序号区分。
- **出错标记**：同一个 query 每次执行开始时先清掉旧的红 ▶，这次有出错的再标上；缓冲区一有改动也清掉。
- **固定与关闭**：
  - `P` 固定当前结果 tab：它不会再被替换，下次执行另开新 tab（C-06）。固定的 tab 画 `pin` 图标，不写「（已固定）」（有图标就不配文字）。`R` 重跑（`result.rerun`）。
  - `q` 关闭当前结果 tab。
  - 快速 SQL 里按 `C-t`，结果会作为一个已固定的 tab 放进结果区。
- **切换**：`gt` / `gT`，或者直接点击 tab（T-02）。
- **上限**（M5 F5.18，用户验收时要求）：日志 tab 之外最多 10 个结果 tab（160 列宽下大约放得下这么多）。新出一个 tab 超过上限时，关掉最久没更新过的那个（按最后一次执行或重跑完成、结果到达的时间，固定的也算），不问；正在执行的不关。一次执行多个结果集时，这一组本身超过上限的，只留最后 10 个。

**焦点**：执行之后，焦点留在 query，方便接着写；结果区只切换到对应的 tab。按 `C-j` 进入结果区。

**工具行**：

- pane 标题的右侧显示来源与序号、行数、耗时（C-07），以及可以点击的按钮：重跑、转置、固定、导出 CSV、关闭 tab，画成 ` <图标> `。放不下时依次舍去，保留的优先级：关闭 > 重跑 > 固定 > 转置 > 导出 > 统计文字。严格按优先级从低往高丢，低优先级的项不会因为更窄就留下来（与 §7.8 pane 标题的贪心摆放不同：ascii 下 `[T]` 比导出的 `>` 宽，贪心会先丢转置、留下导出）。
- 导出按钮和 `result.export`「导出…」打开的是 §7.6「导出」的下拉（M12 F12.4；原来直接写 CSV）：CSV / JSON × 当前页 / 全部。当前页写当前目录下的 `query_1-42.csv`，第 2 页起文件名加页号 `query_1-42-p2.csv`（M10 F10.4），第 1 页不加；「全部」把这条语句去掉自动 LIMIT 流式重跑写文件，自己写了 LIMIT 的和快速 SQL 的「全部」就是当前页。完成后 toast 路径和行数。不走剪贴板：OSC 52 能复制的大小有限，1000 行可能超过终端的上限。
- 表格的交互与 data pane 相同（G-01、G-05、G-06），但只读。

**不做 `result_split = query`**（M3 定）：每个 query 旁边各有一个结果 pane，与「每个 window 一个结果区」是两套布局规则，工作量不小，也没人要过。有需要时再加。
