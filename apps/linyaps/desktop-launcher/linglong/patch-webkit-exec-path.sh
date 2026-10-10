#!/bin/sh
# 字节补丁 libwebkit2gtk-4.1.so.0，把编译期硬编码的 helper 进程路径
# /usr/lib/x86_64-linux-gnu/webkit2gtk-4.1 替换为 internal/packaging/webkit-exec-path.txt
# 里的短路径（当前为 /tmp/dsh-webkit-4.1）。
#
# 背景：Debian 正式构建的 webkit2gtk 把 helper 进程
# （WebKitNetworkProcess/WebKitWebProcess/WebKitGPUProcess）的目录编译进
# libwebkit2gtk-4.1.so.0（PKGLIBEXECDIR），且 WEBKIT_EXEC_PATH 只在
# DEVELOPER_MODE 构建里生效（发行版不启用，实测随包 .so 内不存在该字符串），
# 运行时 /usr 只读、玲珑 layer 不导出 /usr 写入。唯一现实方案是仿照 Lutris
# AppImage：字节替换 .so 内的字符串到可写的短路径，启动时 launcher 建符号链接指向
# ${PREFIX} 下的真实 helper 目录。injected-bundle 路径同样替换，且 launcher 额外
# 设置 WEBKIT_INJECTED_BUNDLE_PATH（正式构建支持）。
#
# 短路径不在本脚本里写字面量：launcher 侧以 //go:embed 读同一个文件
# （internal/packaging/webkit-exec-path.txt）。两边各写一遍的话，改一处就会产出
# 「装得上、GUI 起不来」的包，而两个环节都不会报错。
#
# 用法：sh patch-webkit-exec-path.sh <libwebkit2gtk-4.1.so.0 路径>
#
# 找不到硬编码路径、替换串比原串长、替换后仍能读到原路径、或替换后读不到新路径时
# 一律非零退出：补丁失效意味着 helper 进程路径仍指向容器内不存在的 /usr/lib/...，
# 产物会变成「安装成功但 GUI 起不来」。构建期硬失败好过装到用户机器上才发现。
set -eu

SO=${1:?用法: patch-webkit-exec-path.sh <so 路径>}
[ -f "$SO" ] || { echo "patch-webkit: no such file: $SO" >&2; exit 1; }

SHORT_PATH_FILE=$(dirname "$0")/../internal/packaging/webkit-exec-path.txt
[ -f "$SHORT_PATH_FILE" ] || { echo "patch-webkit: 缺少短路径单一来源文件: $SHORT_PATH_FILE" >&2; exit 1; }

python3 - "$SO" "$SHORT_PATH_FILE" <<'PY'
import sys

path, short_file = sys.argv[1], sys.argv[2]
short = open(short_file, encoding='utf-8').read().strip()
if not short.startswith('/') or any(c.isspace() for c in short):
    print(f'patch-webkit: 短路径无效（须为不含空白的绝对路径）: {short!r}', file=sys.stderr)
    sys.exit(1)

data = open(path, 'rb').read()
orig_exec = b'/usr/lib/x86_64-linux-gnu/webkit2gtk-4.1'
orig_bundle = b'/usr/lib/x86_64-linux-gnu/webkit2gtk-4.1/injected-bundle/'
new_exec = short.encode()
new_bundle = (short + '/injected-bundle/').encode()

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

# 反向自检：换掉的路径必须真的出现在结果里。只查"旧路径消失"是不够的——写坏或
# 漏写同样会让旧路径消失，而 launcher 会据此去建一个指向不存在路径的符号链接。
if count_exec and new_exec not in data:
    print(f'patch-webkit: replacement path {short!r} absent after patch in {path}', file=sys.stderr)
    sys.exit(1)
if count_bundle and new_bundle not in data:
    print(f'patch-webkit: replacement bundle path {short!r}/injected-bundle/ absent after patch in {path}', file=sys.stderr)
    sys.exit(1)

open(path, 'wb').write(data)
print(f'patch-webkit: replaced {count_exec} exec path + {count_bundle} injected-bundle path in {path}')
PY
