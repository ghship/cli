# npm 发行操作

发布对象是主分支 verification 成功运行中保存的同一批七个包，不重新构建。主包与平台包使用相同版本，平台包不得含其他平台的可执行文件。

发布正式 GitHub Latest 前，先完成该提交的六平台 CI、指定测试机和独立验收，再将同一候选的七个 npm 包作为 Release 附件。`npm-release.yml` 收到 `release.published` 后回读当前 Latest，拒绝草稿、预发行、非 Latest、标签与提交不匹配以及不在 main 上的提交；自动选择该提交最新的成功 main verification 和该次运行的未过期构件，并确认打包和六个平台的必要步骤均在同一次 attempt 中成功。流程核对 Release 附件的服务端摘要，并实际下载七包与 CI 候选逐字节摘要比较；缺项、结果不明或不匹配时不发布。

人工恢复可通过 `workflow_dispatch` 指定该 Release 标签（例如 `gh workflow run npm-release.yml --ref vX.Y.Z -f verification_run=RUN -f artifact=ghship-candidate-N`），也可在 main 仍指向该发行提交时使用 main。恢复入口同样要求当前正式 Latest、匹配提交和最新成功 verification，不得借此发布未验收构件。仅重跑失败作业可能无法证明同一 attempt 覆盖所有平台，此时自动及人工入口均拒绝发布。须完整重跑全部作业并重新验收；若新候选与已发布的 Release 附件不一致，应停止并重新评估发行版本，不得替换已验收构件。

自动校验不能证明人工独立验收的内容；发布 Release 的执行者必须先完成并记录独立验收。由 GitHub Actions 的 `GITHUB_TOKEN` 创建的 Release 一般不会触发其他工作流，需按 GitHub 事件规则使用获授权的发布身份或显式执行人工恢复；不要为绕过该限制保存长期 npm 凭据。

流程确认 npm `latest` 不会倒退后先提交六个平台包，等待官方 registry 返回相同版本与完整性哈希，并使用独立空缓存实际下载和校验每个包，再以候选标签发布主包、验证可下载后更新 `latest`。每次提交前、更新 `latest` 前及等待标签回读时重新检查 GitHub 当前 Latest 与附件摘要。标签更新成功后若暂时读到更新前的值，最多等待约 55 秒；读到其他值或到期仍未确认时停止调查。重试已存在的版本时必须核对远端 integrity。哈希不匹配、权限拒绝或状态不明时停止调查。外部发布者可能在检查与 npm 修改之间并发操作，npm dist-tag 不提供条件更新，无法宣称该时间窗口具有原子性；发生竞争须回读并处置。镜像同步另行验证。

发布依赖 npm Trusted Publishing。为七个包配置 `ghship/cli` 仓库中的 `npm-release.yml`，允许发布；主包还须允许 dist-tag 管理，以支持安全续跑。使用 GitHub-hosted runner、OIDC 与自动 provenance，不保存长期 npm 发布令牌。

首次接入时六个平台包尚不存在，npm 不允许为不存在的包配置 Trusted Publisher。先使用经验收的同一批构件，以维护者交互验证逐一发布六个平台包并等待可用；核对远端 integrity 后为这六个包及主包配置同一可信 workflow，再运行发行流水线。已存在的可信配置须先读取核对，不能盲目覆盖或撤销。首次平台包已存在时流水线核对哈希并跳过，不重复发布；主包仍由流水线最后发布。

npm 发布新版本不可覆盖。每次失败后先查询对应包及版本，检查 integrity、扫描状态和 `latest`；结果不明时保留证据，不重新提交同一版本。必须先经六平台 CI 与独立验收，发布后使用隔离配置和真实旧版本安装验证完整升级、实际 skill、数据保全及镜像可用性。

## 镜像验收与缺包恢复

官方 npm 发行结果和第三方镜像同步状态分别记录。对需要支持的镜像，逐一核对主包及六个平台包的目标版本，从镜像以隔离空缓存实际下载七个 tarball，并将 SHA-256 和 SHA-512 与本次冻结候选的 manifest 比较。镜像主包的 `latest` 已更新，不能证明所有平台包已同步；缺少目标平台包时，使用该镜像的用户可能无法安装。记录镜像地址、版本、验收时间、各包结果和必要的独立复核证据；单凭 metadata、标签或同步任务受理不能判定可用。

以 npmmirror 缺少 `@ghship/cli-darwin-arm64` 为例：先确认官方 registry 的该版本可下载且完整性与候选一致，再针对该包请求同步。以下入口针对 npmmirror，不适用于其他镜像；包名中的 `/` 编码为 `%2F`：

```sh
curl --fail-with-body -X PUT 'https://registry.npmmirror.com/@ghship%2Fcli-darwin-arm64/sync?sync_upstream=true&nodeps=true'
```

受理响应中的 `logId` 仅表示已创建任务。查看 `https://registry.npmmirror.com/-/package/@ghship%2Fcli-darwin-arm64/syncs/<logId>/log` 的同步结果，然后重新从镜像实际下载目标版本并核对两个摘要。对其他缺包替换为经核实的包名；不要发送 npm 或 GitHub 凭据。同步未完成、请求失败、结果不明或摘要不一致时记录缺项并调查，不将镜像标为通过，也不重复发布或改写已发布的 npm 包；摘要不一致时停止使用该镜像产物。同步日志可能包含不必公开的维护者信息，记录任务结果时不要复制敏感信息。
