# shellcheck shell=sh
# ll-builder 用 setuid 的 newuidmap/newgidmap 映射用户命名空间。Chromium 系集成终端
# （VS Code、Electron 应用）会给子进程设置 NoNewPrivs=1，setuid 随即失效，映射以
# EPERM 失败，终端上只剩一条含混的 `newuidmap: write to uid_map failed`。检测到
# 这种终端时改走启动器生成的宿主透传包装：systemd-run --user 的服务侧
# NoNewPrivs=0，映射照常完成。包装不存在就明确失败，提示换普通终端，而不是把
# 构建器内部的原始报错留给下一次尝试。
if [ -r /proc/self/status ] && grep -q '^NoNewPrivs:[[:space:]]*1$' /proc/self/status; then
  wrapper_dir="$HOME/.config/dsh-desktop/linglong-bin"
  if [ -x "$wrapper_dir/ll-builder" ]; then
    PATH="$wrapper_dir:$PATH"
    export PATH
    echo "ll-builder preflight: 当前终端 NoNewPrivs=1，改用 $wrapper_dir/ll-builder" >&2
  else
    echo "ll-builder preflight: 当前终端带 NoNewPrivs=1（VS Code/Electron 集成终端），setuid newuidmap 无法映射用户命名空间。" >&2
    echo "请在普通终端运行，或先启动 DSH 桌面客户端以生成 $wrapper_dir/ll-builder。" >&2
    exit 1
  fi
fi
