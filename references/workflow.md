# 实际操作

在目标仓库根运行，`ghship` 二进制位于 PATH。通用 Git 操作直接用 Git，不包成第二套命令。启动、恢复或上下文压缩后先重读本 skill 及其 references 和项目规则，再做真实预检；预检不合格先停止并报告，不改路径绕过。

## 工作区与写者纪律

同一工作区同一时间一个写者：内容写入（编辑、add、commit、push）只由当前叶子的作者在共享工作目录进行；调度者不写内容，评审者只读。叶子之间的分支状态切换由调度者执行。不为同一任务新建 worktree、克隆或检出副本，也不在仓库内另建检出目录；查看默认分支内容可直接读对应远端引用，不需要检出。跨轮或跨会话暂停前先提交或 stash，不以未提交改动作为暂停点；发现活动写者或成果分歧先停止并报告。

派发任务书写明冻结版本（分支与 SHA）、关联 Issue/PR、唯一写者、逐项要求与证伪方式、不得回归清单与范围边界、未覆盖项与汇报格式。领取先于叶子开工：调度者完成指派、in-progress 和回读后作者才开工，作者不自行领取。

冻结 = 已提交 + 已推送 + 工作树干净 + 最新提交必需检查通过。收工序列：squash 合并 → 同步默认分支 → 删除已合本地分支 → 尽力移除 in-progress → 核对合并后默认分支 CI → 再取下一片。

开工与收工都自查并写入汇报：当前分支、`git status --porcelain` 行数、`git worktree list`、远端 `codex/*` 分支数量（应不超过 1）。

## 平台调用纪律

平台调用（`ghship gh`、`ghship git` 及直接 `gh`/`git`）失败时先区分瞬时故障与实质拒绝。瞬时故障如连接中断、`unexpected EOF`、TLS 握手超时、服务端 5xx 或 GraphQL 错误：在原命令上退避重试，有限次数、固定间隔，不更换身份、凭据通道或平台端点。

写请求结果不明（超时、连接中断、输出丢失）时先回读列表、详情、分支或日志，确认是否已生效；确认未生效才重试，禁止盲目重建 Issue、PR、分支或标签。

有限重试仍失败就停止并如实报告命令与环境，等待恢复；权限拒绝与门禁失败不是瞬时故障，停止并报告，不换通道绕行，不把未验证结果写成成功。

## 仓库识别与跨工具续接

目标 host/owner/repo 从当前物理项目的 origin 解析；`init --repo` 是显式校验，不是默认仓库。保存的绑定必须与 origin 一致，否则停止并报告，不能把任务发到产品源码仓库。

切换 AI 客户端不需要迁移聊天记录或本地任务状态库。原执行者先停止并保全未提交成果；新执行者读取本项目 AGENTS、共享 skill 与项目补充规则，然后：

1. 核实 cwd、origin、当前分支、HEAD、脏文件、worktree 列表及活动执行者；不得因恢复而重置或删除未知修改。
2. 查看绑定及真实 readiness，核验角色身份与权限。换机器需重新安装并初始化自己的凭据，不复制 token 到 Git。当前机器共享 home 安装可被不同开发工具复用。
3. 在授权里程碑或父任务范围内，读取 Open Issue 的 assignee、依赖与关联 PR；优先恢复同一作者唯一在途切片。多个候选或不同账号接手时明确原执行者已退出、选中具体切片，再变更指派；不能猜。
4. 已有 PR 时用其 head 分支、最新提交、Draft、检查和 review 事实决定继续开发、返修或等待验收；没有 PR 时核对 Issue 关联开发分支与已保全源码。PR 已合并则进入下一片，新缺陷另开 Issue/PR。
5. 回读任务正文、验收标准和实际源码，重新核验必要测试。看不到的本地修改、未推送提交或外部测试结果明确标为未知，不能由 Open 状态推断“尚未开发”。

读取示例（编号和范围必须来自本次授权）：

```bash
ghship project status --json
ghship gh author -- repo view --json nameWithOwner,defaultBranchRef
ghship gh author -- api user --jq .login
ghship gh author -- issue view <ISSUE> --json body,state,assignees,blockedBy,subIssuesSummary
ghship gh author -- pr list --state open --json number,headRefName,headRefOid,isDraft,author
ghship gh author -- pr view <PR> --json state,headRefName,headRefOid,isDraft,reviewDecision,statusCheckRollup,closingIssuesReferences
git status --short
git log -5 --oneline
```

`project status` 只说明本地安装和绑定，不证明平台 ready。关联 PR/分支通过 GitHub 原生关联与正文查看，不把无法查询到的关联当作不存在；必要时使用原生 API。任务恢复依据 Issue、PR、Review、CI 和 Git。仅将缺失的决策、阻塞原因和下一步补到 Issue。

上方pr list用于限量查看，不能根据它为空就断言没有PR。判断全量时使用原生分页：

```bash
TASK_REPO="$(ghship gh author -- repo view --json nameWithOwner --jq .nameWithOwner)"
ghship gh author -- api "repos/$TASK_REPO/pulls?state=open&per_page=100" --paginate --slurp
```

Issue/PR详情内的子任务、依赖、评审等嵌套列表也须核对完整性并补页。里程碑选片直接用 `ghship next --milestone <实际编号> --json`。

## 身份

角色账号使用当前项目 init 绑定，不默认指向 ghship 自身 bot。先用 user add/update 管理仓库外凭据，项目独立绑定开发、验收和本机 gh 管理员身份。角色命令仅读该角色凭据并核验实际 login；token 不输出，不把换账号当作独立验收。

`ghship gh <author|reviewer|dispatcher> -- <gh 参数...>` 透传实际平台命令。作者和验收执行者真实分离，编排者不能凭常规开发授权消费验收秘密。初始化可以在明确授权下核验角色及接受本仓库邀请。

## 领取与作者

任务已建立且范围已核实时，调度者先领取并回读；作者确认领取后从默认分支开工。恢复已有任务时复用其分支和 PR，不重复创建。

```bash
pwd
git rev-parse --git-common-dir
git status --short
git branch --show-current
git worktree list
git remote -v
BASE_BRANCH="$(gh repo view --json defaultBranchRef --jq .defaultBranchRef.name)"
ghship gh author -- issue create --title '<目标>' --body-file /tmp/issue.md
# 调度者执行，回读确认指派与标识后再交给作者。
ghship gh author -- issue edit <ISSUE> --add-assignee <AUTHOR_LOGIN> --add-label in-progress
ghship gh author -- issue view <ISSUE> --json state,assignees,labels
# 作者执行。
ghship gh author -- issue develop <ISSUE> --name codex/<ISSUE>-<slug> --base "$BASE_BRANCH" --checkout
# 实现并运行有关测试；提交信息用文件避免 shell 吃掉正文。
git -c user.name="AUTHOR_LOGIN" -c user.email="AUTHOR_NOREPLY_EMAIL" commit -F /tmp/commit.md
ghship gh author -- pr create --base "$BASE_BRANCH" --title '<变更>' --body-file /tmp/pr.md
```

认证推送使用当前项目的作者账号，避免系统 credential 缓存采用其他账号：

```bash
ghship git author -- push -u origin HEAD
```

本地 Git 操作直接执行。不要用 `gh auth setup-git` 修改全局配置，也不要为推送把 token 输出到终端；Git author.name/email 填实际账号信息。

PR 正文包含 `Closes #<ISSUE>`，说明变更、实际验证和风险，长度按复杂度确定。自动测试引用对应版本的 CI；人工验证说明被测版本、相关环境、方法、结果和未覆盖范围，必要材料使用可访问链接。

## 独立评审与合并

```bash
gh pr checks <PR> --required
ghship gh reviewer -- pr review <PR> --approve --body-file /tmp/review.md
# 若需返修：pr review <PR> --request-changes --body-file /tmp/review.md
ghship gh dispatcher -- pr merge <PR> --squash
```

实际检查及评审针对最新提交，以原生合并门禁为准。失败时查具体日志，按原因修复或原生重跑；严格检查要求与最新 base 同步。评审只读，评审证据与临时材料放在被评审仓库之外，不写入待评审内容；评审现场形式与证据留存按目标项目规则。

合并后的 PR 是历史记录；新增修改用新 PR。平台自动关 Issue、删远端头分支。本地需要继续工作时执行 `ghship tidy <PR>` 完成同步与安全清理；有独有成果或脏工作区则保留并报告。命令保证与失败处置见 [cli.md](cli.md)。
