# Agent Note: Terminal sessions carry their own SHELL env fallback

Status: implemented

[English](2026-09-10-terminal-session-shell-env.md) | 中文

## Problem

每个新开的终端会话都会打印 `dircolors: no SHELL environment variable, and no shell type option given`。玲珑启动 GUI 应用时环境里不带 `SHELL`，launcher 进程——以及它经 `os.Environ()` 派生的所有子进程（包括 PTY shell）——都没有 `SHELL`。bash 随后会从 `/etc/passwd` 补一个 `$SHELL`，但它是未导出的 shell 变量，于是终端里 `echo $SHELL` 看起来完全正常，把缺口掩盖了；只有读环境的子进程（`~/.bashrc` 里的 dircolors）能观察到。同样的陷阱也让第一轮排查走了弯路：在无关 shell 里 `echo $SHELL` 同样报 `/bin/bash`，而 `env` 数出来是零条 `SHELL`。

## Decision

修复落在 `internal/terminal/session.go` 的 `ensureSessionEnv`——与已有的 `TERM` 兜底同一个接缝：合并后的环境缺少非空 `SHELL` 时追加 `SHELL=<command>`，即本会话正在启动的 shell（默认 `/bin/bash`）。非空的继承值保持不动；空的 `SHELL=` 条目一律剔除，因为 `getenv` 返回首条重复项，留着空值会让兜底失效。该函数是纯函数，keep/replace/inject 规则由表驱动测试钉住，不依赖测试进程自身的环境；另有一条集成测试在真实启动路径上钉住「恰好一条非空 SHELL」。

## Alternatives considered

**在 launcher 的 appenv 里对整个进程树一次性注入 `SHELL`。** 它能顺带覆盖 harness 的非交互 bash 工具，但后者从不读 `~/.bashrc`、没有任何症状，扩大爆炸半径换不来可观察收益，还要动经过凭据清洗的 env 链。

**在用户的 `~/.bashrc` 里消掉对 `SHELL` 的依赖（给 dircolors 加 `-b`）。** 治标不治本，且应用去改用户自己的配置文件不合适；同一缺口下其他读环境的工具（`less`、提示符主题等）依然坏着。

## Consequences

无论容器以何种方式启动，终端 shell 都能拿到可用的 `SHELL`，每次开终端的警告消失。取值等于会话启动的命令，未来若支持自定义 shell 选项会自动继承正确语义。终端接缝之外一切不变：harness bash 工具等其他子进程的环境里依然没有 `SHELL`，对非交互用途无害。
