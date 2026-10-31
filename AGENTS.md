# GHShip 开发规则

遵守适用的父级 AGENTS 工作区与独立验收要求。修改流程或分发契约前阅读 [当前流程与文档设计](docs/process-design.md)。

## 开发内容与交付内容

- 内部设计、评估、调试、迁移取舍和历史说明放在 `docs/` 或 Issue/PR，供维护者阅读。
- `README.md`、`SKILL.md`、`references/`、CLI 帮助和 `npm/README.md` 面向用户或使用工具的 AI：只说明当前能力、操作、有效约束及真实限制。不要夹带旧接口、事故叙事、已删除机制清单、开发进度或解释性历史注释。
- 精简不能删除必要的身份、权限、验收、数据保全和未知状态规则。发布可用性必须据实说明；尚未发布的 registry 命令不能写成已可用。
- 公共内容维护一份源，通过版本一致的嵌入和打包交付；不复制两套容易分歧的 skill，不增加运行时“内部/发布”模式。
- CLI 内嵌只允许 `SKILL.md` 和明确列出的公共引用；npm 包只包含启动器、说明、校验和与对应平台二进制。`AGENTS.md`、`docs/`、开发报告、凭据和本机路径不进入分发包。
- 每次交付检查实际安装的 skill 与源码一致，并检查包内容，而不是只检查仓库文档。完成以独立验收、原生 CI 和实际交付为准。

## 任务与验证记录

Issue 是任务推进的统一入口；PR、Review、CI 和交付产物记录各自原生事实。只补充原生记录不能表达的范围、决策、阻塞与交接信息，不维护第二份动态任务账本。

测试计划、脚本、需求覆盖及固定版本验收报告可以版本化。依据按验收目标选择，不强制统一报告槽位或命令/退出码/SHA/链接四项格式。当前进度从平台读取，历史结果标明对象和时间。

## 回归与成品验收

- 缺陷修复先用回归场景复现失败，再验证修复；覆盖相邻状态转换，不能只测出错函数。
- 发布按代码路由、参数解析及实际行为枚举全部命令及子命令，并核对帮助和教程，记录成功、缺项、失败、重复执行和数据保全场景。干净配置、既有目录、系统文件、损坏条目和权限异常都要纳入。
- 分清单元测试、受控集成测试与实际成品/平台操作。完整测试套件通过不等于所有真实流程通过；未覆盖项明确记录，不用帮助输出或参数拒绝冒充成功路径验证。
- 在指定测试机运行全部回归，使用实际安装包验证CLI连续操作与安装更新卸载；真实凭据只在授权执行者的隔离测试配置内使用，禁止输出或写入测试报告。原用户配置和项目成果必须保留。


<!-- ghship:begin -->
## ghship project integration

Use the ghship skill at the current user's home default `.agents/skills/ghship/SKILL.md`,
including its `references/pipeline.md`, `references/workflow.md` and `references/cli.md`.
Read this repository's `.ghship/skill.md` if present, then `.ghship/README.md`.
Apply current user instructions and applicable AGENTS constraints first; project
supplement/explicit scoped overrides then global defaults. Never invent results.
CLI help and actual platform permissions remain authoritative software facts.
Keep project rules outside these markers or in `.ghship/skill.md`; editing inside
them makes `ghship project update` preserve the file and report a conflict.
<!-- ghship:end -->
