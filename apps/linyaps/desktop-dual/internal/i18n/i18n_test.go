package i18n

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"
)

// TestNormalizeCoversPosixForms 覆盖真实出现过的 locale 写法：带编码、带修饰、
// 下划线与连字符两种分隔，以及 POSIX 的 C/POSIX 兜底。
func TestNormalizeCoversPosixForms(t *testing.T) {
	cases := map[string]Locale{
		"zh_CN.UTF-8": Zh,
		"zh_CN":       Zh,
		"zh-Hans":     Zh,
		"zh_TW":       Zh,
		"en_US.UTF-8": En,
		"en":          En,
		"C":           En,
		"POSIX":       En,
		"":            "",
		"  ":          "",
		"de_DE.UTF-8": En, // 暂无该语言字典，回退英文
	}
	for raw, want := range cases {
		if got := Normalize(raw); got != want {
			t.Errorf("Normalize(%q) = %q，期望 %q", raw, got, want)
		}
	}
}

// TestFromEnvFollowsPosixPriority 覆盖环境变量优先级：LC_ALL 覆盖 LC_MESSAGES 与 LANG，
// 三者都为空时回退英文。两个进程（切换器与壳客户端）必须按同一套规则取语言。
func TestFromEnvFollowsPosixPriority(t *testing.T) {
	cases := []struct {
		name       string
		lcAll      string
		lcMessages string
		lang       string
		want       Locale
	}{
		{"LC_ALL 优先", "en_US.UTF-8", "zh_CN.UTF-8", "zh_CN.UTF-8", En},
		{"LC_ALL 为空时看 LC_MESSAGES", "", "zh_CN.UTF-8", "en_US.UTF-8", Zh},
		{"前两者为空时看 LANG", "", "", "zh_CN.UTF-8", Zh},
		{"全部为空回退英文", "", "", "", En},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("LC_ALL", tc.lcAll)
			t.Setenv("LC_MESSAGES", tc.lcMessages)
			t.Setenv("LANG", tc.lang)
			if got := FromEnv(); got != tc.want {
				t.Fatalf("FromEnv() = %q，期望 %q", got, tc.want)
			}
		})
	}
}

// TestMessagesShareKeys 覆盖字典完整性：两种语言必须同键集，否则切换语言后会出现
// 回退成英文或直接显示键名的界面。
func TestMessagesShareKeys(t *testing.T) {
	for key := range messages[Zh] {
		if _, ok := messages[En][key]; !ok {
			t.Errorf("英文缺少键 %q", key)
		}
	}
	for key := range messages[En] {
		if _, ok := messages[Zh][key]; !ok {
			t.Errorf("中文缺少键 %q", key)
		}
	}
}

// TestTRejectsUnknownKey 覆盖未知键：返回键名而不是空串，界面上能直接看出漏了什么。
func TestTRejectsUnknownKey(t *testing.T) {
	if got := T(Zh, "tray.missing"); got != "tray.missing" {
		t.Fatalf("未知键应原样返回键名，得到 %q", got)
	}
}

// TestNoHardcodedCopyOutsideDictionary 断言模块内除字典以外的 Go 源码不含中文字面量：
// 面向用户的文案必须走本包，否则换语言时必然漏掉某处。
//
// 诊断日志一律用英文，这条规则因此没有例外要解释：日志面向排障、不随语言变化，
// 英文也更便于按关键词检索与跨语言排查。
func TestNoHardcodedCopyOutsideDictionary(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("解析 module 根目录失败: %v", err)
	}
	dictionaryDir := filepath.Join(root, "internal", "i18n")
	var offenders []string
	walkErr := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path == dictionaryDir || entry.Name() == "stage" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(file, func(node ast.Node) bool {
			lit, ok := node.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			if strings.ContainsFunc(lit.Value, func(r rune) bool { return unicode.Is(unicode.Han, r) }) {
				offenders = append(offenders, fmt.Sprintf("%s:%d %s", path, fset.Position(lit.Pos()).Line, lit.Value))
			}
			return true
		})
		return nil
	})
	if walkErr != nil {
		t.Fatalf("扫描模块源码失败: %v", walkErr)
	}
	if len(offenders) > 0 {
		t.Fatalf("以下字面量应进 internal/i18n 字典：\n  %s", strings.Join(offenders, "\n  "))
	}
}
