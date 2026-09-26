package toolchain

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// ToolVersion 描述一个工具的某个具体版本。
type ToolVersion struct {
	Version string `json:"version"`
	// Major 是该版本所属的大版本线，界面按它分组展示（如 JDK 的 "8"/"11"/"21"）。
	//
	// 为什么必须由清单显式声明：版本号格式跨工具差异过大，没有任何一种自动切法通用。
	// Go 的功能版本在第二段（1.26 与 1.27 该分两条线），而 Node 的功能版本在第一段
	// （24.20 与 24.21 必须同一条线）；JDK 8 用无点分隔的发行标签 `8u504`；Rust 有
	// 向后兼容承诺，1.98 与 1.99 不该被拆成两条线。切错会让界面要么把同一条线拆开、
	// 要么把不同线合并。
	//
	// 缺省（旧索引）时按 majorOf 的兜底规则取版本号首个数字段，因此旧索引照常工作。
	Major  string `json:"major,omitempty"`
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size,omitempty"` // 字节数，可选
	BinRel string `json:"bin_rel"`        // 包内二进制目录相对路径
	LibRel string `json:"lib_rel"`        // 包内库目录相对路径（可选，进 LD_LIBRARY_PATH）
	// BinDirs 多个二进制目录（相对路径），用于 bin 分散在多个子目录的发行包
	// （如 Rust：cargo/bin 与 rustc/bin）。非空时优先于 BinRel；为空走默认
	// 布局探测（bin/ 子目录，否则工具根目录）。
	BinDirs []string `json:"bin_dirs,omitempty"`
	// BinNames 覆盖归档内可执行文件的对外命令名：键是归档内文件名，值是命令名。
	// 用于发行包内二进制名与命令名不一致的场景（如 yq 归档内为 yq_linux_amd64）。
	// 值为空串表示不暴露该文件，用于发行包自带的安装/辅助脚本——它们带可执行位，
	// 默认布局探测会一并软链进 bin/，把杂散命令混进 PATH。未列出的文件沿用归档内原名。
	BinNames map[string]string `json:"bin_names,omitempty"`
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

// indexJSON 是内置工具清单（单源）：与远程索引同构，编译期嵌入。
// 有效目录初始化为它，运行时被远程索引覆盖（见 remote.go LoadIndex）。
//
//go:embed tools/index.json
var indexJSON []byte

var (
	catalogMu      sync.RWMutex
	effectiveTools []Tool
	// effectiveLabels 是当前有效索引里的分类中文标签（见 Index.CategoryLabels）。
	// 与工具清单一同被远程索引覆盖：标签属于索引数据，这样新增分类不必等客户端发版。
	effectiveLabels map[string]string
)

// init 解析嵌入的单源索引作为内置兜底；索引非法则立即失败（打包期应被
// verify 捕获，运行期 panic 属不可恢复配置错误）。
func init() {
	idx, err := ParseIndex(indexJSON)
	if err != nil {
		panic("builtin tools index is invalid: " + err.Error())
	}
	effectiveTools = idx.Tools
	effectiveLabels = idx.CategoryLabels
}

// setCatalog 用生效索引覆盖有效目录与分类标签（remote.go 调用）。
func setCatalog(tools []Tool, labels map[string]string) {
	catalogMu.Lock()
	effectiveTools = tools
	effectiveLabels = labels
	catalogMu.Unlock()
}

// CategoryLabels 返回当前有效索引声明的分类中文标签快照。索引未声明某分类时，
// 调用方自行决定回退（前端保留一张同内容的兜底表，兼顾旧索引）。
func CategoryLabels() map[string]string {
	catalogMu.RLock()
	defer catalogMu.RUnlock()
	out := make(map[string]string, len(effectiveLabels))
	for k, v := range effectiveLabels {
		out[k] = v
	}
	return out
}

// Catalog 返回当前有效的工具清单快照。
func Catalog() []Tool {
	catalogMu.RLock()
	defer catalogMu.RUnlock()
	out := make([]Tool, len(effectiveTools))
	copy(out, effectiveTools)
	return out
}

// LookupTool 按 ID 查找工具；未命中返回 false。
func LookupTool(id string) (Tool, bool) {
	catalogMu.RLock()
	defer catalogMu.RUnlock()
	for _, t := range effectiveTools {
		if t.ID == id {
			return t, true
		}
	}
	return Tool{}, false
}

// ToolsByCategory 按分类分组返回工具 ID 列表。
func ToolsByCategory() map[string][]string {
	m := map[string][]string{}
	for _, t := range Catalog() {
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

// Uninstall 卸载指定版本。如果卸载的是当前激活版本，自动激活推荐版本（仍在时），
// 否则激活剩余里版本号最高的一个。
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

	// 如果卸载的是激活版本，接替者由 fallbackVersion 决定
	if active == version {
		if next := fallbackVersion(id, ListVersions(dir, id)); next != "" {
			if err := SetActiveVersion(dir, id, next); err != nil {
				return err
			}
		}
	}

	// 重建软链
	return ReconcileBinLinks(dir)
}

// fallbackVersion 决定卸载激活版本后接替的版本：优先清单里的推荐版本（用户按推荐装过它
// 时，回到推荐最符合预期），否则取剩余里版本号最高的一个。
//
// 早期实现取 ListVersions 的字母序最后一个：单版本时代它等价于「唯一的那个」，多版本下
// 却会选中 `8u504` 这种字母序靠后、版本号反而最低的标签，把 PATH 上的 java 静默换成更旧的
// 版本（AUDIT N21）。工具已不在清单里（孤儿目录）时没有推荐版本可依，同样走数值最高。
func fallbackVersion(id string, remaining []string) string {
	if len(remaining) == 0 {
		return ""
	}
	if tool, ok := LookupTool(id); ok {
		recommended := tool.LatestVersion().Version
		for _, v := range remaining {
			if v == recommended {
				return recommended
			}
		}
	}
	return highestVersion(remaining)
}

// —— 软链与自愈 ——

// toolBinDirs 返回工具已激活版本显式声明的多 bin 目录（相对路径，见
// ToolVersion.BinDirs）。清单未声明或版本不存在时返回空，调用方走默认布局探测。
func toolBinDirs(id, dir string) []string {
	tool, ok := LookupTool(id)
	if !ok {
		return nil
	}
	tv, ok := tool.FindVersion(ActiveVersion(dir, id))
	if !ok {
		return nil
	}
	return tv.BinDirs
}

// toolBinNames 返回工具已激活版本声明的归档内文件名到对外命令名的映射（见
// ToolVersion.BinNames）。清单未声明或版本不存在时返回 nil，调用方沿用归档内原名。
func toolBinNames(id, dir string) map[string]string {
	tool, ok := LookupTool(id)
	if !ok {
		return nil
	}
	tv, ok := tool.FindVersion(ActiveVersion(dir, id))
	if !ok {
		return nil
	}
	return tv.BinNames
}

// ReconcileBinLinks 自愈：扫描 <dir>/current 下已装工具，在 <dir>/bin 重建其
// 可执行文件软链，在 <dir>/lib 重建库目录绑定（如有 LibRel），并清理失效软链。
// 启动时调用，保证重装、更新、HOME 迁移后工具链仍自动可用。
//
// 索引里的 bin_dirs/bin_names 与 current 软链目标都是外部数据，凡越出安装根目录
// 的取值一律拒绝：它们最终变成 <tools>/bin 下的软链，而该目录在 harness 的 PATH
// 上，越界等于把任意文件塞进命令搜索路径（审计 N10）。被拒条目会跳过，但会把
// 第一个错误返回给调用方——不静默成功。
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

	var firstErr error
	record := func(err error) {
		if firstErr == nil {
			firstErr = err
		}
	}

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
		// current/<id> 指向安装根之外时立刻放弃该条目：继续处理等于把任意目录
		// 的可执行文件软链进 <tools>/bin。
		if rel, err := filepath.Rel(dir, root); err != nil || !filepath.IsLocal(rel) {
			record(fmt.Errorf("toolchain: current/%s 指向安装根之外（%s），已跳过", e.Name(), root))
			continue
		}

		// 优先用清单里显式声明的多 bin 目录（如 Rust 的 cargo/bin + rustc/bin）；
		// 未声明时走默认布局探测：bin/ 子目录，否则工具根目录（单文件发行包）。
		binDirs := toolBinDirs(e.Name(), dir)
		names := toolBinNames(e.Name(), dir)
		if len(binDirs) > 0 {
			for _, rel := range binDirs {
				if !binDirOK(rel) {
					record(fmt.Errorf("toolchain: %s 的 bin_dirs %q 越出工具根目录，已跳过", e.Name(), rel))
					continue
				}
				if info, err := os.Stat(filepath.Join(root, rel)); err == nil && info.IsDir() {
					record(linkExecutables(filepath.Join(root, rel), linkDir, seenBins, names))
				}
			}
		} else if info, err := os.Stat(filepath.Join(root, "bin")); err == nil && info.IsDir() {
			record(linkExecutables(filepath.Join(root, "bin"), linkDir, seenBins, names))
		} else if info, err := os.Stat(root); err == nil && info.IsDir() {
			// 根目录直接含可执行
			record(linkExecutables(root, linkDir, seenBins, names))
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
	return firstErr
}

// linkNameOK 判断对外命令名能否安全地作为 <tools>/bin 下的软链名。
//
// 名字来自（可被远程索引覆盖的）BinNames，必须挡住越出 linkDir 的取值：如
// `../../.bashrc` 会让紧邻的 os.Remove 删掉安装目录之外的文件，再建一条指向
// 归档文件的软链（审计 N10）。
func linkNameOK(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	if strings.ContainsAny(name, `/\`) {
		return false
	}
	return filepath.IsLocal(name) // Windows 上另拒保留名（NUL/COM1 之类）
}

// binDirOK 判断清单声明的 bin 目录能否安全地拼到工具根目录下。
//
// 必须是留在根目录内的相对路径：`../../` 这类取值会令 linkExecutables 把任意
// 目录的可执行文件软链进 <tools>/bin（审计 N10）。
func binDirOK(rel string) bool {
	return filepath.IsLocal(rel)
}

// linkExecutables 把 src 下所有可执行文件软链进 linkDir，并记入 seen。
// names 把归档内文件名映射为对外命令名（见 ToolVersion.BinNames）：映射为空串的
// 文件直接跳过，未命中的沿用原名；seen 记录的是对外命令名，cleanStaleLinks 据此
// 保留本次重建的软链。
//
// 映射名来自外部索引，越出 linkDir 的取值会被跳过并把错误交给调用方（审计 N10）；
// 跳过而非中止，是为了让同一目录里其余合法文件仍按预期暴露。
func linkExecutables(src, linkDir string, seen map[string]bool, names map[string]string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return nil
	}
	var firstErr error
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
		file := e.Name()
		name := file
		if renamed, ok := names[file]; ok {
			if renamed == "" {
				continue // 清单显式要求不暴露
			}
			name = renamed
		}
		if !linkNameOK(name) {
			if firstErr == nil {
				firstErr = fmt.Errorf("toolchain: 索引里的命令名 %q 不是合法的软链名，已跳过", name)
			}
			continue
		}
		_ = os.Remove(filepath.Join(linkDir, name))
		if err := os.Symlink(filepath.Join(src, file), filepath.Join(linkDir, name)); err == nil {
			seen[name] = true
		}
	}
	return firstErr
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

// VersionGroup 是一个大版本线（如 JDK 21）在当前环境下的展示单元。
//
// 卡片按它渲染两级选择：一级列出所有 VersionGroup（大版本），二级列出选中组内的
// Versions（小版本）。二级只含「该线最新小版本」与「本机已装的小版本」——同线的
// 历史小版本之间只差补丁，全列出来只会淹没真正要选的项；跨大版本之间才是有兼容性
// 后果的选择，因此一级必须全部保留。
type VersionGroup struct {
	Major     string   // 大版本标识，如 "21"；界面直接展示它
	Latest    string   // 该线内清单里的最新小版本；该线只有孤儿版本时即该孤儿版本
	Installed []string // 该线内本机已装的小版本，降序
	Active    string   // 该线内的激活小版本，空表示该线未激活
	// Versions 是该线在界面上要展示的小版本（Latest ∪ Installed，降序）。
	// 后端算好下发，前端只做纯渲染，避免两处各自实现同一套合并规则。
	Versions []string
}

// ToolStatus 是工具的运行时状态，供 UI 渲染。
// 字段故意不带 JSON tag：序列化用 Go 字段名（PascalCase），与前端 toolCard 的
// camelCase 读取（c.ID / c.AvailableVersion / c.Installed）及外层 app.ToolStatus
// （同样无 tag）保持一致。历史教训：曾误加 snake_case tag（available_version
// 等），导致前端读不到值、工具市场卡片全空白、统计恒为「0 个工具」。
type ToolStatus struct {
	ID                string
	Name              string
	Category          string
	Description       string
	Provides          []string
	Dependencies      []string
	AvailableVersion  string   // 推荐版本
	AvailableVersions []string // 全部可装版本
	Installed         bool
	ActiveVersion     string   // 当前激活版本
	InstalledVersions []string // 所有已装版本
	Size              int64    // 字节
	HasUpdate         bool     // 激活版本低于同大版本线内的最新小版本；激活链接缺失/损坏时也为 true，作为修复入口
	// UpdateTarget 是 HasUpdate 为真时要升到的版本，即激活版本所属大版本线内的
	// Latest。它不等于 AvailableVersion：后者是「没装时该装哪个」的推荐版本，
	// 可能位于另一条大版本线（例如推荐 21 而用户装的是 8），拿它当更新目标会
	// 把「升级小版本」误导成「换大版本」。
	UpdateTarget string
	// Groups 是按大版本线归组后的展示单元，降序（新大版本在前）。
	// 单大版本工具的 Groups 长度为 1，界面据此退化成只显示小版本。
	Groups []VersionGroup
	// 以下三项描述"容器内运行时可用性"：该工具提供的命令已在当前 PATH 命中
	// （随包/宿主导入/系统提供），与市场仓库安装状态（Installed）相互独立。
	// 由 app 层组装填充，toolchain 包只声明字段；未命中均为空。
	RuntimeCmd     string // 命中的主命令名（如 node）
	RuntimeVersion string // 命中命令的版本号，探测失败为空
	RuntimeSource  string // 命中来源：随包 / 宿主导入 / 系统
}

// ToolStatuses 组装所有工具的状态列表。
func ToolStatuses(dir string) []ToolStatus {
	tools := Catalog()
	out := make([]ToolStatus, 0, len(tools))
	for _, t := range tools {
		latest := t.LatestVersion().Version
		installedVersions := ListVersions(dir, t.ID)
		// 已装版本按版本号从高到低展示：字母序会把 `8u504` 排在 `21.0.12.1` 之后，
		// 卡片下拉里「越靠前越新」的预期就不成立了。
		sortVersionsDesc(installedVersions)
		active := ActiveVersion(dir, t.ID)
		groups := buildVersionGroups(t, installedVersions, active)
		// 「可更新」只在激活版本所属的大版本线内比较：装 8u504 的用户不该被提示
		// 「更新到 21」，那是换大版本而不是打补丁，跨线迁移必须由用户主动选。
		// 按版本号而非字符串比较，是因为推荐版本只是清单首项，它可以低于当前激活
		// 版本（清单回退时），那时不该报更新。
		hasUpdate, updateTarget := updateOf(groups, installedVersions, active)
		ts := ToolStatus{
			ID:                t.ID,
			Name:              t.Name,
			Category:          t.Category,
			Description:       t.Description,
			Provides:          t.Provides,
			Dependencies:      t.Dependencies,
			AvailableVersion:  latest,
			Size:              t.LatestVersion().Size,
			InstalledVersions: installedVersions,
			ActiveVersion:     active,
			HasUpdate:         hasUpdate,
			UpdateTarget:      updateTarget,
			Groups:            groups,
		}
		for _, v := range t.Versions {
			ts.AvailableVersions = append(ts.AvailableVersions, v.Version)
		}
		ts.Installed = len(ts.InstalledVersions) > 0
		out = append(out, ts)
	}
	return out
}

// majorOf 返回版本所属的大版本线：清单显式声明优先，缺省时按版本号首个数字段兜底。
//
// 兜底规则与 compareVersions 的分段同源（`8u504` 取 8、`21.0.12.1` 取 21），因此旧索引
// 在没有 major 字段时也能归组。代价是首个数字段无区分度的工具会退化成单线（Go 的
// 1.26/1.27 都会并到 "1"），这正是新索引必须逐条显式声明 Major 的原因。
func majorOf(declared, version string) string {
	if declared != "" {
		return declared
	}
	if segs, ok := versionSegments(version); ok && len(segs) > 0 {
		return strconv.Itoa(segs[0])
	}
	// 整串不含数字（畸形或非版本标签）：按整串归组，至少让相同字符串落在同一组。
	return version
}

// buildVersionGroups 把清单版本与本机已装版本按大版本线归组，并算出每线要展示的小版本。
//
// 已装但不在清单里的版本（索引下架、手工放进目录的版本）必须单独成组并保留：归组
// 逻辑一旦漏掉它们，用户在卡片上既看不到、也切不回去、也卸不掉，而磁盘上那份安装
// 仍占着空间——这比多出一组更难解释。
func buildVersionGroups(tool Tool, installed []string, active string) []VersionGroup {
	byMajor := map[string]*VersionGroup{}
	var order []string
	ensure := func(major string) *VersionGroup {
		if g, ok := byMajor[major]; ok {
			return g
		}
		g := &VersionGroup{Major: major}
		byMajor[major] = g
		order = append(order, major)
		return g
	}
	for _, v := range tool.Versions {
		g := ensure(majorOf(v.Major, v.Version))
		if g.Latest == "" || compareVersions(v.Version, g.Latest) > 0 {
			g.Latest = v.Version
		}
	}
	for _, v := range installed {
		g := ensure(majorOf("", v))
		g.Installed = append(g.Installed, v)
		if g.Latest == "" {
			// 孤儿版本：该线在清单里没有任何条目，它自己就是这条线唯一可见的版本。
			g.Latest = v
		}
	}
	out := make([]VersionGroup, 0, len(order))
	for _, major := range order {
		g := byMajor[major]
		if len(g.Installed) > 1 {
			sortVersionsDesc(g.Installed)
		}
		g.Active = activeIn(g.Installed, active)
		g.Versions = groupVersions(g.Latest, g.Installed)
		out = append(out, *g)
	}
	// 新大版本排在前面：一级下拉里「越靠前越新」与二级的排序预期一致。
	sort.SliceStable(out, func(i, j int) bool {
		return compareVersions(out[i].Latest, out[j].Latest) > 0
	})
	return out
}

// activeIn 返回激活版本在该线已装集合中的归属；不属于该线时返回空。
func activeIn(installed []string, active string) string {
	for _, v := range installed {
		if v == active {
			return active
		}
	}
	return ""
}

// groupVersions 合并「该线最新小版本」与「该线已装小版本」，降序去重。
// 同线的历史小版本不在此列：它们与最新版只差补丁，全列出来会淹没真正要选的项。
func groupVersions(latest string, installed []string) []string {
	out := make([]string, 0, len(installed)+1)
	if latest != "" {
		out = append(out, latest)
	}
	for _, v := range installed {
		if v != latest {
			out = append(out, v)
		}
	}
	sortVersionsDesc(out)
	return out
}

// updateOf 判定是否有更新及更新目标，只在激活版本所属的大版本线内比较。
//
// 激活链接缺失或损坏（active 为空）而本机确有安装时仍报更新，并把目标指向最新一条
// 大版本线：此时「更新」是一键重建软链的入口，与既有语义一致。
func updateOf(groups []VersionGroup, installed []string, active string) (bool, string) {
	if len(installed) == 0 {
		return false, ""
	}
	if active == "" {
		// 软链缺失/损坏（含用户手工删了 current 的情况）。修复目标取「已装最高版本
		// 所属那条线」的最新版，而不是清单里最新的大版本线：后者会把「重建软链」
		// 偷偷变成「换大版本」，而用户原本刻意激活的可能是旧线（如 JDK 8）。
		// 已装版本的归组由 buildVersionGroups 保证必然存在，因此按每个组的
		// Installed 反查即可，无需再算一次 major。
		var newest *VersionGroup
		for i := range groups {
			if len(groups[i].Installed) == 0 {
				continue
			}
			if newest == nil || compareVersions(groups[i].Latest, newest.Latest) > 0 {
				newest = &groups[i]
			}
		}
		if newest == nil || newest.Latest == "" {
			return false, ""
		}
		return true, newest.Latest
	}
	for _, g := range groups {
		if g.Active != active {
			continue
		}
		if g.Latest != "" && compareVersions(active, g.Latest) < 0 {
			return true, g.Latest
		}
		return false, ""
	}
	return false, ""
}

// OutdatedTools 返回所有已安装且有更新版本的工具 ID 列表。
func OutdatedTools(dir string) []string {
	var outdated []string
	for _, ts := range ToolStatuses(dir) {
		if ts.HasUpdate {
			outdated = append(outdated, ts.ID)
		}
	}
	return outdated
}
