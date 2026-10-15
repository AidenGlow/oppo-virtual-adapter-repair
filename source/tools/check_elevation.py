"""核验发布 EXE 的真实 Windows 资源和普通权限启动行为，不点击 UAC。"""
from pathlib import Path
import argparse
import ctypes
import hashlib
import json
import subprocess
import xml.etree.ElementTree as ET


def read_manifest(exe):
    kernel = ctypes.WinDLL('kernel32', use_last_error=True)
    kernel.LoadLibraryExW.argtypes = [ctypes.c_wchar_p, ctypes.c_void_p, ctypes.c_uint32]
    kernel.LoadLibraryExW.restype = ctypes.c_void_p
    kernel.FindResourceW.argtypes = [ctypes.c_void_p, ctypes.c_void_p, ctypes.c_void_p]
    kernel.FindResourceW.restype = ctypes.c_void_p
    kernel.SizeofResource.argtypes = [ctypes.c_void_p, ctypes.c_void_p]
    kernel.SizeofResource.restype = ctypes.c_uint32
    kernel.LoadResource.argtypes = [ctypes.c_void_p, ctypes.c_void_p]
    kernel.LoadResource.restype = ctypes.c_void_p
    kernel.LockResource.argtypes = [ctypes.c_void_p]
    kernel.LockResource.restype = ctypes.c_void_p
    kernel.FreeLibrary.argtypes = [ctypes.c_void_p]
    module = kernel.LoadLibraryExW(str(exe), None, 0x22)  # 只读资源，不执行入口。
    if not module:
        raise ctypes.WinError(ctypes.get_last_error())
    try:
        resource = kernel.FindResourceW(module, ctypes.c_void_p(1), ctypes.c_void_p(24))
        if not resource:
            raise ctypes.WinError(ctypes.get_last_error())
        size = kernel.SizeofResource(module, resource)
        pointer = kernel.LockResource(kernel.LoadResource(module, resource))
        if not pointer or not size:
            raise RuntimeError('发布 EXE 的 manifest 资源不可读取')
        return ctypes.string_at(pointer, size)
    finally:
        kernel.FreeLibrary(module)


def check(exe):
    manifest = read_manifest(exe)
    source_manifest = Path(__file__).resolve().parents[1] / 'app.manifest'
    assert manifest == source_manifest.read_bytes(), 'EXE 内嵌 manifest 与源码不一致'
    document = ET.fromstring(manifest)
    namespace = {'v3': 'urn:schemas-microsoft-com:asm.v3'}
    level = document.find('.//v3:requestedExecutionLevel', namespace)
    assert level is not None and level.get('level') == 'requireAdministrator'
    assert level.get('uiAccess') == 'false'
    dpi = document.find('.//{http://schemas.microsoft.com/SMI/2016/WindowsSettings}dpiAwareness')
    assert dpi is not None and 'PerMonitorV2' in dpi.text
    result = {
        'exe': exe.name,
        'exe_sha256': hashlib.sha256(exe.read_bytes()).hexdigest(),
        'embedded_manifest_matches_source': True,
        'requested_execution_level': level.get('level'),
        'ui_access': False,
        'per_monitor_v2_preserved': True,
        'caller_is_administrator': bool(ctypes.windll.shell32.IsUserAnAdmin()),
        'uac_consent_clicked': False,
    }
    if result['caller_is_administrator']:
        result['non_elevated_launch_check'] = 'skipped: current caller is already elevated'
    else:
        startup = subprocess.STARTUPINFO()
        startup.dwFlags |= subprocess.STARTF_USESHOWWINDOW
        startup.wShowWindow = 0
        try:
            process = subprocess.Popen([str(exe)], startupinfo=startup, creationflags=subprocess.CREATE_NO_WINDOW)
        except OSError as error:
            assert error.winerror == 740, f'应返回 ERROR_ELEVATION_REQUIRED，实际为 {error.winerror}'
            result['non_elevated_launch_check'] = 'passed'
            result['windows_error'] = error.winerror
        else:
            process.terminate()
            process.wait(timeout=5)
            raise AssertionError('Windows 允许普通权限直接创建发布进程，提权声明未生效')
    return result


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('exe', type=Path)
    parser.add_argument('--report', type=Path)
    arguments = parser.parse_args()
    result = check(arguments.exe.resolve())
    text = json.dumps(result, ensure_ascii=False, indent=2) + '\n'
    if arguments.report:
        arguments.report.parent.mkdir(parents=True, exist_ok=True)
        arguments.report.write_text(text, encoding='utf-8')
    print(text)
