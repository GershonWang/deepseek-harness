#!/bin/sh
# 字节补丁 libwebkit2gtk-4.1.so.0，把编译期硬编码的 helper 进程路径
# /usr/lib/x86_64-linux-gnu/webkit2gtk-4.1 替换为 /tmp/dsh-webkit-4.1。
#
# 背景：Debian 正式构建的 webkit2gtk 把 helper 进程
# （WebKitNetworkProcess/WebKitWebProcess/WebKitGPUProcess）的目录编译进
# libwebkit2gtk-4.1.so.0（PKGLIBEXECDIR），且 WEBKIT_EXEC_PATH 只在
# DEVELOPER_MODE 构建里生效（发行版不启用），运行时 /usr 只读、玲珑 layer
# 不导出 /usr 写入。唯一现实方案是仿照 Lutris AppImage：字节替换 .so 内的
# 字符串到可写的 /tmp 短路径，启动时 launcher 建 /tmp 符号链接指向
# ${PREFIX} 下的真实 helper 目录。injected-bundle 路径同样替换，且
# launcher 额外设置 WEBKIT_INJECTED_BUNDLE_PATH（正式构建支持）。
#
# 用法：sh patch-webkit-exec-path.sh <libwebkit2gtk-4.1.so.0 路径>
#
# 找不到硬编码路径、或替换后仍能读到原路径时一律非零退出：补丁失效意味着
# helper 进程路径仍指向容器内不存在的 /usr/lib/...，产物会变成「安装成功但
# GUI 起不来」。构建期硬失败好过装到用户机器上才发现。
set -eu

SO=${1:?用法: patch-webkit-exec-path.sh <so 路径>}
[ -f "$SO" ] || { echo "patch-webkit: no such file: $SO" >&2; exit 1; }

python3 - "$SO" <<'PY'
import sys

path = sys.argv[1]
data = open(path, 'rb').read()
orig_exec = b'/usr/lib/x86_64-linux-gnu/webkit2gtk-4.1'
orig_bundle = b'/usr/lib/x86_64-linux-gnu/webkit2gtk-4.1/injected-bundle/'
new_exec = b'/tmp/dsh-webkit-4.1'
new_bundle = b'/tmp/dsh-webkit-4.1/injected-bundle/'

count_bundle = data.count(orig_bundle)
count_exec = data.count(orig_exec)

if count_bundle == 0 and count_exec == 0:
    print(f'patch-webkit: no hardcoded webkit paths found in {path}', file=sys.stderr)
    sys.exit(1)

# 等长替换成立的前提是替代串更短（否则 padding 长度算成负数）；在写盘前挡住，
# 避免将来改短路径时留下写坏的 .so。
for orig, new in ((orig_exec, new_exec), (orig_bundle, new_bundle)):
    if len(new) > len(orig):
        print(f'patch-webkit: replacement longer than original ({new!r} > {orig!r})', file=sys.stderr)
        sys.exit(1)

if count_bundle:
    pad = b'\x00' * (len(orig_bundle) - len(new_bundle))
    data = data.replace(orig_bundle, new_bundle + pad)

if count_exec:
    pad = b'\x00' * (len(orig_exec) - len(new_exec))
    data = data.replace(orig_exec, new_exec + pad)

# 写盘前最后一道自检：替换结果里不应再残留任何原始路径。
leftovers = data.count(orig_exec) + data.count(orig_bundle)
if leftovers:
    print(f'patch-webkit: {leftovers} hardcoded path occurrence(s) remain in {path}', file=sys.stderr)
    sys.exit(1)

open(path, 'wb').write(data)
print(f'patch-webkit: replaced {count_exec} exec path + {count_bundle} injected-bundle path in {path}')
PY
