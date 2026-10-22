# Fork 维护说明

本仓库 `fan776783/Quosparr` 是 `Licoy/Quosparr` 的独立 fork，在上游基础上保留 Cursor 额度监控（上游 PR #59 未合并）。

## 与上游的差异

- Cursor 额度监控：`internal/cursor`、相关 UI / 偏好 / 文案与 `docs/cursor-monitoring.md`
- 更新源：`mygo.json` 的 `updates.github` 为 `fan776783/Quosparr`，`updates.publicKey` 为 fork 自有公钥；`internal/app/update.go` 的 Releases 链接同步指向 fork
- 版本号：`<上游版本>-cursor.<序号>`，如上游 `1.7.3` 对应 `1.7.3-cursor.1`，同一上游版本上的后续发布递增序号
- 发布：`Scripts/fork-release.sh` 在本机打包 macOS 版本、签名更新清单并发布到 fork Releases（设为 latest）；fork 仓库停用了 `Release Draft` 与 `Website` workflow，CI 保留

## 自动同步

`.github/workflows/fork-sync.yml` 每 6 小时（也可在 Actions 页手动运行）合并上游 `Licoy/Quosparr` 的 main：

- 只有 `mygo.json` 冲突时自动解决：保留上游其他字段，写回 fork 的公钥与仓库；上游升版时版本号改为 `<上游版本>-cursor.1`，上游预发布版本不自动同步
- 其他冲突、测试失败，或上游改动了 `.github/workflows`（`GITHUB_TOKEN` 无权推送 workflow 文件）时不推送，运行失败并在摘要中列出原因，需按下方步骤手动同步
- 有新合并或待发布版本时，必须运行与 CI 共用的 `Scripts/check-go-release.sh`：`go vet`、race 测试、开发包测试、六平台交叉编译及全部发布辅助检查；检查通过后才推送和发布。没有新合并也不能跳过待发布版本的检查，检查失败不发布
- `mygo.json` 的版本尚无对应 Release 时，在 macOS runner 上用 `FORK_UPDATE_PRIVATE_KEY` Secret 打包签名并发布 arm64 版本，已安装的 fork 版本随后经应用内更新升级；写权限仅授予限定本 fork 的同步 job，签名私钥临时文件在退出时清理
- 普通 CI 还会执行 Windows / Linux 原生测试、安装器验证与 macOS 双架构开发包检查；Fork Sync 的共用检查不替代这些平台验收。GitHub macOS ARM runner 容量不足时任务可能未启动，恢复容量后可重跑；代码冲突必须先手动解决
- 运行失败由 GitHub 的定时 workflow 通知邮件提醒（fork 未开启 issues）

## 手动同步上游

```bash
git fetch origin
git switch main
git merge origin/main
# mygo.json 的 version 冲突时取上游版本加 -cursor.1；publicKey / github 保留 fork 值
bash Scripts/check-go-release.sh
git push fork main
```

## 发布

```bash
bash Scripts/fork-release.sh
```

脚本要求工作区干净、`HEAD` 已推送到 `fork/main`、版本号符合 `-cursor.N` 格式，签名私钥默认读取 `~/.config/quosparr-fork/mygo-update.key`（不进仓库，丢失后已安装的 fork 版本无法再收到更新）。已安装的 fork 版本通过应用内更新升级；从上游正式版切换到 fork 版需手动安装一次。
