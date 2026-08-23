package toolchain

import (
	"os"
	"path/filepath"
	"sort"
)

// CatalogItem 声明一个可一键安装的工具链。
// url/sha256 从官方发布页取真实值并在此固定；sha256 为空表示尚未填实、
// 安装会被拒绝（沿用 tools.yaml 的"启用前填入"约定）。
type CatalogItem struct {
	Name    string // 标识：安装目录名与 current 软链名
	Label   string // 界面显示名
	Version string
	URL     string
	SHA256  string
	BinRel  string // 安装根内二进制所在相对目录（"." 或 "bin"）
}

// Catalog 返回内置工具链清单（只含容器里没有、且官方提供便携 tar.gz 的工具）。
// 容器内已内置的 node/git/python/curl 等不重复收录。
func Catalog() []CatalogItem {
	return []CatalogItem{
		{
			Name:    "jdk21",
			Label:   "JDK 21 (Temurin)",
			Version: "21.0.12.1",
			URL:     "https://github.com/adoptium/temurin21-binaries/releases/download/jdk-21.0.12.1%2B1/OpenJDK21U-jdk_x64_linux_hotspot_21.0.12.1_1.tar.gz",
			SHA256:  "ce79869e1307ed8ee1e2baa86a412b1eb5b75d10a01006d788a6f968bcfaee94",
			BinRel:  "bin",
		},
		{
			Name:    "go",
			Label:   "Go",
			Version: "1.23.2",
			URL:     "https://go.dev/dl/go1.23.2.linux-amd64.tar.gz",
			SHA256:  "542d3c1705f1c6a1c5a80d5dc62e2e45171af291e755d591c5e6531ef63b454e",
			BinRel:  "bin",
		},
		{
			Name:    "ripgrep",
			Label:   "ripgrep",
			Version: "14.1.0",
			URL:     "https://github.com/BurntSushi/ripgrep/releases/download/14.1.0/ripgrep-14.1.0-x86_64-unknown-linux-musl.tar.gz",
			SHA256:  "f84757b07f425fe5cf11d87df6644691c644a5cd2348a2c670894272999d3ba7",
			BinRel:  ".",
		},
	}
}

// Lookup 按名称查找清单项；未命中返回 false。
func Lookup(name string) (CatalogItem, bool) {
	for _, it := range Catalog() {
		if it.Name == name {
			return it, true
		}
	}
	return CatalogItem{}, false
}

// InstallFromCatalog 安装清单项并把它可执行文件软链进 <dir>/bin（进入 PATH）。
func InstallFromCatalog(dir string, it CatalogItem) error {
	if it.SHA256 == "" {
		return errCatalogUnpinned{item: it}
	}
	if err := Install(dir, it.Name, it.Version, it.URL, it.SHA256); err != nil {
		return err
	}
	return LinkBin(dir, it)
}

// LinkBin 把已安装工具 <dir>/current/<name>/<BinRel> 下的可执行文件软链到
// <dir>/bin，使这些命令进入 PATH（dshToolsEnv 已把 <dir>/bin 前置）。
func LinkBin(dir string, it CatalogItem) error {
	src := filepath.Join(dir, "current", it.Name, it.BinRel)
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	linkDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(linkDir, 0o755); err != nil {
		return err
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
		_ = os.Remove(filepath.Join(linkDir, e.Name()))
		if err := os.Symlink(filepath.Join(src, e.Name()), filepath.Join(linkDir, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

// CatalogStatuses 组装设置弹框的清单状态：已安装工具的版本来自 current 软链
// 目标目录名（<name>-<version>）。
func CatalogStatuses(dir string) []CatalogStatus {
	installed := ListInstalled(dir)
	have := map[string]bool{}
	for _, n := range installed {
		have[n] = true
	}
	out := make([]CatalogStatus, 0, len(Catalog()))
	for _, it := range Catalog() {
		cs := CatalogStatus{Name: it.Name, Label: it.Label, Version: it.Version, State: "installable", Pinned: it.SHA256 != ""}
		if have[it.Name] {
			cs.State = "installed"
			cs.InstalledVersion = installedVersion(dir, it.Name)
		}
		out = append(out, cs)
	}
	return out
}

// installedVersion 从 current 软链目标目录名提取已装版本（"go-1.23.2" -> "1.23.2"）。
func installedVersion(dir, name string) string {
	target, err := os.Readlink(filepath.Join(dir, "current", name))
	if err != nil {
		return ""
	}
	base := filepath.Base(target)
	prefix := name + "-"
	if len(base) > len(prefix) && base[:len(prefix)] == prefix {
		return base[len(prefix):]
	}
	return base
}

// CatalogStatus 是设置弹框清单项的渲染状态。
type CatalogStatus struct {
	Name             string
	Label            string
	Version          string
	InstalledVersion string
	State            string // "installed" | "installable"
	Pinned           bool   // sha256 已填实、可安装
}

// errCatalogUnpinned 表示清单项 sha256 未填实，安装被拒绝。
type errCatalogUnpinned struct {
	item CatalogItem
}

func (e errCatalogUnpinned) Error() string {
	return "工具链 " + e.item.Name + " 的 sha256 尚未填实（从官方发布页取真实值后填入 catalog）"
}

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
