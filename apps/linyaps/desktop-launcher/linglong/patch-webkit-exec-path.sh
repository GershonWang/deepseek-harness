#!/bin/sh
# 字节补丁 libwebkitgtk-6.0.so.4：把四处编译期硬编码的绝对路径替换为
# internal/packaging/short-paths.txt 里的短路径。
#
# 为什么需要（与 v2 时代 webkit2gtk-4.1 的做法同源，但对象更多）：
#   - helper 进程目录（WebKitWebProcess/WebKitNetworkProcess/WebKitGPUProcess 与
#     injected-bundle）以 PKGLIBEXECDIR 编译进 .so，WEBKIT_EXEC_PATH 只在
#     DEVELOPER_MODE 构建里生效，发行版不启用；
#   - WebKitGTK 6.0 默认开启 UI 进程沙箱，它按 /usr/bin/bwrap 启动 xdg-dbus-proxy
#     与 Web 进程。
# 运行时 /usr 只读、玲珑 layer 不导出 /usr 写入，因此只能把这些路径等长替换到
# /tmp 下的短路径，再由 launcher 启动时建符号链接指向包内真实文件。
#
# 用法：sh patch-webkit-exec-path.sh <libwebkitgtk-6.0.so.4 路径>
#
# 任一原路径找不到、替换串更长、替换后残留原串或读不到新串，一律非零退出：
# 补丁失效的表现是「装得上但窗口起不来」，构建期硬失败远好过真机才发现。
set -eu

SO=${1:?用法: patch-webkit-exec-path.sh <so 路径>}
[ -f "$SO" ] || { echo "patch-webkit: 文件不存在: $SO" >&2; exit 1; }

PATHS_FILE=$(dirname "$0")/../internal/packaging/short-paths.txt
[ -f "$PATHS_FILE" ] || { echo "patch-webkit: 缺少短路径单一来源文件: $PATHS_FILE" >&2; exit 1; }

python3 - "$SO" "$PATHS_FILE" <<'PYEOF'
import sys

so_path, paths_file = sys.argv[1], sys.argv[2]

values = {}
for line in open(paths_file, encoding='utf-8'):
    line = line.strip()
    if not line or line.startswith('#'):
        continue
    key, _, value = line.partition('=')
    values[key.strip()] = value.strip()

for key in ('WEBKIT_EXEC', 'BWARP', 'DBUS_PROXY'):
    if key not in values:
        print(f'patch-webkit: 短路径文件缺少 {key}', file=sys.stderr)
        sys.exit(1)
    value = values[key]
    if not value.startswith('/') or any(c.isspace() for c in value):
        print(f'patch-webkit: {key} 不是合法的绝对短路径: {value!r}', file=sys.stderr)
        sys.exit(1)

# injected-bundle 必须排在 exec 之前：它是 exec 路径的超集。
pairs = [
    (b'/usr/lib/x86_64-linux-gnu/webkitgtk-6.0/injected-bundle/',
     (values['WEBKIT_EXEC'] + '/injected-bundle/').encode(), 'injected-bundle'),
    (b'/usr/lib/x86_64-linux-gnu/webkitgtk-6.0',
     values['WEBKIT_EXEC'].encode(), 'webkit exec'),
    (b'/usr/bin/xdg-dbus-proxy', values['DBUS_PROXY'].encode(), 'xdg-dbus-proxy'),
    (b'/usr/bin/bwrap', values['BWARP'].encode(), 'bwrap'),
]

data = open(so_path, 'rb').read()
counts = {name: data.count(orig) for orig, _, name in pairs}
missing = [name for name, count in counts.items() if count == 0]
if missing:
    print(f'patch-webkit: 未找到硬编码路径: {", ".join(missing)}（上游构建方式变了？）', file=sys.stderr)
    sys.exit(1)

for orig, new, name in pairs:
    if len(new) > len(orig):
        print(f'patch-webkit: {name} 替换串更长（{new!r} > {orig!r}）', file=sys.stderr)
        sys.exit(1)

for orig, new, _ in pairs:
    data = data.replace(orig, new + b'\x00' * (len(orig) - len(new)))

for orig, new, name in pairs:
    if data.count(orig):
        print(f'patch-webkit: {name} 替换后仍残留原始路径', file=sys.stderr)
        sys.exit(1)
    if new not in data:
        print(f'patch-webkit: {name} 替换后读不到新路径 {new!r}', file=sys.stderr)
        sys.exit(1)

open(so_path, 'wb').write(data)
summary = ', '.join(f'{name} x{counts[name]}' for _, _, name in pairs)
print(f'patch-webkit: 已替换 {summary}')
PYEOF
