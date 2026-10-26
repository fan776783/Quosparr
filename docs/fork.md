# Fork 维护说明

本仓库 `fan776783/Quosparr` 是 `Licoy/Quosparr` 的独立 fork，在上游基础上保留 Cursor 额度监控（上游 PR #59 未合并）。

## 与上游的差异

- Cursor 额度监控：`internal/cursor`、相关 UI / 偏好 / 文案与 `docs/cursor-monitoring.md`
- 更新源：`mygo.json` 的 `updates.github` 为 `fan776783/Quosparr`，`updates.publicKey` 为 fork 自有公钥；`internal/app/update.go` 的 Releases 链接同步指向 fork
- 版本号：`<上游版本>-cursor.<序号>`，如上游 `1.7.3` 对应 `1.7.3-cursor.1`，同一上游版本上的后续发布递增序号
- 发布：Fork Sync 发布 macOS arm64 与 Windows x64 正式包到同一个 fork Release；`Scripts/fork-release.sh` 保留本机 macOS 发布入口（设为 latest）；fork 仓库停用了 `Release Draft` 与 `Website` workflow，CI 保留

## 自动同步

`.github/workflows/fork-sync.yml` 每 6 小时（也可在 Actions 页手动运行）合并上游 `Licoy/Quosparr` 的 main：

- `Scripts/fork-merge.sh` 先合并但不提交，叠加 fork 配置后再生成一个合并提交；只有 `mygo.json` 冲突时自动解决：保留上游其他字段，写回 fork 的公钥与仓库；上游升版时版本号改为 `<上游版本>-cursor.1`，上游预发布版本不自动同步
- 其他冲突、测试失败，或上游改动了 `.github/workflows`（`GITHUB_TOKEN` 无权推送 workflow 文件，构建前检查）时不推送，运行失败并在摘要中列出原因，需按下方步骤手动同步
- 有新合并或待发布版本时，必须运行与 CI 共用的 `Scripts/check-go-release.sh`：`go vet`、race 测试、开发包测试、六平台交叉编译及全部发布辅助检查；检查通过后才推送和发布。没有新合并也不能跳过待发布版本的检查，检查失败不发布
- `mygo.json` 的版本尚无对应 Release 时，在 macOS runner 上用 `FORK_UPDATE_PRIVATE_KEY` Secret 打包签名并发布 arm64 版本，已安装的 fork 版本随后经应用内更新升级；写权限仅授予限定本 fork 的同步 job，签名私钥临时文件在退出时清理
- 同步成功后，独立 Windows x64 job 检查同版本 Release：缺少全部 Windows 附件时，从该 Release 的标签检出源码，以同一 `FORK_UPDATE_PRIVATE_KEY` 构建正式包（更新器开启），通过归档验签及 Windows 原生安装、卸载、SQLite 检查后，追加 `Quosparr.Setup.<version>.amd64.exe`、`quosparr-<version>-windows-amd64.tar.gz` 和 `update-windows-amd64.json`；更新清单最后上传。macOS 发布后、Windows job 完成前，Release 暂时只有 macOS 包
- Windows 三个附件已完整存在时跳过；因此重跑 Fork Sync 也能给已有版本补包。部分附件存在、附件为空或网络查询失败时停止，不覆盖已有文件、不移动标签；需核对上次上传后再处理。Windows 写权限同样只授予限定本 fork 的发布 job
- 手动运行时可勾选 `windows_only`：仅为当前 `mygo.json` 版本的已有 Release 补 Windows 包，跳过上游合并与 macOS 发布；定时运行仍按正常同步流程执行
- 普通 CI 还会执行 Windows / Linux 原生测试、安装器验证与 macOS 双架构开发包检查；Fork Sync 的共用检查不替代这些平台验收。GitHub macOS ARM runner 容量不足时任务可能未启动，恢复容量后可重跑；代码冲突必须先手动解决
- 运行失败由 GitHub 的定时 workflow 通知邮件提醒（fork 未开启 issues）

## 手动同步上游

```bash
git fetch origin
git switch main
bash Scripts/fork-merge.sh origin/main
# 若业务代码冲突，脚本会中止合并；手动 git merge --no-commit --no-ff origin/main 后逐项核对
# mygo.json 的 version 取上游版本加 -cursor.1；publicKey / github 保留 fork 值
bash Scripts/check-go-release.sh
git push fork main
```

## 发布

```bash
bash Scripts/fork-release.sh
```

脚本要求工作区干净、`HEAD` 已推送到 `fork/main`、版本号符合 `-cursor.N` 格式，签名私钥默认读取 `~/.config/quosparr-fork/mygo-update.key`（不进仓库，丢失后已安装的 fork 版本无法再收到更新）。已安装的 fork 版本通过应用内更新升级；从上游正式版切换到 fork 版需手动安装一次。

## 减少重复冲突

- `AGENTS.md` 与所有 `README*.md` 完全跟随上游，不加入 fork 内容或入口提示；Cursor 的七语言功能、隐私和验收说明只保留在 fork 独有的 `docs/fork-cursor.md`。
- Cursor 专有字段、初始化与测试条件独立成行，避免修改上游字段组及长条件表达式；新增逻辑优先放在现有 `cursor_*.go` 文件中。
- 不对 README、AGENTS.md 或业务代码配置全局 ours/theirs 策略；上游的新功能与 Cursor 功能必须同时保留。版本、更新仓库与公钥由配置叠加脚本明确维护。
- `python3 Scripts/Tests/test-fork-merge.py` 使用临时 Git 仓库验证版本冲突、源码冲突回滚、配置保留与重复同步。
