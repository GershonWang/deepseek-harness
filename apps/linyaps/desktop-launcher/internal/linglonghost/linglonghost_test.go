package linglonghost

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fakeHostRootfs 造一个假的宿主根挂载点，只放指定的宿主命令。
func fakeHostRootfs(t *testing.T, names ...string) string {
	t.Helper()
	root := t.TempDir()
	binDir := filepath.Join(root, hostSearchDirs[0])
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(binDir, name), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// hostPathOf 返回某命令按宿主视角应有的绝对路径（测试与生产用同一套拼接规则）。
func hostPathOf(dir, name string) string {
	return filepath.Join(string(filepath.Separator), dir, name)
}

// fakeLauncher 造一个假 systemd-run：把收到的参数逐行写进 $DSH_TEST_RECORD。
// 包装脚本的透传契约因此可观察，且不会真的在宿主上执行任何东西。
func fakeLauncher(t *testing.T) (binDir, record string) {
	t.Helper()
	binDir = t.TempDir()
	record = filepath.Join(t.TempDir(), "args")
	body := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$DSH_TEST_RECORD\"\n"
	if err := os.WriteFile(filepath.Join(binDir, "systemd-run"), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return binDir, record
}

// runWrapper 执行包装脚本，返回合并输出与错误。
func runWrapper(t *testing.T, home, binDir, record string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("sh", filepath.Join(WrapperDir(home), args[0]))
	cmd.Args = append(cmd.Args, args[1:]...)
	cmd.Env = append(os.Environ(), "PATH="+binDir, "DSH_TEST_RECORD="+record)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestDetect_SkipsMissingNonRegularAndNonExecutable(t *testing.T) {
	root := fakeHostRootfs(t, "ll-builder")
	binDir := filepath.Join(root, hostSearchDirs[0])
	// 同名目录与非可执行文件都必须被跳过。
	if err := os.MkdirAll(filepath.Join(binDir, "ll-cli"), 0o755); err != nil {
		t.Fatal(err)
	}
	got := Detect(root)
	if len(got) != 1 || got[0].Name != "ll-builder" {
		t.Fatalf("Detect = %+v, 预期只认 ll-builder", got)
	}
	// 关键区别：探测在容器内的挂载点下，写进包装的必须是宿主视角的路径——
	// 宿主上没有 /run/host/rootfs，写容器路径会让命令在宿主侧 exec 失败。
	if want := hostPathOf(hostSearchDirs[0], "ll-builder"); got[0].HostPath != want {
		t.Fatalf("HostPath = %q, 预期宿主路径 %q", got[0].HostPath, want)
	}
	// 宿主没有玲珑时探测结果为空（非玲珑环境不产生任何包装）。
	if empty := Detect(t.TempDir()); len(empty) != 0 {
		t.Fatalf("空宿主根应探测为空, got %+v", empty)
	}
	// 非首选目录里的命令同样要能找到（发行版可能装在 /usr/local/bin）。
	alt := t.TempDir()
	altDir := filepath.Join(alt, hostSearchDirs[1])
	if err := os.MkdirAll(altDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(altDir, "ll-cli"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	altGot := Detect(alt)
	if len(altGot) != 1 || altGot[0].HostPath != hostPathOf(hostSearchDirs[1], "ll-cli") {
		t.Fatalf("Detect(备选目录) = %+v, 预期命中 %s", altGot, hostSearchDirs[1])
	}
}

func TestEnsure_WritesWrappersAndKeepsThemStable(t *testing.T) {
	home := t.TempDir()
	root := fakeHostRootfs(t, "ll-builder", "ll-cli")
	n, err := Ensure(home, root)
	if err != nil || n != 2 {
		t.Fatalf("Ensure = (%d, %v), 预期 (2, nil)", n, err)
	}
	for _, tc := range []struct {
		name       string
		wantsGuard bool
	}{
		{"ll-builder", true},
		{"ll-cli", true},
	} {
		path := filepath.Join(WrapperDir(home), tc.name)
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("%s 未生成: %v", tc.name, err)
		}
		if info.Mode().Perm() != 0o755 {
			t.Fatalf("%s 权限 = %v, 预期 0755", tc.name, info.Mode().Perm())
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		text := string(data)
		for _, want := range []string{marker, "systemd-run", "--wait", "--pipe", `"$(pwd -P)"`,
			hostPathOf(hostSearchDirs[0], tc.name)} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s 包装缺少 %q:\n%s", tc.name, want, text)
			}
		}
		// 容器内的探测路径绝不能进包装：宿主上不存在那条路径。
		if strings.Contains(text, root) {
			t.Fatalf("%s 包装里出现了容器内路径 %q:\n%s", tc.name, root, text)
		}
		if got := strings.Contains(text, "case \"$sub\" in"); got != tc.wantsGuard {
			t.Fatalf("%s 子命令白名单存在性 = %v, 预期 %v", tc.name, got, tc.wantsGuard)
		}
	}
	// 第二次调用内容一致，不重写（mtime 不变）。
	path := filepath.Join(WrapperDir(home), "ll-cli")
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := Ensure(home, root); err != nil || n != 2 {
		t.Fatalf("重复 Ensure = (%d, %v), 预期 (2, nil)", n, err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("内容一致时不应重写: mtime %v -> %v", before.ModTime(), after.ModTime())
	}
}

func TestEnsure_PrunesStaleWrappersAndEmptyDir(t *testing.T) {
	home := t.TempDir()
	root := fakeHostRootfs(t, "ll-builder", "ll-cli")
	if _, err := Ensure(home, root); err != nil {
		t.Fatal(err)
	}
	// 宿主玲珑消失（卸载/换机器）：旧包装必须清掉，目录也不能留下。
	n, err := Ensure(home, t.TempDir())
	if err != nil || n != 0 {
		t.Fatalf("Ensure = (%d, %v), 预期 (0, nil)", n, err)
	}
	if _, err := os.Stat(filepath.Join(home, dirName)); !os.IsNotExist(err) {
		t.Fatalf("包装目录应被删除, stat err = %v", err)
	}
}

func TestEnsure_KeepsForeignFilesInWrapperDir(t *testing.T) {
	home := t.TempDir()
	root := fakeHostRootfs(t, "ll-builder")
	if _, err := Ensure(home, root); err != nil {
		t.Fatal(err)
	}
	// 用户自己的同名/其它文件不带 marker，清理必须无视它们。
	foreign := filepath.Join(WrapperDir(home), "mine")
	if err := os.WriteFile(foreign, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	impostor := filepath.Join(WrapperDir(home), "ll-builder")
	if err := os.WriteFile(impostor, []byte("用户自己写的 ll-builder\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Ensure(home, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{foreign, impostor} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("不该删除非本包生成的文件 %s: %v", path, err)
		}
	}
}

func TestWrapper_RejectsSubcommandOutsideWhitelist(t *testing.T) {
	home := t.TempDir()
	root := fakeHostRootfs(t, "ll-builder", "ll-cli")
	if _, err := Ensure(home, root); err != nil {
		t.Fatal(err)
	}
	binDir, record := fakeLauncher(t)
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"ll-cli", []string{"ll-cli", "install", "x.uab"}},
		{"ll-cli", []string{"ll-cli", "--json", "upgrade"}},
		{"ll-cli", []string{"ll-cli", "repo", "add", "https://example.invalid"}},
		{"ll-builder", []string{"ll-builder", "push"}},
		{"ll-builder", []string{"ll-builder", "clean"}},
	} {
		out, err := runWrapper(t, home, binDir, record, tc.args...)
		if err == nil {
			t.Fatalf("%v 应被拒绝, 输出: %s", tc.args, out)
		}
		if !strings.Contains(out, "容器内只允许") {
			t.Fatalf("%v 的拒绝信息缺少白名单说明: %s", tc.args, out)
		}
		// 拒绝必须发生在触达宿主之前。
		if _, err := os.Stat(record); err == nil {
			t.Fatalf("%v 在被拒绝前就调用了 systemd-run", tc.args)
		}
	}
}

func TestWrapper_PassesAllowedCallsToHostLauncher(t *testing.T) {
	home := t.TempDir()
	root := fakeHostRootfs(t, "ll-builder", "ll-cli")
	if _, err := Ensure(home, root); err != nil {
		t.Fatal(err)
	}
	binDir, record := fakeLauncher(t)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"ll-cli", "list"},
		{"ll-cli", "--json", "info", "com.deepseek.dsh-desktop"},
		{"ll-cli", "info", "含 空格 的 id"},
		{"ll-cli", "--version"},
		{"ll-builder", "build", "-f", "linglong.yaml"},
	} {
		if err := os.Remove(record); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		out, err := runWrapper(t, home, binDir, record, args...)
		if err != nil {
			t.Fatalf("%v 应被放行, 输出: %s", args, out)
		}
		data, err := os.ReadFile(record)
		if err != nil {
			t.Fatalf("%v 未触达 systemd-run: %v", args, err)
		}
		lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
		joined := strings.Join(lines, " ")
		for _, want := range []string{"--user", "--quiet", "--collect", "--wait", "--pipe",
			"--working-directory=" + cwd, hostPathOf(hostSearchDirs[0], args[0])} {
			if !strings.Contains(joined, want) {
				t.Fatalf("%v 的 systemd-run 参数缺少 %q: %v", args, want, lines)
			}
		}
		// 用户参数必须逐字、逐个抵达（含空格的那个也要保持一个参数）。
		want := args[1:]
		got := lines[len(lines)-len(want):]
		if len(want) == 0 {
			continue
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("%v 参数透传 = %v, 预期尾部为 %v", args, got, want)
			}
		}
	}
	if _, err := os.Stat(record); err != nil {
		t.Fatalf("最后一次放行未触达 systemd-run: %v", err)
	}
}

// TestWrapper_RealHostLinglongInsideContainer 是唯一一条触碰真实宿主的用例：它只在
// 玲珑容器内成立（宿主根挂载点与宿主逃逸启动器都在），在真实的宿主上执行
// ll-builder/ll-cli，验证"生成包装 → 宿主执行 → 白名单拒绝"这条链在真实环境里闭环。
// 容器外（例如在宿主上直接跑 go test）或宿主没装玲珑时自动跳过，不构成测试负担。
func TestWrapper_RealHostLinglongInsideContainer(t *testing.T) {
	const realHostRootfs = "/run/host/rootfs"
	if !dirExistsForTest(realHostRootfs) {
		t.Skip("不在玲珑容器内：没有宿主根挂载点")
	}
	if _, err := exec.LookPath("systemd-run"); err != nil {
		t.Skip("宿主逃逸启动器不在 PATH 上")
	}
	available := Detect(realHostRootfs)
	if len(available) == 0 {
		t.Skip("宿主上没有可用的玲珑命令")
	}
	// 用临时 home，不碰真实的 ~/.dsh-linglong。
	home := t.TempDir()
	if n, err := Ensure(home, realHostRootfs); err != nil || n != len(available) {
		t.Fatalf("Ensure = (%d, %v), 预期 (%d, nil)", n, err, len(available))
	}

	names := make(map[string]bool, len(available))
	for _, c := range available {
		names[c.Name] = true
	}
	run := func(name string, args ...string) (string, error) {
		out, err := exec.Command(filepath.Join(WrapperDir(home), name), args...).CombinedOutput()
		return string(out), err
	}
	if names["ll-builder"] {
		// 版本查询是最小的一次真实宿主执行：宿主缺命令时退出码会是非零。
		if out, err := run("ll-builder", "--version"); err != nil || strings.TrimSpace(out) == "" {
			t.Fatalf("ll-builder --version 未能在宿主上执行: err=%v out=%q", err, out)
		}
		// 构建类子命令必须进入放行集合。
		if out, err := run("ll-builder", "--help"); err != nil {
			t.Fatalf("ll-builder --help 应放行: err=%v out=%q", err, out)
		}
		if out, err := run("ll-builder", "push"); err == nil || !strings.Contains(out, "只允许") {
			t.Fatalf("ll-builder push 应被白名单拒绝: err=%v out=%q", err, out)
		}
	}
	if names["ll-cli"] {
		// 只读查询真的打到宿主（宿主守护进程/层仓库都只在宿主侧才有答案）。
		if out, err := run("ll-cli", "list"); err != nil {
			t.Fatalf("ll-cli list 未能在宿主上执行: err=%v out=%q", err, out)
		}
		if out, err := run("ll-cli", "install", "x.uab"); err == nil || !strings.Contains(out, "只允许") {
			t.Fatalf("ll-cli install 应被白名单拒绝: err=%v out=%q", err, out)
		}
	}
}

// dirExistsForTest 是测试内的目录判定，避免为测试把生产代码里的辅助函数导出。
func dirExistsForTest(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
