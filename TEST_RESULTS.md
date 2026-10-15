# 测试结果与已知范围

版本：v1.9 发布版。日期：2026-10-15。平台：Windows x64；Go 1.27.2；依赖通过 vendor 离线使用。

## 已完成验证

| 检查 | 结果 |
| --- | --- |
| 完整回归 | 50 项顶层测试、31 个模糊测试种子通过，0 失败；1 项符号链接测试因权限不足跳过；语句覆盖率 74.2% |
| 备份发现 | 仅匹配当前 YAML 同目录的对应备份，按文件名时间及数值序号倒序；过滤无效名称、日期和目录；不改写文件 |
| 原生界面 | 不默认选择网卡或备份；键盘选择、详情同步、取消、刷新和最终否决均通过；100% / 125% / 150% / 200% 模拟 DPI 布局通过 |
| 文件保护 | 备份、恢复前保护备份、确认后文件变化中止、DLL 不覆盖改名、DACL 和跨进程恢复等回归通过 |
| 构建与静态检查 | go vet、离线 AMD64 / Windows GUI 构建通过；内嵌 DPI manifest 与源码一致 |
| GitHub 发布构建 | 相同源码加 -trimpath 去除本机编译路径；PE / manifest 核对、Per-Monitor V2、10 次消息响应与正常关闭通过 |
| 实际 EXE 短测 | 两种方式切换、自动识别、常驻高亮、调整窗口、备份空态与手动入口、60 次消息响应和正常关闭通过 |
| 既有配置 | 实际 EXE 短测前后，5 份本地客户端配置 / 索引的 SHA256 保持一致 |

完整回归摘要见 [test-summary.json](verification/v1.9/test-summary.json)。含个人路径的本机截图和原始环境日志未纳入公开仓库。

## 临时配置的完整闭环

专项测试使用与 v1.9 发布源码一致的隔离副本，屏蔽真实客户端识别和系统网卡扫描，仅操作自动创建的临时文件。

1. 创建含 UTF-8 BOM、CRLF、中文注释的 209 字节 YAML。
2. 从原生窗口进入修复最终确认，测试自动化核对临时路径后选择“是”；配置真实改为 222 字节，生成原始备份。
3. 打开备份窗口，由程序自动找到对应备份，再用原生列表选择。打开和选择前后文件内容及目录没有变化。
4. 进入恢复最终确认并选择“是”，执行真实恢复，同时保存修复后配置的保护备份。
5. 恢复文件回到 209 字节，逐字节等于原文件；原始备份未变，保护备份等于修复后的内容，无残留锁或写入临时文件。

原文件与恢复后 SHA256 均为：

```text
2fa5d2ec37284bfa03861763656feba39869bdc0eb54b724a7a91ad5416a8784
```

专项摘要见 [result.json](verification/v1.9/roundtrip/result.json)，测试源码见 [roundtrip_integration_windows_test.go](verification/v1.9/roundtrip/roundtrip_integration_windows_test.go)。该测试与公开产品源码分开保存，便于核对发布源码未改动。

要复现该专项测试，可在仓库根目录使用 PowerShell 将测试文件临时复制到源码目录；仅操作测试自己创建的临时文件：

```powershell
$temporaryTest = Join-Path $PWD.Path 'source/roundtrip_integration_windows_test.go'
if (Test-Path -LiteralPath $temporaryTest) { throw '临时测试文件已存在，请先检查' }
Copy-Item -LiteralPath 'verification/v1.9/roundtrip/roundtrip_integration_windows_test.go' -Destination $temporaryTest
try {
    Push-Location 'source'
    try {
        go test -mod=vendor -count=1 -timeout=60s -run '^TestTemporaryYAMLRepairAutoFindRestore$' .
        if ($LASTEXITCODE -ne 0) { throw '专项测试未通过' }
    } finally { Pop-Location }
} finally { Remove-Item -LiteralPath $temporaryTest }
```

## 未验证事项

- 符号链接测试受 Windows Token 权限限制跳过，具体条目记录在回归摘要中。
- 真实手机连接、重启前后效果、真实代理配置恢复后的生效值和网络连通性未联调。
- 全部 Windows / 客户端版本、多显示器真实 DPI 切换、完整辅助功能及长期运行尚未验收。
- 外部进程最终替换竞争、脚本 / 订阅覆盖、真实权限 / 占用、断电及磁盘满等边界未全面覆盖。EXE 未签名，未运行 go test -race。
