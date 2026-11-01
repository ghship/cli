# GHShip

面向人和 AI 的轻量 GitHub 开发交付工具：从开发文档拆分 Issue，使用原生里程碑、父子关系和依赖串行领取任务，开发、测试、独立验收、合并，再验证产品交付。Go 单入口；Issue 是任务推进的统一入口，PR、Review、CI 和产物记录各自事实。

**安装渠道：** [GitHub Release](https://github.com/ghship/cli/releases/tag/v1.0.0) 和 [npm registry](https://www.npmjs.com/package/@ghship/cli) 均已提供 1.0.0。

## 安装与首次接入

npm 渠道需要 [Node.js/npm](https://nodejs.org/en/download)。项目接入需要 [Git](https://git-scm.com/downloads) 和支持原生 Issue 父子/依赖的 [gh](https://cli.github.com/)。

```sh
npx @ghship/cli@latest install
```

安装分两个环节：先安装一份 `~/.agents/skills/ghship` 与配套 CLI，再自动进入当前项目初始化。非项目目录或缺少必需配置时保留用户安装，明确报告项目未就绪。先只安装用户工具：

```sh
npx @ghship/cli@latest install --user-only
ghship doctor
cd /path/to/project
ghship init
```

工具不静默安装系统软件、不调用 sudo。直接从 GitHub Release 下载对应平台的二进制后也可安装，无需 Node/npm：

```sh
./download/ghship install --standalone --user-only
ghship init --project /path/to/project
```

CLI目录默认 Unix `~/.local/bin`、Windows `%LOCALAPPDATA%/Programs/ghship`，按提示加入 PATH。Windows 更新卸载从另一个下载目录的 exe 运行。

## 账号与初始化

开发与验收使用不同 GitHub 用户；管理员使用本机 gh 登录。token 保存在用户级账号目录，不放进项目。Unix 仓库外文件受权限保护，Windows 存储采用当前用户 DPAPI；同一个 OS 用户内不构成模型隔离。

```sh
ghship user add --user developer --token 'TOKEN' --non-interactive
ghship user add --user acceptor --token-file /secure/acceptor.json --non-interactive
ghship user list --json
ghship init --project /work/product --repo github.com/team/product \
  --author developer --reviewer acceptor --admin owner --configure --non-interactive --json
```

账号输入支持 `--token TOKEN`、`--token-file PATH`、`--token-stdin`，三选一；不指定时终端隐藏输入，非交互模式报缺项。`--token` 输入单个 token 文本；文件和 stdin 共用下列 JSON 格式，只允许一个字符串字段 `token`：

```json
{
  "token": "github_pat_REPLACE_WITH_YOUR_TOKEN"
}
```

将 JSON 保存为仓库外的 `/secure/acceptor.json`，Unix 权限设为 `0600`。文件和 stdin 调用二选一：

```sh
ghship user add --user acceptor --token-file /secure/acceptor.json --non-interactive
ghship user add --user acceptor --token-stdin --non-interactive < /secure/acceptor.json
```

JSON 拒绝未知/重复字段、非字符串值、缺字段、尾随内容及格式错误。整个文件或 stdin 输入最大 16 KiB；token 去首尾空白，拒绝空内容、内部空白和装饰格式。输入文件不会自动删除。文本参数可能保留在 shell 历史或进程参数中，自动调用优先用文件或 stdin。`--user` 是 token 对应的真实 GitHub 用户名，`--host` 默认 `github.com`；身份由 GitHub 验证。

所有来源验证后保存到同一用户级账号 JSON：macOS/Linux `~/.config/ghship/accounts/<host>/<username>.json`，Windows `%APPDATA%\ghship\accounts\<host>\<username>.json`。默认 host 为 `github.com`。Unix 自动将当前用户拥有且其他用户不可写的配置目录收紧到 `0700`，账号文件 `0600`（权限保护，不是加密），Windows 使用当前用户 DPAPI；项目只引用身份，不复制 token。更新失败保留原账号。导入 JSON 是用户提供的 token 输入；账号 JSON 由 CLI 管理，包含身份与系统凭据保护数据，不作为跨平台导入文件。

`--configure` 明确授权目标仓库的协作者邀请、对应账号接受邀请及自动删除分支设置；已有更高权限保留。账号有效不代表所有项目权限，list 的401标为invalid，403/网络问题为unknown；平台返回过期日期才显示，否则未知。

项目需要真实 CI、独立评审与默认分支保护。缺少治理时由 AI 根据项目技术栈形成接入 PR，真实测试和独立评审后合并，再配置对应规则并复核。工具不生成占位绿灯 CI，不撤销保护。自动关关联 Issue 的 UI 开关需确认一次：

```sh
ghship init --confirm-auto-close --acceptance-pr 123 --non-interactive --json
```

这里只能填写实际已合并、最新提交独立批准且 CI 成功的接入 PR 编号；示例123是占位符。

## 项目复用与定制

每项目安装自己的引用入口和说明，共用 home skill；账号保存在用户级，项目绑定各自角色，不含 token。

```sh
ghship project install --project /work/second-project
ghship project status --project /work/second-project --json
ghship project skill --create
ghship project skill --show --json
```

项目可编辑 `.ghship/skill.md` 补充文档路径、测试、验收和交付要求。默认 `mode: supplement`；明确复写可选择范围：

```sh
ghship project skill --create --mode override --overrides testing,acceptance
```

模板已有则拒绝覆盖。可复写范围为 planning/development/testing/acceptance/delivery。当前用户指令和适用 AGENTS 优先，随后项目补充/指定复写，未覆盖部分使用 home 默认。项目文字不能改变 CLI 接口或实际 GitHub 权限。

## 从文档到交付

让 AI 阅读本项目开发文档和 ghship skill，建立交付里程碑，按需要建立父任务，再拆分可独立验收的叶片和必要依赖。授权范围明确后串行领取；每片独立开发、测试、验收。PR 用 `Closes #N`，平台原生门禁通过才合并；GitHub 自动关关联 Issue、删除远端开发分支。

无仓库变更的任务按 Issue 标准执行；需要独立验收时在 Issue 留独立结论，满足标准后关闭。测试计划、脚本和固定版本报告可保留，当前进度从平台读取。

Milestone 的 Issue 完成比例不等于产品验收。最终对照开发文档验证集成、安装更新卸载和真实产物；发现新问题建立同轮修复片，验收通过后结束父任务及里程碑。[AI 流水线](references/pipeline.md)给出完整操作与恢复路径。

## 更新与卸载

```sh
npx @ghship/cli@latest update
ghship project update --project /work/product
ghship project uninstall --project /work/product
npx @ghship/cli@latest uninstall
```

全局更新不遍历改所有项目。项目更新只改未修改的管理片段；卸载只移除该项目管理入口与绑定，保留定制skill、其他项目、账号、GitHub历史与平台治理。账号仍被项目引用则拒绝删除：

```sh
ghship user update --user developer --token-file /secure/new.json --non-interactive
ghship project uninstall --binding-only --project /old/missing/path
ghship user delete --user developer
```

直接下载渠道：新版下载二进制 `update --standalone`，另一个下载二进制 `uninstall --standalone`。未知目录、修改过的管理文件、版本降级拒绝覆盖/删除。项目文件变化需评审提交，本地卸载成功不表示远程已合并。

## AI 帮助与开发

先运行 `ghship help` / `ghship help user|project|init|doctor`，机器调用显式 `--non-interactive --json`。退出码、JSON 与参数契约见 `ghship help` 和 [CLI](references/cli.md)。未知状态不能当作成功；文档中的用户名、路径及编号必须替换为实际目标。

详细接口见 [接入配置](references/setup.md) 与 [实际流程](references/workflow.md)。源码开发需要 Go，不要求业务项目安装 Go：

```sh
go test ./...
go run ./tools/package
go run ./tools/smoke ./dist/npm
```
