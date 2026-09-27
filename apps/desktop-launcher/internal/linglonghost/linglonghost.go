// Package linglonghost 让宿主机的玲珑命令行工具在沙箱容器内可用。
//
// 背景：玲珑容器隔离宿主与容器，容器里既没有 ll-builder/ll-cli，也没有宿主的玲珑
// 守护进程、层仓库（宿主 /var/lib/linglong）与可写状态目录（容器内 ~/.linglong 只读）。
// 把这些命令装进容器或工具链市场都解决不了这种耦合：实测容器内直调宿主二进制只能
// 打印 --version，`ll-cli list` 静默退出 255。客户端为此已声明宿主逃逸通道（appenv 的
// DSH_HOST_ROOTFS/DSH_HOST_LAUNCH 一对变量，harness 侧由 hostEscapeOf() 消费）。
//
// 本包只做一件事：把该通道包装成命令放进 ~/.dsh-linglong/bin，由
// appenv.ConfigureChildEnv 注入子进程 PATH，模型用既有的 Bash 工具即可构建与打包。
// 包装是形态固定的 shell 透传，因此不需要新增"在宿主执行任意命令"的能力：容器内只有
// 白名单里的命令与子命令能到达宿主，通道的能力面没有扩大。
package linglonghost

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// dirName 是包装目录在用户家目录下的名字。
const dirName = ".dsh-linglong"

// binDirName 是包装目录下供 PATH 使用的子目录名。
const binDirName = "bin"

// hostSearchDirs 是宿主上可能安放玲珑命令的目录（相对宿主根），按优先级排列。
// 容器内的探测路径是「宿主根挂载点 + 这里的目录」，而写进包装脚本的路径必须是宿主
// 自己看到的绝对路径（不带挂载点前缀）——包装在宿主上执行，宿主没有 /run/host/rootfs，
// 实测把容器内路径写进去会得到 exit 127「exec: not found」。
var hostSearchDirs = []string{"usr/bin", "usr/local/bin", "bin"}

// marker 出现在每个包装脚本里，用于区分"本包生成的文件"。
// 清理只针对带该标记的文件：用户自己放进该目录的内容一律不动。
const marker = "dsh-linglong:generated"

// Command 是一条要在容器内可用的宿主玲珑命令。
type Command struct {
	// Name 是容器内的命令名。
	Name string
	// HostPath 是它在宿主机上的绝对路径（宿主视角，不含宿主根挂载点前缀）。
	HostPath string
	// Allow 是允许透传到宿主的子命令白名单；空表示不限子命令。
	Allow []string
}

// commands 是本包包装的命令集合，顺序稳定（测试与日志依赖它）。
//
// 白名单是安全不变式，只列不改宿主状态的子命令，且失败闭合：白名单之外、或第一个
// 非选项参数之外的写法一律拒绝。需要放开某项时改这里，不要在包装脚本里另写判断。
//   - ll-builder：开发-测试-打包主链路（构建、导出、本地运行、列出已构建应用）。
//     push 会推送远程仓库、repo 会改仓库配置、clean/remove/import/extract 会改本地
//     构建仓库，都不放。
//   - ll-cli：只读查询。install/uninstall/upgrade 会改宿主已装应用，run/enter/kill
//     会在宿主启动或干预应用，prune 会删宿主的 base/runtime，repo 会改仓库配置，
//     一律拒绝。
var commands = []Command{
	{Name: "ll-builder", Allow: []string{"build", "export", "run", "list"}},
	{Name: "ll-cli", Allow: []string{"list", "info", "content", "analyze", "ps", "search"}},
}

// WrapperDir 返回包装目录（<home>/.dsh-linglong/bin）。
// 路径只在这里定义：appenv.ConfigureChildEnv 注入 PATH 时也用它，避免两处字面量漂移。
func WrapperDir(home string) string {
	return filepath.Join(home, dirName, binDirName)
}

// Detect 探测宿主根挂载点下实际可用的玲珑命令，顺序与 commands 一致。
//
// 探测路径在容器内（宿主根挂载点下），返回的 HostPath 是宿主视角的绝对路径：两者
// 必须分开，包装脚本是在宿主上跑的。不是普通文件、或没有执行位的条目会被跳过：
// 能力"不存在"好过"看得见但一用就报错"。
func Detect(hostRootfs string) []Command {
	var out []Command
	for _, c := range commands {
		for _, dir := range hostSearchDirs {
			info, err := os.Stat(filepath.Join(hostRootfs, dir, c.Name))
			if err != nil || !info.Mode().IsRegular() || info.Mode()&0o111 == 0 {
				continue
			}
			c.HostPath = filepath.Join(string(filepath.Separator), dir, c.Name)
			out = append(out, c)
			break
		}
	}
	return out
}

// Ensure 让包装目录与当前探测结果一致，返回可用命令数。
//
// 可用：写入包装脚本；内容与目标一致时不重写，避免每次启动都改 mtime 造成无意义的
// 状态漂移。不可用：只删除带 marker 的旧包装，目录因此变空时连目录一起删掉，使
// ConfigureChildEnv 不再把它注入 PATH——否则模型会对着一个空目录里的命令反复失败。
func Ensure(home, hostRootfs string) (int, error) {
	dir := WrapperDir(home)
	available := Detect(hostRootfs)
	if len(available) == 0 {
		return 0, pruneGenerated(dir)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, err
	}
	keep := make(map[string]bool, len(available))
	for _, c := range available {
		keep[c.Name] = true
		if err := writeIfChanged(filepath.Join(dir, c.Name), script(c)); err != nil {
			return 0, err
		}
	}
	if err := pruneGeneratedExcept(dir, keep); err != nil {
		return len(available), err
	}
	return len(available), nil
}

// script 生成一条命令的包装脚本。
//
// 形态固定：systemd-run 把命令送到宿主执行；--working-directory 让宿主的工作目录与
// 容器内一致（工作区在两侧是同一路径）；--wait 会把宿主的退出码原样带回（实测 7/0
// 透传、宿主缺命令返回 1），因此不需要任何跨命名空间的握手文件——容器内
// /run/user/1000 是只读的，本来也写不了。不加 --unit：PID 复用时可能与残留 unit 撞名，
// 让 systemd 自己命名更稳，审计靠 harness 的命令记录与本包装文件本身。
//
// `sh -c 'exec "$0" "$@"' '<host path>' "$@"` 用 $0 承载宿主命令路径、$@ 承载参数，
// 避免在 shell 里拼引号，带空格或非 ASCII 的参数也原样透传。宿主路径用单引号包裹：
// 它由固定的挂载点与命令名拼成，不含单引号。
func script(c Command) string {
	var b strings.Builder
	fmt.Fprintf(&b, "#!/bin/sh\n")
	fmt.Fprintf(&b, "# %s 由 DeepSeek Harness 桌面客户端生成，请勿手改：\n", marker)
	fmt.Fprintf(&b, "# 客户端每次启动都会按宿主探测结果重写或删除本文件。\n")
	fmt.Fprintf(&b, "# 容器内调用 %s 会在宿主机上执行——玲珑守护进程、层仓库与可写状态都只在宿主侧。\n", c.Name)
	b.WriteString("set -u\n")
	if len(c.Allow) > 0 {
		fmt.Fprintf(&b, "# 子命令白名单：取第一个非选项参数（%s --json list 这类全局选项在前也认）。\n", c.Name)
		b.WriteString("sub=\"\"\n")
		b.WriteString("for arg in \"$@\"; do case \"$arg\" in -*) continue ;; *) sub=\"$arg\"; break ;; esac; done\n")
		fmt.Fprintf(&b, "case \"$sub\" in\n  %s|\"\") ;;\n", strings.Join(c.Allow, "|"))
		fmt.Fprintf(&b, "  *) echo \"%s：容器内只允许 %s；『$sub』被拒绝，需要时请在宿主机上执行\" >&2; exit 2 ;;\n",
			c.Name, strings.Join(c.Allow, "/"))
		b.WriteString("esac\n")
	}
	b.WriteString("exec systemd-run --user --quiet --collect --wait --pipe \\\n")
	b.WriteString("  --working-directory=\"$(pwd -P)\" \\\n")
	fmt.Fprintf(&b, "  /bin/sh -c 'exec \"$0\" \"$@\"' '%s' \"$@\"\n", c.HostPath)
	return b.String()
}

// writeIfChanged 原子写入包装脚本：内容一致时直接返回，否则先写临时文件再 rename，
// 避免有进程读到写了一半的脚本。权限固定 0755（包装必须可执行）。
func writeIfChanged(path, content string) error {
	if old, err := os.ReadFile(path); err == nil && string(old) == content {
		return nil
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o755); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// pruneGenerated 删除目录下所有本包生成的包装；目录随之变空时一并删除。
// 父目录（~/.dsh-linglong）由 Ensure 创建，也同样只在空时删除：留下一个空壳会让
// "有没有宿主玲珑"这件事变得看不出答案。
func pruneGenerated(dir string) error {
	if err := pruneGeneratedExcept(dir, nil); err != nil {
		return err
	}
	// 目录里还有用户自己的东西时 Remove 会失败，保留目录正是想要的。
	if err := os.Remove(dir); err != nil {
		return nil
	}
	_ = os.Remove(filepath.Dir(dir))
	return nil
}

// pruneGeneratedExcept 删除目录下本包生成、但不在 keep 里的文件。
func pruneGeneratedExcept(dir string, keep map[string]bool) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil // 目录不存在即无需清理
	}
	for _, e := range entries {
		if e.IsDir() || keep[e.Name()] {
			continue
		}
		path := filepath.Join(dir, e.Name())
		// 判据是文件内容里的 marker，不是文件名：同名文件可能是用户自己放的。
		data, err := os.ReadFile(path)
		if err != nil || !strings.Contains(string(data), marker) {
			continue
		}
		if err := os.Remove(path); err != nil {
			return err
		}
	}
	return nil
}
