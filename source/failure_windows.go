//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// 根据错误链分类；占用相关错误附加有时限的只读进程查询，不关闭程序或改写配置。
func fileFailureMessage(err error, mode repairMode, path string) string {
	if err == nil {
		return ""
	}
	lookupPath := path
	var code syscall.Errno
	hasCode := errors.As(err, &code)
	var reason, next string
	switch {
	case hasCode && (code == 32 || code == 33): // ERROR_SHARING_VIOLATION / ERROR_LOCK_VIOLATION
		reason = "文件被占用，Windows 检测到文件共享或锁定冲突。"
		if mode == modeDLL {
			next = "关闭使用该 DLL 的程序及其他修复工具后重试。若 OPPO 互联后台持续占用，请先保存工作并重启电脑，再按界面的登录和手机连接前提操作；仍失败可改用“Clash / Mihomo 配置”方式。"
		} else {
			next = "完全退出所选代理客户端（包括托盘程序），关闭正在编辑这份 YAML 的编辑器及其他修复工具，再重试。"
		}
	case errors.Is(err, os.ErrNotExist) || hasCode && (code == 15 || code == 53 || code == 67 || code == 123 || code == 161 || code == 206 || code == 267):
		reason = "路径已失效或不可用，Windows 无法找到或使用文件、目录。"
		if mode == modeDLL {
			next = "点击“重新查找”，核对 OPPO 互联的实际安装目录；也可“手动选择”现存文件。修复请选择 wintun.dll，恢复请选择 wintun.dll.oppo互联。"
		} else {
			next = "返回主界面点击“重新识别”，或“手动指定”实际使用的 YAML。恢复时请核对原 YAML 和所选 .bak 的位置，放回对应目录后重新选择备份。"
		}
	case errors.Is(err, os.ErrPermission) || hasCode && (code == 19 || code == 65 || code == 740 || code == 1314):
		reason = "权限不足或访问受限，Windows 拒绝了文件操作。"
		next = "本工具启动时会自动请求管理员授权，普通账户需电脑管理员授权。若已授权仍失败，请让管理员检查目标文件和文件夹“属性 → 安全”中的访问权限、只读状态及安全软件拦截记录。"
		if mode == modeDLL && code == syscall.ERROR_ACCESS_DENIED {
			next += "\n仅凭“拒绝访问”不能排除 DLL 被加载的情况；若 OPPO 后台持续占用，可保存工作并重启后按手机连接前提重试，或改用“Clash / Mihomo 配置”方式。"
		}
	default:
		// 保留格式校验、快照变化、拒绝覆盖等已有安全提示，不套用文件故障结论。
		return err.Error()
	}
	var pathErr *os.PathError
	var linkErr *os.LinkError
	if errors.As(err, &pathErr) {
		path = pathErr.Path
	} else if errors.As(err, &linkErr) {
		path = linkErr.Old + "\n→\n" + linkErr.New
	}
	body := reason + "\n\n下一步：\n" + next
	if path != "" {
		body += "\n\n相关路径：\n" + path
	}
	if hasCode {
		body += fmt.Sprintf("\n\nWindows 错误码：%d", uint32(code))
	}
	return body + occupancyFailureHint(err, mode, lookupPath) + "\n\n原始错误：\n" + err.Error()
}
