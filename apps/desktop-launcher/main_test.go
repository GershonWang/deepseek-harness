// 启动器嵌入资源的结构判据。
//
// 为什么需要它：main.go 用显式清单嵌入前端，而「清单里写了什么」与「frontend/ 下
// 实际有什么」是两个互相独立的事实。新增前端文件却忘了登记，构建不会失败，后果要到
// 运行时才以空白窗口显形；把仅开发用的文件写进清单则会静默增大二进制。本测试把这两
// 个事实对齐，不一致时点名文件并给出两个去向。
package main

import (
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// devOnlyFrontendFiles 是 frontend/ 下只供开发使用的文件：main.go 的嵌入清单刻意
// 排除它们，因此不进入启动器二进制。这些文件改名、新增或删除时，本清单与 main.go
// 的清单必须同步。
var devOnlyFrontendFiles = []string{
	"frontend/test-app.cjs",
	"frontend/test-i18n.cjs",
	"frontend/tools/preview.mjs",
}

// TestFrontendEmbedMatchesShippedFiles 断言 frontend/ 下磁盘上的每个文件，要么被
// 嵌入随包，要么在仅开发用清单里。
func TestFrontendEmbedMatchesShippedFiles(t *testing.T) {
	onDisk, err := frontendFilesOnDisk()
	if err != nil {
		t.Fatalf("列出 frontend/ 失败: %v", err)
	}
	embedded, err := embeddedFrontendFiles()
	if err != nil {
		t.Fatalf("遍历嵌入资源失败: %v", err)
	}

	devOnly := make(map[string]bool, len(devOnlyFrontendFiles))
	for _, path := range devOnlyFrontendFiles {
		devOnly[path] = true
		// 清单指向不存在的文件时，说明它已改名或删除，本测试会因此失去约束力。
		if !onDisk[path] {
			t.Errorf("仅开发用清单里的 %s 在磁盘上不存在：该文件已改名或删除，请同步 devOnlyFrontendFiles", path)
		}
		if embedded[path] {
			t.Errorf("%s 同时在仅开发用清单与嵌入集合里：它会被打进启动器二进制，请从 main.go 的 //go:embed 清单中移除", path)
		}
	}

	var unclassified []string
	for path := range onDisk {
		if !embedded[path] && !devOnly[path] {
			unclassified = append(unclassified, path)
		}
	}
	if len(unclassified) > 0 {
		sort.Strings(unclassified)
		t.Errorf("frontend/ 下这些文件既没有随包，也不在仅开发用清单里：\n  %s\n二选一：要随包就加进 main.go 的 //go:embed 清单，只供开发就加进 devOnlyFrontendFiles",
			strings.Join(unclassified, "\n  "))
	}
}

// frontendFilesOnDisk 返回 frontend/ 下磁盘上所有普通文件的路径集合，分隔符统一为
// `/`，与嵌入文件系统里的写法一致。
func frontendFilesOnDisk() (map[string]bool, error) {
	files := map[string]bool{}
	err := filepath.WalkDir("frontend", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		files[filepath.ToSlash(path)] = true
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

// embeddedFrontendFiles 返回真正嵌入启动器的文件路径集合。取自 main.go 声明的
// assets 而非清单文本，因此断言的是随包事实。
func embeddedFrontendFiles() (map[string]bool, error) {
	files := map[string]bool{}
	err := fs.WalkDir(assets, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			files[path] = true
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}
