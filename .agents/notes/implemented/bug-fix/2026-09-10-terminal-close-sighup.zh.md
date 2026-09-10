# Agent Note: Terminal close signals SIGHUP, not SIGTERM

Status: implemented

[English](2026-09-10-terminal-close-sighup.md) | 中文

## Problem

关闭终端标签页时界面每次都恰好冻结三秒。`Session.Close` 先发 `SIGTERM`，再关 PTY 主端，等待最多三秒，最后落到 `SIGKILL` 兜底。交互式 bash 按作业控制惯例忽略 `SIGTERM`，宽限期必然耗满。为玲珑沙箱移除 `Setpgid` 的提交（沙箱禁止该调用——`fork/exec: operation not permitted`）假设关闭 PTY 主端会让内核向子进程广播 `SIGHUP`，但该广播只送达*控制终端*的前台进程组；没有 `Setpgid`/`Setsid`，子进程从未建立这层关系，信号永远不会发生。实测真实路径：空闲交互式 bash 耗满 3.002 秒才被 `SIGKILL`，而非交互式 bash 收到 `SIGTERM` 约 200 微秒即退。前端在重绘前会 `await TerminalClose`，于是每次删标签都吞下整个等待。

## Decision

`Session.Close` 的首信号改为 `SIGHUP`。HUP 是终端关闭的语义信号，交互式 shell 不忽略它，且经直接 `kill(2)` 送达——不需要进程组，也不需要控制终端关系，玲珑限制继续满足。PTY 主端关闭、三秒宽限期与 `SIGKILL` 兜底保留给病态场景；健康路径实测亚毫秒（空闲 bash 约 0.9 毫秒，带前台子进程约 0.3 毫秒）。回归测试 spawn 交互式 bash 并在 `Close` 超过两秒时失败，终端套件其余测试全部通过。

## Alternatives considered

**用 `SysProcAttr` 的 `Setsid` + `Setctty` 让子进程把 PTY 设为控制终端。** 这样主端关闭时内核的 `SIGHUP` 广播就能送达。否决：这正是玲珑已经禁止 `Setpgid` 的同一类进程属性操作，会重新引入那个提交修掉的启动失败。

**前端不等后端、先更新界面再关闭。** 把等待藏起来而不是修掉；僵尸 shell 及其作业会在后端滞留三秒，立刻重开终端的用户可能撞上仍在终止中的会话。

## Consequences

删标签在感知上即时完成，shell 清理遵循终端语义：bash 收到 HUP 即退出并向运行中的作业转发，前台子进程由 shell 自行清理。忽略 HUP 的进程仍走宽限期加 `SIGKILL`，行为未变。未来任何恢复进程组属性的尝试都必须重新面对这里记录的玲珑 `setpgid` 禁令，而不是在启动失败时重新发现它。
