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
}

// indexCacheTTL 是远程索引缓存有效期。超过后下次加载会尝试重新拉取。
const indexCacheTTL = 24 * time.Hour

// defaultIndexURL 是远程索引默认地址。可在构建/部署时用
// DSH_TOOLCHAIN_INDEX_URL 环境变量覆盖（便于内网镜像或自托管）。
const defaultIndexURL = "https://raw.githubusercontent.com/GershonWang/deepseek-harness/linglong/apps/desktop-launcher/internal/toolchain/tools/index.json"

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
			setCatalog(idx.Tools)
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
	setCatalog(idx.Tools)
	return "remote", nil
}

// fallback 回退到缓存（即使过期），否则保持内置兜底。返回生效来源。
func fallback(dir string) string {
	if idx := loadCachedIndex(dir); idx != nil {
		setCatalog(idx.Tools)
		return "cache"
	}
	return "builtin"
}
