# Fork 维护说明

本仓库 `fan776783/Quosparr` 是 `Licoy/Quosparr` 的独立 fork，在上游基础上保留 Cursor 额度监控（上游 PR #59 未合并）。

## 与上游的差异

- Cursor 额度监控：`internal/cursor`、相关 UI / 偏好 / 文案与 `docs/cursor-monitoring.md`
- 更新源：`mygo.json` 的 `updates.github` 为 `fan776783/Quosparr`，`updates.publicKey` 为 fork 自有公钥；`internal/app/update.go` 的 Releases 链接同步指向 fork
- 版本号：`<上游版本>-cursor.<序号>`，如上游 `1.7.3` 对应 `1.7.3-cursor.1`，同一上游版本上的后续发布递增序号
- 发布：`Scripts/fork-release.sh` 在本机打包 macOS 版本、签名更新清单并发布到 fork Releases（设为 latest）；fork 仓库停用了 `Release Draft` 与 `Website` workflow，CI 保留

## 同步上游

```bash
git fetch origin
git switch main
git merge origin/main
# mygo.json 的 version 冲突时取上游版本加 -cursor.1；publicKey / github 保留 fork 值
go test ./... && go vet ./...
git push fork main
```

## 发布

```bash
bash Scripts/fork-release.sh
```

脚本要求工作区干净、`HEAD` 已推送到 `fork/main`、版本号符合 `-cursor.N` 格式，签名私钥默认读取 `~/.config/quosparr-fork/mygo-update.key`（不进仓库，丢失后已安装的 fork 版本无法再收到更新）。已安装的 fork 版本通过应用内更新升级；从上游正式版切换到 fork 版需手动安装一次。
