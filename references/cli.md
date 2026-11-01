# CLI 与 AI 调用契约

`ghship help [command]` 和 `<command> --help` 查看当前版本实际帮助。用户和项目子命令也支持 `--help`；项目命令另见 `ghship help project`。使用已安装版本的参数；不能因自然语言模板提到命令就猜测它存在。

## 结果与退出

0：本次指定范围完成；1：操作失败；2：参数/必需输入缺失；3：next无候选；4：用户安装完成但项目待配置，或需要外部生命周期入口。`gh`保留原生退出码。JSON stdout是机器结果，诊断stderr；不得从一条“完成”推断整条产品交付成功。

`init`：project/repo/ready/completed/missing/next。缺项必须根据next处理并再次实查。`project status`仅读本地安装事实，ready为null、readiness=not_checked；不能把installed解释成ready。`user list`逐账号显示status/checkedAt/expiresAt/projects，有效不等于全库权限。列表读取规范host目录与账号JSON，忽略并保留无关普通文件；host符号链接拒绝，损坏账号显示unavailable，不隐藏其他正常账号。缺过期日为null，403/网络错误为unknown。

## 用户级与项目级

- npm安装：`npx @ghship/cli@VERSION install [--user-only]`。skill先完成，再安装同版本持久CLI，默认进入init。
- npm更新/卸载：外部`npx @ghship/cli@VERSION update|uninstall`；普通已安装CLI返回外部入口，不在Windows删除自己。
- 直接下载：外部下载二进制`install|update|uninstall --standalone [--bin-dir PATH] [--dir PATH]`。
- 安装默认进入初始化，可同时传入下述init参数；`--user-only`仅完成用户级安装。默认skill目录为`~/.agents/skills/ghship`，自定义目录需在后续更新/卸载时继续传入同一`--dir`。直接下载默认CLI目录为Unix `~/.local/bin`、Windows `%LOCALAPPDATA%/Programs/ghship`，可用`--bin-dir`指定，不自动修改PATH。协调器使用`--skill-only`仅维护内嵌skill，`--check`只检查所有权；两者不代表项目已就绪。
- `user add|update|delete|list`：`--host`、`--user`、`--json`；add/update用`--token TOKEN`、`--token-file PATH`或`--token-stdin`（三选一），AI必须`--non-interactive`。delete有引用则拒绝。
- `init`/`project install`：`--project`、`--repo HOST/OWNER/REPO`、`--author`、`--reviewer`、`--admin`、`--configure`、`--confirm-auto-close`、`--acceptance-pr NUMBER`、`--remove-binding`、`--non-interactive`、`--json`。confirm记录UI确认，acceptance-pr必须是实际已合且独立批准并通过CI的接入PR；remove-binding仅移除本地绑定。自动邀请/接受只限目标仓库；configure不授权任意ruleset修改。
- `project status|update|uninstall`：`--project`、`--json`；uninstall可`--binding-only`清理旧路径绑定，不删除项目内容。
- `project skill --show|--create`：`--project`、`--mode supplement|override`、`--overrides testing,acceptance`、`--json`；用户内容始终保留。
- `doctor --scope user|project --json`：实际诊断，缺工具给官方安装链接，不自动sudo安装。

GitHub下载渠道自带skill，不需Node/Go。npm包包含Go二进制，用户不需编译器。开发测试用`npm exec --package /absolute/package.tgz -- ghship install --package-source /absolute/package.tgz --user-only`；这是本地验收入口，不能将尚未发布的版本说成registry已可用。

`--token` 为 token 文本；`--token-file` 与 `--token-stdin` 共用 JSON 输入，格式固定为：

```json
{"token":"github_pat_REPLACE_WITH_YOUR_TOKEN"}
```

用户名和host由CLI指定，host默认github.com。AI从仓库外JSON文件调用：

```sh
ghship user add --user acceptor --token-file /secure/acceptor.json --non-interactive --json
```

或将同一文件重定向给 `--token-stdin`；两种来源不可同时指定。JSON只允许一个字符串字段token，拒绝缺字段、未知/重复字段、非字符串、尾随内容和错误格式。文件/stdin原始输入最多16 KiB；所有来源提取的token去首尾空白，拒绝空值和内部空白/装饰格式。不得猜测纯文本文件或其他JSON结构。原输入文件保留。

账号默认保存：Unix `~/.config/ghship/accounts/<host>/<login>.json`，Windows `%APPDATA%\ghship\accounts\<host>\<login>.json`。所有来源共用身份验证、原子存储和失败保留机制，项目只引用账号。导入JSON只含用户提供的token；CLI管理的账号JSON含身份及系统凭据数据，不是导入文件。`--token`可能留在shell历史及进程参数中，AI优先文件/stdin；错误与输出不得回显token。Unix自动收紧当前用户拥有且其他用户不可写的配置目录到0700，拒绝符号链接、他人所有或他人可写目录；不修改整个.config或用户输入文件。权限保护并非加密，Windows使用当前用户DPAPI。

## 原生流程辅助

`gh author|reviewer|dispatcher -- GH_ARGUMENTS...`核验当前项目绑定身份后透传。各角色仅由对应执行者使用，换token不等于独立验收；用户名和token不默认指向ghship自身bot。项目配置和秘密均在仓库外。

角色命令必须在目标Git仓库内运行，即使显式设置身份及外部凭据覆盖。受控集成可用`GHSHIP_AUTHOR`/`GHSHIP_REVIEWER`与`GHSHIP_AUTHOR_PAT_FILE`/`GHSHIP_REVIEWER_PAT_FILE`；这些环境路径读取受保护的纯文本凭据，须位于仓库外，与`user --token-file`的导入JSON用途不同。`GHSHIP_DISPATCHER`约束本地gh登录身份；覆盖不会跳过在线身份验证。

平台调用失败：瞬时故障在原命令上退避重试（有限次数、固定间隔），不更换身份、凭据通道或端点；写请求结果不明先回读确认未生效再重试，禁止盲目重建；有限重试仍失败或权限被拒绝就停止并如实报告。故障判定与完整纪律见 [workflow.md](workflow.md)。

`next --milestone NUMBER --json`读取当前项目origin及绑定作者，自动逐页读取本轮Open Issue与依赖，直到原生hasNextPage=false，不设任务数量上限。全部查询成功后才选择，分页异常、错误或总量变化即失败，不输出部分候选。唯一已指派叶片resume，否则最小编号可领取叶片claim，零写入；父任务不领取。分页不是事务快照，领取前回读具体任务及验收标准、依赖并确认唯一执行者；已指派但阻塞则保全并停放。

`next --author LOGIN --json` 的stdin数组模式，必需number/assignees/blockedBy/subIssuesSummary；达到1000条时保守拒绝可能截断的输入，改用自动分页模式，不提供可调上限掩盖截断。原生父任务范围仍由agent递归核实；直接分页入口使用明确的里程碑编号，不能自动跨轮领取。

生产列表复用 `gh api --paginate`，Go不另行维护Link/游标调度或通用HTTP重试。每次原生分页调用默认10分钟预算，可用 `GHSHIP_QUERY_TIMEOUT=20m` 调整；Ctrl-C取消，失败不返回部分候选。里程碑JSON附query.milestone和complete=true，仅说明该查询完成，不代表验收或交付成功。原生gh透传仍遵循命令本身的数量限制：需要完整列表时明确使用原生API分页，不能用默认30条列表推断不存在。

`tidy PR --json`仅本地维护：核实同仓库PR已合到实际默认分支，clean Git，fetch/快进与SHA核对，匹配PR head才删本地分支。已有独有成果、脏树或分歧保留，不关Issue、不删远端、不重跑验收。

项目skill格式与真实生效来源通过project skill --show核验；CLI只验证结构，无法证明自然语言无冲突。未查询事实明确unknown，示例占位符不得照抄成实际证据。

认证Git操作 `ghship git ROLE -- ...` 共用角色验证并使用子进程内gh凭据，不改全局helper；普通本地Git直接执行。
