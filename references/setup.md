# 项目配置

## 原生配置

- 启用仓库 Issues，开启 **Auto-close issues with merged linked pull requests**。
- 开启 **Automatically delete head branches**（CLI：`gh repo edit --delete-branch-on-merge`）。
- 主分支要求 PR、独立 code owner 批准、最新提交批准、必要检查通过和讨论解决；禁止绕过。严格检查确保分支跟上最新 base。
- 规则集仅保护默认主分支，不用通配符保护所有开发分支，否则可能阻止自动删除。
- 必需检查名称必须与项目实际 CI 一致；检查应验证代码、任务关联和交付要求。

配置在安装或治理变更时检查；日常操作依赖平台门禁，不在每次开发前整份比对线上规则集、遍历协作者权限或查询全库状态。自动关单开关未在当前公开 API 暴露，设置时在 UI 确认一次，不要求每次合并人工重复确认。

项目使用自己的 CI、CODEOWNERS 和主分支规则，安装接入时核对实际配置。

先 `ghship doctor` 检查 Git/gh；缺工具按官方入口安装，再重试。用户级能力在 home 共享，项目接入单独安装/更新/卸载。项目定制 `.ghship/skill.md` 是用户内容，默认保留。详细使用教程见 README，机器契约见 cli.md。

## 身份与仓库

用户名来自初始化输入及项目角色绑定，不内置任何产品维护者账号。GitHub CODEOWNERS 不支持变量，治理接入 PR 必须按本项目选择的验收用户名写具体所有者规则；保留已有规则，评审其覆盖范围，不由安装器静默覆盖。

## 平台依据

- [关联 PR 与 Issue](https://docs.github.com/en/issues/tracking-your-work-with-issues/using-issues/linking-a-pull-request-to-an-issue)
- [自动删除头分支](https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/configuring-pull-request-merges/managing-the-automatic-deletion-of-branches)
- [必需检查与重跑](https://docs.github.com/en/pull-requests/how-tos/merge-and-close-pull-requests/troubleshooting-required-status-checks)
- [主分支保护及严格检查](https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/managing-protected-branches/about-protected-branches)

队列和父子关系需要支持 --parent、--blocked-by、blockedBy JSON 的新版gh；运行 `ghship doctor` 确认当前客户端能力；缺能力时升级 gh，不模拟本地关系。
