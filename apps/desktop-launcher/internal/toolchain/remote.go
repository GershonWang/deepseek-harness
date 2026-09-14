package toolchain

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// Index 是远程工具索引的完整结构（与 tools/index.json 同构）。
// Bundles（一键工具集）已从产品中移除：索引文件里若还残留 bundles 字段，
// json.Unmarshal 会按未知字段忽略，不影响解析。
type Index struct {
	Version   int    `json:"version"`
	UpdatedAt string `json:"updated_at"`
	Tools     []Tool `json:"tools"`
	// CategoryLabels 是分类 ID 到中文标签的映射。标签属于索引数据而非客户端常量：
	// 分类页签按清单里的分类生成后，中文名若还写死在客户端，新增分类就仍要等发版。
	// 缺项由客户端的兜底表补齐，旧索引（无此字段）照常工作。
	CategoryLabels map[string]string `json:"category_labels,omitempty"`
}

// indexCacheTTL 是远程索引缓存有效期。超过后下次加载会尝试重新拉取。
const indexCacheTTL = 24 * time.Hour

// defaultIndexURL 是远程索引默认地址，引用固定到提交哈希而非分支名。
//
// 索引自带每个工具的下载地址与 sha256，两者出自同一份数据：sha256 只能证明归档与
// 清单一致，不能证明清单本身可信。引用一旦是分支名，控制该分支就能整体替换索引与
// 哈希，而校验方无从察觉。固定提交后，索引内容由客户端自身的发布过程锚定——信任
// 对象从「上游账号」收敛为「这份二进制」。
//
// 代价与更新方式：索引更新不再能只发索引，必须改这个常量并重新发客户端。
// 升级时把 <sha> 换成承载新 index.json 的不可变提交（该提交随合并进入 linglong，
// 因而永久可达；用
// `git rev-parse <commit>:apps/desktop-launcher/internal/toolchain/tools/index.json`
// 确认 blob 与工作区一致），并保持 TestDefaultIndexURL_PinnedToCommit 通过。
//
// DSH_TOOLCHAIN_INDEX_URL 仍可在构建/部署时覆盖该地址（内网镜像或自托管）；
// 环境变量与二进制同属一个信任域，不构成额外的攻击面。
const defaultIndexURL = "https://raw.githubusercontent.com/GershonWang/deepseek-harness/ff0b924d11a2ca5cef4a908bec0ec54282ae7dc8/apps/desktop-launcher/internal/toolchain/tools/index.json"

// indexURL 返回生效的远程索引地址。
func indexURL() string {
	if u := os.Getenv("DSH_TOOLCHAIN_INDEX_URL"); u != "" {
		return u
	}
	return defaultIndexURL
}

// indexCachePath 返回本地索引缓存路径（<dir>/index.json）。
func indexCachePath(dir string) string {
	return filepath.Join(dir, "index.json")
}

// indexMetaPath 返回本地缓存元数据路径（记录拉取时间）。
func indexMetaPath(dir string) string {
	return filepath.Join(dir, "index.meta")
}

// ParseIndex 解析索引 JSON。空 tools 也视为有效（远程可能临时为空）。
func ParseIndex(data []byte) (*Index, error) {
	var idx Index
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, fmt.Errorf("parse index: %w", err)
	}
	if idx.Version != 1 {
		return nil, fmt.Errorf("unsupported index version %d", idx.Version)
	}
	return &idx, nil
}

// cacheFresh 判断本地缓存是否仍有效且未过期。meta 记录的是写入时刻。
func cacheFresh(dir string) bool {
	data, err := os.ReadFile(indexMetaPath(dir))
	if err != nil {
		return false
	}
	var t time.Time
	if err := t.UnmarshalText(data); err != nil {
		return false
	}
	return time.Since(t) < indexCacheTTL
}

// loadCachedIndex 读取本地缓存索引；失败或不存在返回 nil。
func loadCachedIndex(dir string) *Index {
	data, err := os.ReadFile(indexCachePath(dir))
	if err != nil {
		return nil
	}
	idx, err := ParseIndex(data)
	if err != nil {
		return nil
	}
	return idx
}

// storeIndex 写索引与其拉取时间元数据到本地缓存。
func storeIndex(dir string, data []byte) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(indexCachePath(dir), data, 0o644); err != nil {
		return err
	}
	meta := time.Now().UTC().AppendFormat(nil, time.RFC3339)
	return os.WriteFile(indexMetaPath(dir), meta, 0o644)
}

// fetchIndex 从 url 拉取索引并返回原始字节。
func fetchIndex(url string) ([]byte, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("index %s: %s", url, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 4<<20)) // 上限 4 MiB
}

// LoadIndex 按混合策略加载远程索引并更新有效目录：
//  1. 缓存未过期 → 直接用缓存；
//  2. 否则拉取远程，成功则写缓存并生效；
//  3. 拉取失败 → 用缓存（即使过期）；
//  4. 无缓存 → 保持内置兜底（init 时已生效）。
//
// 返回生效来源："remote" | "cache" | "builtin"，以及拉取错误（仅诊断用，
// 非致命）。调用方据此在 UI 上标注索引新鲜度。
func LoadIndex(dir string) (source string, err error) {
	if cacheFresh(dir) {
		if idx := loadCachedIndex(dir); idx != nil {
			setCatalog(idx.Tools, idx.CategoryLabels)
			return "cache", nil
		}
	}
	if src, ferr := loadRemote(dir); ferr == nil {
		return src, nil
	} else {
		return fallback(dir), ferr
	}
}

// RefreshIndex 强制重新拉取远程索引（忽略缓存新鲜度），失败仍回退缓存/内置。
func RefreshIndex(dir string) (source string, err error) {
	if src, ferr := loadRemote(dir); ferr == nil {
		return src, nil
	} else {
		return fallback(dir), ferr
	}
}

// loadRemote 拉取远程索引、写缓存并生效；失败返回错误，不改动有效目录。
func loadRemote(dir string) (string, error) {
	data, err := fetchIndex(indexURL())
	if err != nil {
		return "", err
	}
	idx, err := ParseIndex(data)
	if err != nil {
		return "", err
	}
	_ = storeIndex(dir, data) // 缓存写失败不阻断生效
	setCatalog(idx.Tools, idx.CategoryLabels)
	return "remote", nil
}

// fallback 回退到缓存（即使过期），否则保持内置兜底。返回生效来源。
func fallback(dir string) string {
	if idx := loadCachedIndex(dir); idx != nil {
		setCatalog(idx.Tools, idx.CategoryLabels)
		return "cache"
	}
	return "builtin"
}
