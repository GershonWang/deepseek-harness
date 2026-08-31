package toolchain

import (
	"os"
	"path/filepath"
	"sort"
)

// ToolVersion 描述一个工具的某个具体版本。
type ToolVersion struct {
	Version string `json:"version"`
	URL     string `json:"url"`
	SHA256  string `json:"sha256"`
	Size    int64  `json:"size,omitempty"` // 字节数，可选
	BinRel  string `json:"bin_rel"`        // 包内二进制目录相对路径
	LibRel  string `json:"lib_rel"`        // 包内库目录相对路径（可选，进 LD_LIBRARY_PATH）
}

// Tool 描述一个可安装的工具链。
type Tool struct {
	ID           string        `json:"id"`           // 唯一标识：安装目录名与 current 软链名
	Name         string        `json:"name"`         // 界面显示名
	Category     string        `json:"category"`     // 分类：compiler / language-sdk / modern-cli / debug / code-quality
	Description  string        `json:"description"`  // 简短描述
	Provides     []string      `json:"provides"`     // 提供的命令名（用于冲突检测）
	Dependencies []string      `json:"dependencies"` // 依赖的工具 ID 列表（单层依赖）
	Versions     []ToolVersion `json:"versions"`     // 可用版本列表，第一个为推荐版本
}

// LatestVersion 返回推荐（第一个）版本。
func (t Tool) LatestVersion() ToolVersion {
	if len(t.Versions) == 0 {
		return ToolVersion{}
	}
	return t.Versions[0]
}

// FindVersion 按版本号查找，未找到返回 false。
func (t Tool) FindVersion(ver string) (ToolVersion, bool) {
	for _, v := range t.Versions {
		if v.Version == ver {
			return v, true
		}
	}
	return ToolVersion{}, false
}

// ToolBundle 工具集：一组可一键安装的工具组合。
type ToolBundle struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	ToolIDs     []string `json:"tool_ids"`
}

// catalogData 内置工具清单（单源）。
// 真实项目中这个列表由 tools.yaml 构建生成，这里先用代码写死。
var builtinTools = []Tool{
	{
		ID:           "go",
		Name:         "Go",
		Category:     "language-sdk",
		Description:  "Go 编程语言工具链",
		Provides:     []string{"go", "gofmt"},
		Dependencies: []string{},
		Versions: []ToolVersion{
			{
				Version: "1.23.2",
				URL:     "https://go.dev/dl/go1.23.2.linux-amd64.tar.gz",
				SHA256:  "542d3c1705f1c6a1c5a80d5dc62e2e45171af291e755d591c5e6531ef63b454e",
				BinRel:  "bin",
			},
		},
	},
	{
		ID:           "jdk21",
		Name:         "JDK 21 (Temurin)",
		Category:     "language-sdk",
		Description:  "Eclipse Temurin OpenJDK 21",
		Provides:     []string{"java", "javac", "jdb", "jar"},
		Dependencies: []string{},
		Versions: []ToolVersion{
			{
				Version: "21.0.12.1",
				URL:     "https://github.com/adoptium/temurin21-binaries/releases/download/jdk-21.0.12.1%2B1/OpenJDK21U-jdk_x64_linux_hotspot_21.0.12.1_1.tar.gz",
				SHA256:  "ce79869e1307ed8ee1e2baa86a412b1eb5b75d10a01006d788a6f968bcfaee94",
				BinRel:  "bin",
				LibRel:  "lib",
			},
		},
	},
	{
		ID:           "ripgrep",
		Name:         "ripgrep (rg)",
		Category:     "modern-cli",
		Description:  "更快的 grep，递归搜索目录",
		Provides:     []string{"rg"},
		Dependencies: []string{},
		Versions: []ToolVersion{
			{
				Version: "14.1.0",
				URL:     "https://github.com/BurntSushi/ripgrep/releases/download/14.1.0/ripgrep-14.1.0-x86_64-unknown-linux-musl.tar.gz",
				SHA256:  "f84757b07f425fe5cf11d87df6644691c644a5cd2348a2c670894272999d3ba7",
				BinRel:  ".",
			},
		},
	},
	{
		ID:           "uv",
		Name:         "uv",
		Category:     "modern-cli",
		Description:  "极快的 Python 包管理和 Python 版本管理工具",
		Provides:     []string{"uv"},
		Dependencies: []string{},
		Versions: []ToolVersion{
			{
				Version: "0.12.6",
				URL:     "https://github.com/astral-sh/uv/releases/download/0.12.6/uv-x86_64-unknown-linux-gnu.tar.gz",
				SHA256:  "8681d8921e7d520fb368991dcf5f9c1905b80f5bf2a265a0ed085c8d8e342477",
				BinRel:  ".",
			},
		},
	},
}

var builtinBundles = []ToolBundle{
	{
		ID:          "modern-cli",
		Name:        "现代 CLI 套装",
		Description: "fd + bat + ripgrep + sd + fzf + eza",
		ToolIDs:     []string{"ripgrep", "uv"},
	},
}

// Catalog 返回内置工具清单。
// 后续可被远程索引覆盖（remote.go）。
func Catalog() []Tool {
	return builtinTools
}

// Bundles 返回内置工具集。
func Bundles() []ToolBundle {
	return builtinBundles
}

// LookupTool 按 ID 查找工具；未命中返回 false。
func LookupTool(id string) (Tool, bool) {
	for _, t := range builtinTools {
		if t.ID == id {
			return t, true
		}
	}
	return Tool{}, false
}

// LookupBundle 按 ID 查找工具集。
func LookupBundle(id string) (ToolBundle, bool) {
	for _, b := range builtinBundles {
		if b.ID == id {
			return b, true
		}
	}
	return ToolBundle{}, false
}

// ToolsByCategory 按分类分组返回工具 ID 列表。
func ToolsByCategory() map[string][]string {
	m := map[string][]string{}
	for _, t := range builtinTools {
		m[t.Category] = append(m[t.Category], t.ID)
	}
	return m
}

// —— 安装与软链 ——

// versionDir 返回 <dir>/<id>-<version>。
func versionDir(dir, id, version string) string {
	return filepath.Join(dir, id+"-"+version)
}

// currentLink 返回 <dir>/current/<id>。
func currentLink(dir, id string) string {
	return filepath.Join(dir, "current", id)
}

// cacheDir 返回下载缓存目录。
func cacheDir(dir string) string {
	return filepath.Join(dir, "cache")
}

// ListInstalled 返回当前已激活的工具 ID 列表（按 current 软链）。
func ListInstalled(dir string) []string {
	cur := filepath.Join(dir, "current")
	entries, err := os.ReadDir(cur)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names
}

// ListVersions 列出某个工具所有已安装的版本号（按字母序）。
func ListVersions(dir, id string) []string {
	prefix := id + "-"
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var vers []string
	for _, e := range entries {
		name := e.Name()
		if len(name) > len(prefix) && name[:len(prefix)] == prefix {
			vers = append(vers, name[len(prefix):])
		}
	}
	sort.Strings(vers)
	return vers
}

// ActiveVersion 返回某个工具的激活版本，未安装返回空。
func ActiveVersion(dir, id string) string {
	target, err := os.Readlink(currentLink(dir, id))
	if err != nil {
		return ""
	}
	base := filepath.Base(target)
	prefix := id + "-"
	if len(base) > len(prefix) && base[:len(prefix)] == prefix {
		return base[len(prefix):]
	}
	return ""
}

// IsInstalled 检查某个工具的某个版本是否已安装。
func IsInstalled(dir, id, version string) bool {
	_, err := os.Stat(versionDir(dir, id, version))
	return err == nil
}

// SetActiveVersion 切换激活版本。目标版本必须已安装。
func SetActiveVersion(dir, id, version string) error {
	if !IsInstalled(dir, id, version) {
		return os.ErrNotExist
	}
	link := currentLink(dir, id)
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		return err
	}
	_ = os.Remove(link)
	if err := os.Symlink(versionDir(dir, id, version), link); err != nil {
		return err
	}
	// 切换后重建 bin/ 软链
	return ReconcileBinLinks(dir)
}

// Uninstall 卸载指定版本。如果卸载的是当前激活版本，自动激活最新的剩余版本。
func Uninstall(dir, id, version string) error {
	if !IsInstalled(dir, id, version) {
		return os.ErrNotExist
	}
	active := ActiveVersion(dir, id)

	// 先移除 current 软链（如果是激活版本）
	if active == version {
		_ = os.Remove(currentLink(dir, id))
	}

	// 删除版本目录
	if err := os.RemoveAll(versionDir(dir, id, version)); err != nil {
		return err
	}

	// 如果卸载的是激活版本，尝试激活最新的剩余版本
	if active == version {
		vers := ListVersions(dir, id)
		if len(vers) > 0 {
			// 激活最后一个（字母序最新）
			latest := vers[len(vers)-1]
			if err := SetActiveVersion(dir, id, latest); err != nil {
				return err
			}
		}
	}

	// 重建软链
	return ReconcileBinLinks(dir)
}

// —— 软链与自愈 ——

// ReconcileBinLinks 自愈：扫描 <dir>/current 下已装工具，在 <dir>/bin 重建其
// 可执行文件软链，在 <dir>/lib 重建库目录绑定（如有 LibRel），并清理失效软链。
// 启动时调用，保证重装、更新、HOME 迁移后工具链仍自动可用。
func ReconcileBinLinks(dir string) error {
	cur := filepath.Join(dir, "current")
	entries, err := os.ReadDir(cur)
	if err != nil {
		return nil // 尚未安装任何工具
	}

	linkDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(linkDir, 0o755); err != nil {
		return err
	}
	libDir := filepath.Join(dir, "lib")
	if err := os.MkdirAll(libDir, 0o755); err != nil {
		return err
	}

	seenBins := map[string]bool{}
	seenLibs := map[string]bool{}

	for _, e := range entries {
		if e.Type().IsRegular() {
			continue
		}
		root := filepath.Join(cur, e.Name())
		if e.Type()&os.ModeSymlink != 0 {
			if t, err := os.Readlink(root); err == nil {
				if !filepath.IsAbs(t) {
					t = filepath.Join(cur, t)
				}
				root = t
			}
		}

		// 尝试已知布局：bin/ 子目录
		if info, err := os.Stat(filepath.Join(root, "bin")); err == nil && info.IsDir() {
			linkExecutables(filepath.Join(root, "bin"), linkDir, seenBins)
		} else if info, err := os.Stat(root); err == nil && info.IsDir() {
			// 根目录直接含可执行
			linkExecutables(root, linkDir, seenBins)
		}

		// 库目录 lib/
		if info, err := os.Stat(filepath.Join(root, "lib")); err == nil && info.IsDir() {
			bindLibDir := filepath.Join(libDir, e.Name())
			_ = os.Remove(bindLibDir)
			if err := os.Symlink(filepath.Join(root, "lib"), bindLibDir); err == nil {
				seenLibs[e.Name()] = true
			}
		}
		if info, err := os.Stat(filepath.Join(root, "lib64")); err == nil && info.IsDir() {
			bindLibDir := filepath.Join(libDir, e.Name())
			_ = os.Remove(bindLibDir)
			if err := os.Symlink(filepath.Join(root, "lib64"), bindLibDir); err == nil {
				seenLibs[e.Name()] = true
			}
		}
	}

	cleanStaleLinks(linkDir, seenBins)
	cleanStaleLinks(libDir, seenLibs)
	return nil
}

// linkExecutables 把 src 下所有可执行文件软链进 linkDir，并记入 seen。
func linkExecutables(src, linkDir string, seen map[string]bool) {
	entries, err := os.ReadDir(src)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		if fi.Mode()&0o111 == 0 {
			continue
		}
		name := e.Name()
		_ = os.Remove(filepath.Join(linkDir, name))
		if err := os.Symlink(filepath.Join(src, name), filepath.Join(linkDir, name)); err == nil {
			seen[name] = true
		}
	}
}

// cleanStaleLinks 删除 dir 里不再有效的软链。
func cleanStaleLinks(dir string, seen map[string]bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if seen[e.Name()] {
			continue
		}
		if e.Type()&os.ModeSymlink != 0 {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

// —— 冲突检测 ——

// sortedBins 返回目录里所有可执行命令名（用于冲突检测），已排序。
func sortedBins(binDir string) []string {
	entries, err := os.ReadDir(binDir)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		if fi.Mode()&0o111 != 0 {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names
}

// Conflicts 返回两目录里同名的可执行命令（宿主挂载 vs 按需安装冲突检测用）。
func Conflicts(dirA, dirB string) []string {
	set := map[string]bool{}
	for _, n := range sortedBins(dirB) {
		set[n] = true
	}
	var out []string
	for _, n := range sortedBins(dirA) {
		if set[n] {
			out = append(out, n)
		}
	}
	return out
}

// —— 状态组装（给 UI 用） ——

// ToolStatus 是工具的运行时状态，供 UI 渲染。
type ToolStatus struct {
	ID                string   `json:"id"`
	Name              string   `json:"name"`
	Category          string   `json:"category"`
	Description       string   `json:"description"`
	Provides          []string `json:"provides"`
	Dependencies      []string `json:"dependencies"`
	AvailableVersion  string   `json:"available_version"` // 推荐版本
	Installed         bool     `json:"installed"`
	ActiveVersion     string   `json:"active_version"`     // 当前激活版本
	InstalledVersions []string `json:"installed_versions"` // 所有已装版本
	Size              int64    `json:"size"`               // 字节
}

// ToolStatuses 组装所有工具的状态列表。
func ToolStatuses(dir string) []ToolStatus {
	out := make([]ToolStatus, 0, len(builtinTools))
	for _, t := range builtinTools {
		ts := ToolStatus{
			ID:                t.ID,
			Name:              t.Name,
			Category:          t.Category,
			Description:       t.Description,
			Provides:          t.Provides,
			Dependencies:      t.Dependencies,
			AvailableVersion:  t.LatestVersion().Version,
			Size:              t.LatestVersion().Size,
			InstalledVersions: ListVersions(dir, t.ID),
			ActiveVersion:     ActiveVersion(dir, t.ID),
		}
		ts.Installed = len(ts.InstalledVersions) > 0
		out = append(out, ts)
	}
	return out
}
