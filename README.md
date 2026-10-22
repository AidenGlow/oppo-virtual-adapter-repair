# OPPO互联占用虚拟网卡修复

## Windows 用户下载提示

> **Windows 10 / 11（64 位）用户，推荐直接下载：[Windows EXE（点击下载）](https://github.com/AidenGlow/oppo-virtual-adapter-repair/releases/latest/download/oppo-virtual-adapter-repair-windows-amd64.exe)。**
>
> 文件名：`oppo-virtual-adapter-repair-windows-amd64.exe`。`amd64` 表示 x64 架构，Intel 和 AMD 的 64 位电脑都适用。
>
> 下载后双击 EXE，在 Windows 管理员授权窗口点击“是”即可运行。

- **只想使用工具：** 下载上面的 `.exe` 即可。
- **需要程序、源码和中文说明：** 下载[完整包 ZIP](https://github.com/AidenGlow/oppo-virtual-adapter-repair/releases/latest/download/oppo-virtual-adapter-repair-windows-amd64-full.zip)，解压后运行其中的 EXE。
- **[SHA256-latest.txt](https://github.com/AidenGlow/oppo-virtual-adapter-repair/releases/latest/download/SHA256-latest.txt)：** 用于核对下载文件的完整性。
- **`Source code (zip)` / `Source code (tar.gz)`：** GitHub 自动生成的源码包；直接使用工具请选择 EXE 或完整包。

一个用于排查 **OPPO 互联与 Clash / Mihomo TUN 网络冲突**的 Windows 小工具：自动找到需要处理的文件，你选择修复方式，确认后再修改，遇到问题可以恢复。

**Windows 10 / 11 · 64 位 · 中文界面 · 单文件 EXE · [最新发布版](https://github.com/AidenGlow/oppo-virtual-adapter-repair/releases/latest)**

## v1.10.0 新增功能

- **网卡列表更简洁：**默认只显示已连接的物理网卡；“显示全部”可查看虚拟、断开及属性未知的网卡。只有唯一符合连接、物理属性和明确 IPv4 默认网关条件的候选才标注“建议选择”，详情显示理由。推荐不会代替选择，切换筛选和刷新均清空旧选择。
- **检查是否生效：**配置方式新增同名按钮。修复后重新打开所选代理客户端，再点击检查。工具核对所选配置对应的运行内核，通过本机控制接口只读获取当前出口网卡和 TUN 设置，区分已生效、运行设置不一致、TUN 未开启、尚未加载与无法确认。
- 保留两种修复方式自主选择、Go / Win32 单文件架构、管理员启动、备份、恢复及最终确认。

## 为什么做这个工具

本项目针对的一类问题是：**安装 OPPO 互联后，Clash / Mihomo 原本可用的 TUN 模式一开启就无法联网。**

Clash Verge Rev 的 [讨论 #6712](https://github.com/clash-verge-rev/clash-verge-rev/discussions/6712) 记录了一个案例：安装 OPPO 互联后新增虚拟网卡 `utun8`，开启 TUN 即断网；单独卸载该网卡，重启后又会出现；卸载 OPPO 互联后，网卡移除，TUN 恢复。

**这个案例指向 OPPO 互联引入的虚拟网卡与代理 TUN 共存时的网络冲突。** 为什么会影响整机上网？Mihomo 的 TUN 在启用自动路由时会接收全局流量，处理后的连接仍需通过实际联网网卡发出。结合其[自动选择出口接口的机制](https://wiki.metacubex.one/config/inbound/tun/#auto-detect-interface)，一个合理推断是：新增虚拟网卡影响了出口识别或路由，使流量进入 TUN 后没有从正确的联网接口发出，于是表现为“打开 TUN 就断网”。

截图没有路由表和内核日志，具体冲突环节仍待确认；`utun8` 也是该案例的网卡名，其他电脑可能不同。详细依据、为什么删网卡后会重现，以及两种修复方式的作用见 [原因说明](docs/原因说明.md)。

这个工具把社区排障中的 DLL 改名和代理出口网卡配置放到同一个中文界面里，自动寻找目标文件、显示网卡信息，并提供备份和恢复，减少手动找文件、改错位置和忘记备份的问题。

这两种方法适用于不同情况，可以自行选择其中一种，无需依次执行。它们来自社区排障经验，实际连接效果取决于软件版本和网络环境。本项目是第三方工具。

## 两种方式分别解决什么

| 方式 | 处理什么 | 需要注意 |
| --- | --- | --- |
| **OPPO DLL 修复** | 将 OPPO 安装目录中 `daemon\wintun.dll` 改名为 `wintun.dll.oppo互联`，用于尝试排查相关组件冲突；支持改回原名 | 必须先登录 OPPO 互联并连接手机。修复和恢复后都要重启电脑；修改后可能无法连接新设备 |
| **Clash / Mihomo 配置** | 为 TUN 明确指定实际联网的 Wi-Fi / 以太网，避免依赖自动选择出口网卡；自动识别 Clash Verge Rev 和 Clash for Windows 的配置 | 修改前完全退出代理软件；由你选择网卡，完成后重新打开。订阅刷新或脚本可能覆盖设置 |

## 下载和使用

普通 Windows 用户请使用首页顶部的 **Windows EXE 直接下载**入口。完整包和 SHA256 校验文件见 [最新发布页面](https://github.com/AidenGlow/oppo-virtual-adapter-repair/releases/latest)。

双击 EXE，Windows 会自动弹出管理员授权窗口，确认后进入界面，无需寻找右键菜单。取消授权则程序不启动；修改文件仍需在工具内最终确认。无需安装 Go 或 Python。

1. **选择一种修复方式**，等待工具自动找到 OPPO 文件或代理客户端配置。
2. DLL 方式先核对目录，并勾选手机已连接；配置方式先退出代理客户端，再扫描、选择实际联网的网卡；默认只列已连接物理网卡，需要时点“显示全部”。
3. 阅读最终确认窗口，核对文件路径和改动，点击“是”后才会修改。
4. 按提示重启电脑或重新打开代理软件。配置方式可点击“检查是否生效”，再打开常用网页确认联网情况。

**打开界面、识别配置、扫描 / 筛选网卡及检查是否生效均不会修改配置。取消最终确认也不会修改。** 工具只处理明确选中的文件，保留用户选择和确认；不安装驱动、不修改系统 DNS、路由表或全局代理。

## 出现问题怎样恢复

- **DLL：**选择原目录中的 `wintun.dll.oppo互联`，点击“恢复 DLL”，确认后重启电脑。
- **YAML：**选择对应客户端 / 配置，点击“恢复 YAML 备份”。工具会自动列出当前配置同目录的备份，按时间从新到旧排列；由你选一份，再最终确认。找不到时可点“手动选择文件…”。恢复前还会另存当前配置。
- **删掉工具后仍可恢复：**重新下载工具即可，前提是原目录中的已改名 DLL，或原 YAML 及旁边的 `.bak` 还在。请保留这些文件和所在目录。

详细步骤见 [使用说明](docs/使用说明.md)。

## 界面示例

以下截图使用临时样本配置；生效结果为测试样本：

![Clash / Mihomo 配置界面](docs/images/proxy-sample.png)

![简洁网卡列表（样本）](docs/images/nic-simple-sample.png)

本版新增简洁网卡列表和“检查是否生效”。检查只读取所选客户端的本机状态，不自动重载配置、关闭客户端或发起外网测速。详见[使用说明](docs/使用说明.md#检查修复是否生效)。

## 测试到哪一步了

- **v1.10.0：68 项业务回归、31 个模糊测试种子通过，0 失败，1 项权限相关测试跳过。** 网卡筛选、真实本机 HTTP / 命名管道、只读生效检查、后台取消和临时文件修复恢复通过。

- v1.9.2：54 项业务回归、31 个模糊测试种子通过，0 失败，1 项权限相关测试跳过。新增真实临时文件占用、访问拒绝、路径缺失及原生备份窗口指引检查通过。

- v1.9.1：50 项业务回归和 31 个模糊测试种子通过，0 失败，1 项权限相关测试跳过；发布 EXE 的原生资源核对及 Windows 要求提权（错误 740）检查通过。

- v1.9 回归：**50 项测试、31 个模糊测试种子通过，0 失败**；1 项符号链接测试因系统权限不足跳过。`go vet` 和离线 Windows x64 构建通过。
- 额外用临时 YAML 完整跑通 **修复 → 自动查找备份 → 列表选择 → 最终确认 → 恢复**，包含实际文件写入；恢复后的字节和 SHA256 与原文件一致，恢复前保护备份也核对通过。
- 真实手机连接、真实代理配置修改后的网络效果，以及全部 Windows / 客户端版本尚未联调验证。

记录和复现方法见 [测试结果](TEST_RESULTS.md)。

## 从源码运行和编译

采用 **Go + 原生 Win32 API**，依赖源码已放在 `source/vendor/`，可离线构建。在 Windows 安装 Go 1.23 或更新版本后，在管理员终端执行测试和构建（测试 EXE 也包含提权声明）：

```powershell
Set-Location 'source'
go test -mod=vendor -count=1 ./...
go vet -mod=vendor ./...
go build -trimpath -mod=vendor -ldflags='-H windowsgui -s -w' -o '..\OPPO互联连接修复工具_v1.10.0.exe' .
```

本版实际使用 Go 1.27.2 构建；未使用 Go 1.23 工具链复测。发布构建使用 `-trimpath` 去除本机编译路径。修改 `app.manifest` 后先运行 `go generate .`。

自有代码使用 [MIT 许可证](LICENSE)。第三方组件和软件图标的来源及许可见 [THIRD_PARTY_NOTICES.txt](THIRD_PARTY_NOTICES.txt)。版本变化见 [CHANGELOG](CHANGELOG.md)。

发布附件和固定下载地址的维护方法见[发布说明](docs/发布说明.md)。
