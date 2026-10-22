# 测试结果与已知范围

## v1.10.0（2026-10-22）

| 检查 | 结果 |
| --- | --- |
| 全量 Windows 业务回归 | 68 项顶层测试通过，0 失败；1 项符号链接测试因系统权限不足跳过 |
| 模糊测试种子与覆盖率 | 31 个种子通过；语句覆盖率 72.3% |
| 网卡筛选与推荐 | 联网物理网卡筛选、显示全部、唯一网关候选、多出口不猜测、未知属性、原始接口名及切换后清空选择通过 |
| 运行设置判断 | Rev / CFW 临时配置：已生效、TUN 未开启、字段不一致、字段缺失、内核未运行、多内核歧义、配置检查中变化均通过 |
| 本机控制接口 | 真实本机 HTTP GET、认证、绕过系统代理、拒绝跳转 / 远程地址；真实 Windows 命名管道 GET、核对服务端 PID、错误归属不发送请求通过 |
| 后台与原生界面 | 检查不阻塞窗口、切换方式取消、丢弃旧结果、检查按钮状态及两种修复方式独立性通过；原有 DPI / 键盘 / 高亮 / 备份窗口回归继续通过 |
| 临时文件完整流程 | 修复 → 检查运行设置 → 自动查找备份 → 恢复；恢复后与原文件逐字节一致。检查期间源文件、运行配置未被改写 |
| 静态分析与发布构建 | Go 1.27.2、go vet、离线 Windows x64 GUI 构建通过；EXE 内嵌管理员 / PerMonitorV2 资源与源码一致，普通权限启动返回 740 |

概要及样本截图见 [verification/v1.10.0](verification/v1.10.0/)。本轮所有文件写入使用临时配置；没有修改真实 OPPO 文件和代理配置。运行接口测试使用本机模拟内核，尚未覆盖真实 Rev / CFW 全部版本的运行接口组合。检查确认内核采用了出口 / TUN 设置，不等同于证明真实网络已恢复；没有新增真实手机连接与重启后的网络联调结果。

业务回归使用隔离副本的 asInvoker 资源；正式发布 EXE 保留 requireAdministrator 和 PerMonitorV2，资源及普通权限启动要求另行核验。主业务测试日志保留于本机临时验证目录。

## v1.9.2（2026-10-15）

| 检查 | 结果 |
| --- | --- |
| 全量业务回归 | 54 项顶层测试通过，0 失败；1 项符号链接测试因 Token 权限不足跳过 |
| 模糊测试种子 | 31 个通过；语句覆盖率 70.9% |
| 新增错误提示回归 | 17 个 Windows 错误码场景、错误链中的实际路径、源与目标路径及备份上下文通过；非本次范围的安全提示原样保留 |
| 真实 Windows 临时文件故障 | 持有 DLL 句柄造成共享冲突；仅对临时 YAML 设置专用 DACL 造成访问拒绝；不存在的临时 DLL 路径造成查找失败。分类、操作指引与失败后文件不变检查通过 |
| 原生窗口 | 缺失 YAML 时，备份窗口显示重新识别 / 手动指定指引，恢复按钮禁用，未生成文件 |
| 构建与资源 | go vet、Go 1.27.2 离线 Windows AMD64 GUI 构建通过；内嵌 requireAdministrator / PerMonitorV2 与源码一致，普通权限直接启动返回 740 |

记录见 [test-summary.json](verification/v1.9.2/test-summary.json) 和 [elevation.json](verification/v1.9.2/elevation.json)。业务回归在隔离源码副本中使用 asInvoker 测试资源；正式源码与 EXE 保留 requireAdministrator。没有点击 UAC 授权窗口、操作真实 OPPO 文件或代理配置，也未执行重启。本轮专门验证失败提示，未新增真实设备和网络联调结果。

错误码依据：[Microsoft Windows 系统错误码](https://learn.microsoft.com/en-us/windows/win32/debug/system-error-codes--0-499-)。只依据错误链分类，不通过错误文字猜测，也不自动关闭程序或调整文件权限。

## v1.9.1（2026-10-15）

| 检查 | 结果 |
| --- | --- |
| 业务回归 | 50 项顶层测试、31 个模糊测试种子通过，0 失败；1 项符号链接测试因 Token 权限不足跳过 |
| 静态检查与构建 | go vet、Go 1.27.2 离线 Windows AMD64 GUI 构建通过，使用 -trimpath |
| 发布 EXE 资源 | 通过 Windows 原生资源 API 读取；与源码 manifest 字节一致，requireAdministrator、uiAccess=false，PerMonitorV2 保留 |
| Windows 实际启动规则 | 普通权限直接 CreateProcess 返回 ERROR_ELEVATION_REQUIRED（740），确认系统已识别管理员启动要求 |

业务回归在普通权限的 asInvoker 测试资源下执行；随后生成 requireAdministrator 发布资源，构建最终 EXE 并独立执行 Windows 启动检查。除版本常量外，Go 业务代码未改动。记录见 [test-summary.json](verification/v1.9.1/test-summary.json) 和 [elevation.json](verification/v1.9.1/elevation.json)。本轮没有代替用户点击系统 UAC 授权窗口，也未操作真实 OPPO 文件、代理配置或执行重启。

可在普通权限 PowerShell 中复现发布检查（仅开发验证需要 Python）：

```powershell
python source/tools/check_elevation.py '.\OPPO互联连接修复工具_v1.9.1.exe'
```

从当前发布源码运行 Go 测试时请使用管理员终端，因为测试 EXE 也会嵌入管理员启动声明。

## v1.9 历史验证记录

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
