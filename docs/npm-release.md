# npm 发行操作

发布对象是主分支 verification 成功运行中保存的同一批七个包，不重新构建。主包与平台包使用相同版本，平台包不得含其他平台的可执行文件。

发布正式 GitHub Latest 前，先完成该提交的六平台 CI、指定测试机和独立验收，再将同一候选的七个 npm 包作为 Release 附件。`npm-release.yml` 收到 `release.published` 后回读当前 Latest，拒绝草稿、预发行、非 Latest、标签与提交不匹配以及不在 main 上的提交；自动选择该提交最新的成功 main verification 和该次运行的未过期构件，并确认打包和六个平台的必要步骤均在同一次 attempt 中成功。流程核对 Release 附件的服务端摘要，并实际下载七包与 CI 候选逐字节摘要比较；缺项、结果不明或不匹配时不发布。

人工恢复可通过 `workflow_dispatch` 指定该 Release 标签（例如 `gh workflow run npm-release.yml --ref vX.Y.Z -f verification_run=RUN -f artifact=ghship-candidate-N`），也可在 main 仍指向该发行提交时使用 main。恢复入口同样要求当前正式 Latest、匹配提交和最新成功 verification，不得借此发布未验收构件。仅重跑失败作业可能无法证明同一 attempt 覆盖所有平台，此时自动及人工入口均拒绝发布。须完整重跑全部作业并重新验收；若新候选与已发布的 Release 附件不一致，应停止并重新评估发行版本，不得替换已验收构件。

自动校验不能证明人工独立验收的内容；发布 Release 的执行者必须先完成并记录独立验收。由 GitHub Actions 的 `GITHUB_TOKEN` 创建的 Release 一般不会触发其他工作流，需按 GitHub 事件规则使用获授权的发布身份或显式执行人工恢复；不要为绕过该限制保存长期 npm 凭据。

流程确认 npm `latest` 不会倒退后先提交六个平台包，等待官方 registry 返回相同版本与完整性哈希，并使用独立空缓存实际下载和校验每个包，再以候选标签发布主包、验证可下载后更新 `latest`。每次提交前、更新 `latest` 前及等待标签回读时重新检查 GitHub 当前 Latest 与附件摘要。标签更新成功后若暂时读到更新前的值，最多等待约 55 秒；读到其他值或到期仍未确认时停止调查。重试已存在的版本时必须核对远端 integrity。哈希不匹配、权限拒绝或状态不明时停止调查。外部发布者可能在检查与 npm 修改之间并发操作，npm dist-tag 不提供条件更新，无法宣称该时间窗口具有原子性；发生竞争须回读并处置。镜像同步另行验证。

发布依赖 npm Trusted Publishing。为七个包配置 `ghship/cli` 仓库中的 `npm-release.yml`，允许发布；主包还须允许 dist-tag 管理，以支持安全续跑。使用 GitHub-hosted runner、OIDC 与自动 provenance，不保存长期 npm 发布令牌。

首次接入时六个平台包尚不存在，npm 不允许为不存在的包配置 Trusted Publisher。先使用经验收的同一批构件，以维护者交互验证逐一发布六个平台包并等待可用；核对远端 integrity 后为这六个包及主包配置同一可信 workflow，再运行发行流水线。已存在的可信配置须先读取核对，不能盲目覆盖或撤销。首次平台包已存在时流水线核对哈希并跳过，不重复发布；主包仍由流水线最后发布。

npm 发布新版本不可覆盖。每次失败后先查询对应包及版本，检查 integrity、扫描状态和 `latest`；结果不明时保留证据，不重新提交同一版本。必须先经六平台 CI 与独立验收，发布后使用隔离配置和真实旧版本安装验证完整升级、实际 skill、数据保全及镜像可用性。
