# 字节补丁 libwebkitgtk-6.0.so.4，把编译期硬编码的 helper 进程路径替换为
# internal/packaging/webkit-exec-path.txt 里的短路径。
#
# 为什么需要：Debian/Deepin 的正式构建把 helper 目录（WebKitWebProcess /
# WebKitNetworkProcess / WebKitGPUProcess 与 injected-bundle）编译进
# libwebkitgtk-6.0.so.4（PKGLIBEXECDIR），而 WEBKIT_EXEC_PATH 只在 DEVELOPER_MODE
# 构建里生效（发行版不启用）。运行时 /usr 只读、玲珑 layer 不导出 /usr 写入，
# 唯一现实方案是把 .so 内的字符串等长替换到可写的短路径，启动时由包装脚本把该路径
# 建为指向 ${PREFIX} 下真实 helper 目录的符号链接。
#
# 用法：sh patch-webkit-exec-path.sh <libwebkitgtk-6.0.so.4 路径>
#
# 找不到硬编码路径、替换串更长、替换后仍残留原路径或读不到新路径，一律非零退出：
# 补丁失效意味着包「装得上、窗口起不来」，构建期硬失败远好过真机才发现。
set -eu

SO=${1:?用法: patch-webkit-exec-path.sh <so 路径>}
[ -f "$SO" ] || { echo "patch-webkit6: 文件不存在: $SO" >&2; exit 1; }

SHORT_PATH_FILE=$(dirname "$0")/../internal/packaging/webkit-exec-path.txt
[ -f "$SHORT_PATH_FILE" ] || { echo "patch-webkit6: 缺少短路径单一来源文件: $SHORT_PATH_FILE" >&2; exit 1; }

python3 - "$SO" "$SHORT_PATH_FILE" <<'PY'
import sys

path, short_file = sys.argv[1], sys.argv[2]
short = open(short_file, encoding='utf-8').read().strip()
if not short.startswith('/') or any(c.isspace() for c in short):
    print(f'patch-webkit6: 短路径无效（须为不含空白的绝对路径）: {short!r}', file=sys.stderr)
    sys.exit(1)

data = open(path, 'rb').read()
orig_exec = b'/usr/lib/x86_64-linux-gnu/webkitgtk-6.0'
orig_bundle = b'/usr/lib/x86_64-linux-gnu/webkitgtk-6.0/injected-bundle/'
new_exec = short.encode()
new_bundle = (short + '/injected-bundle/').encode()

count_exec = data.count(orig_exec)
count_bundle = data.count(orig_bundle)
if count_exec == 0:
    print(f'patch-webkit6: {path} 里没有硬编码的 helper 路径（上游构建方式变了？）', file=sys.stderr)
    sys.exit(1)

for orig, new in ((orig_exec, new_exec), (orig_bundle, new_bundle)):
    if len(new) > len(orig):
        print(f'patch-webkit6: 替换串更长 ({new!r} > {orig!r})', file=sys.stderr)
        sys.exit(1)

data = data.replace(orig_bundle, new_bundle + b'\x00' * (len(orig_bundle) - len(new_bundle)))
data = data.replace(orig_exec, new_exec + b'\x00' * (len(orig_exec) - len(new_exec)))

if data.count(orig_exec) or data.count(orig_bundle):
    print('patch-webkit6: 替换后仍残留原始路径', file=sys.stderr)
    sys.exit(1)
if new_exec not in data:
    print(f'patch-webkit6: 替换后读不到新路径 {short!r}', file=sys.stderr)
    sys.exit(1)

open(path, 'wb').write(data)
print(f'patch-webkit6: 替换 {count_exec} 处 exec 路径、{count_bundle} 处 injected-bundle 路径')
PY
