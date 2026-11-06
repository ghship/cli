# npm 发行操作

发布对象是主分支 verification 成功运行中保存的同一批七个包，不重新构建。主包与平台包使用相同版本，平台包不得含其他平台的可执行文件。

`npm-release.yml` 通过 `workflow_dispatch` 接收成功的主分支 verification run ID 和该 run 的候选 artifact 名称。流程核验工作流、提交、结论、构件和 SHA-256；确认 npm `latest` 不会倒退后先提交六个平台包，等待官方 registry 返回相同版本与完整性哈希，并使用独立空缓存实际下载和校验每个包，再发布主包并确认 `latest`。重试已存在的版本时必须核对远端 integrity；哈希不匹配、权限拒绝或状态不明时停止调查。npm 发布扫描可能延迟可用；主包不能在平台包可用前发布。镜像同步另行验证。

发布依赖 npm Trusted Publishing。为七个包配置 `ghship/cli` 仓库中的 `npm-release.yml`，允许发布；主包还须允许 dist-tag 管理，以支持安全续跑。使用 GitHub-hosted runner、OIDC 与自动 provenance，不保存长期 npm 发布令牌。

首次接入时六个平台包尚不存在，npm 不允许为不存在的包配置 Trusted Publisher。先使用经验收的同一批构件，以维护者交互验证逐一发布六个平台包并等待可用；核对远端 integrity 后为这六个包及主包配置同一可信 workflow，再运行发行流水线。已存在的可信配置须先读取核对，不能盲目覆盖或撤销。首次平台包已存在时流水线核对哈希并跳过，不重复发布；主包仍由流水线最后发布。

npm 发布新版本不可覆盖。每次失败后先查询对应包及版本，检查 integrity、扫描状态和 `latest`；结果不明时保留证据，不重新提交同一版本。必须先经六平台 CI 与独立验收，发布后使用隔离配置和真实旧版本安装验证完整升级、实际 skill、数据保全及镜像可用性。
