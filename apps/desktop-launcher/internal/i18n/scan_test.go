// 本文件是 Go 侧的机器兜底：把「源码里不许出现中文文案」「调用点引用的键必须存在」
// 这两条只靠约定维持的规则钉成可执行测试。
//
// 为什么必须有它：ErrorKind、ValidationKind 这类字符串枚举允许把未加类型的字符串常量
// 直接赋给它（`return "网络连接失败"` 与 `return ErrorKindNetwork` 同样合法），编译期
// 抓不住把中文当枚举值的漏改；键名写错则不会报错，界面上会直接显示键名本身。两条都
// 只能靠扫描发现。领域包不再含文案这一架构约束（见 docs/i18n.md 第六节）也由它守住。
//
// 判据不是「文件白名单」而是「字面量白名单」：日志与诊断链按计划不迁（它们面向排障、
// 不随语言变化），但同一个文件里新写的用户文案不会被顺带放行。
package i18n

import (
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// moduleRoot 返回外壳 Go module 的根目录（本包是 internal/i18n）。
func moduleRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("解析 module 根目录失败: %v", err)
	}
	return root
}

// productionGoFiles 列出参与扫描的非测试 Go 源文件。
//
// 跳过测试文件（用例里的中文期望值是判据的一部分）、tmp/ 下的一次性探针脚本，以及
// 本包的字典文件（它是中文唯一的合法归属地）。
func productionGoFiles(t *testing.T) []string {
	t.Helper()
	root := moduleRoot(t)
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "tmp", "dist", "build":
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		if filepath.Base(path) == "messages.go" {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatalf("遍历源码失败: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("没扫到任何 Go 源文件：路径判断有误，扫描等于没跑")
	}
	return files
}

// literal 是源码里的一个字符串字面量及其位置。
type literal struct {
	file string
	line int
	text string
}

// stringLiterals 用标准库扫描器取出一个文件里所有字符串字面量（注释不算）。
//
// 用 go/scanner 而不是正则：它能正确处理反引号原文串、转义与字符串里的注释符号，
// 正则在这三种情况下都会误判。
func stringLiterals(t *testing.T, file string) []literal {
	t.Helper()
	src, err := os.ReadFile(filepath.Join(moduleRoot(t), file))
	if err != nil {
		t.Fatalf("读 %s 失败: %v", file, err)
	}
	fset := token.NewFileSet()
	parsed := fset.AddFile(file, fset.Base(), len(src))
	var s scanner.Scanner
	s.Init(parsed, src, nil, scanner.ScanComments)
	var out []literal
	for {
		pos, tok, lit := s.Scan()
		if tok == token.EOF {
			break
		}
		// 注释里允许中文：注释是给维护者看的，不是界面文案。
		if tok != token.STRING {
			continue
		}
		text := lit
		if decoded, err := strconv.Unquote(lit); err == nil {
			text = decoded
		}
		out = append(out, literal{file: file, line: fset.Position(pos).Line, text: text})
	}
	return out
}

// cjk 匹配表意文字与全角标点。全角标点也算：`"（"` 这类不含字母的字面量同样是文案，
// 只查汉字会漏。用 \x{...} 转义而不是 \uXXXX——Go 的 regexp 不认后者（会 panic）。
var cjk = regexp.MustCompile(`[\x{3000}-\x{303F}\x{3400}-\x{4DBF}\x{4E00}-\x{9FFF}\x{F900}-\x{FAFF}\x{FE30}-\x{FE4F}\x{FF00}-\x{FFEF}]`)

// allowedChinese 是按计划不迁的中文：日志与诊断链。
//
// 键是仓库相对路径，值是允许出现在该文件里的字面量原文；精确到字面量而非整个文件，
// 是为了让「同一个文件里新写的用户文案」仍然被拦下。
var allowedChinese = map[string][]string{
	"internal/app/autodisabled.go": {
		"无法序列化已提示记录: ",
		"无法创建运行时目录: ",
		"无法写入已提示记录: ",
	},
	"internal/packaging/webkit_linux.go": {
		"dsh-desktop: 创建 webkit helper 符号链接失败: %v\n",
	},
	"internal/toolchain/catalog.go": {
		"toolchain: current/%s 指向安装根之外（%s），已跳过",
		"toolchain: %s 的 bin_dirs %q 越出工具根目录，已跳过",
		"toolchain: 索引里的命令名 %q 不是合法的软链名，已跳过",
	},
	"internal/toolchain/project.go": {
		"line %d: 无法解析 %q",
		"line %d: tools 应为 section 头（tools:）",
		"需要 true 或 false，得到 %q",
	},
	"internal/appenv/env.go": {
		// 写进磁盘的 cordis overlay 注释：面向生成物而不是界面——它解释「插件市场
		// 为什么不得自行重启」，内容必须与界面语言无关（同一份 overlay 会被反复
		// 覆盖写入，按语言变化会让文件内容随界面漂移）。
		`# 由 dsh-desktop-launcher 生成，请勿手工编辑。
# harness 由 launcher 的 Supervisor 监护（spawn、重启、端口都归它管），
# 插件市场不得再自行重启：两者会用同一个 --port 竞态，先 bind 的胜出，
# 另一方 EADDRINUSE 退出。
- id: dsh-market
  config:
    allowRestart: false
`,
	},
	"main.go": {
		"工具链软链自愈有被拒绝的条目: %v",
	},
}

// TestNoChineseLiteralsOutsideDictionary 断言中文文案只出现在字典里。
//
// 这是「领域包只报事实、app 层渲染」的机器兜底：领域包一旦把文案写回源码，这里就会红。
func TestNoChineseLiteralsOutsideDictionary(t *testing.T) {
	for _, file := range productionGoFiles(t) {
		allowed := make(map[string]bool, len(allowedChinese[file]))
		for _, text := range allowedChinese[file] {
			allowed[text] = true
		}
		for _, lit := range stringLiterals(t, file) {
			if !cjk.MatchString(lit.text) || allowed[lit.text] {
				continue
			}
			if _, ok := allowedChinese[file]; ok && strings.TrimSpace(lit.text) == "" {
				continue
			}
			t.Errorf("%s:%d 出现中文字面量：%q\n"+
				"用户可见文案请加进 internal/i18n/messages.go 并在调用点用 a.t(...)；"+
				"确属日志/诊断链不迁的，请连同理由加进本文件顶部 allowedChinese",
				lit.file, lit.line, lit.text)
		}
	}
}

// TestReferencedKeysExist 断言静态键引用都能在字典里查到。
//
// 键名写错不会编译失败，界面上会显示键名本身（如 "toolchain.unknownID"），只有这里能
// 拦住。拼出来的动态键（"toolchain.error." + kind）由 TestKindValuesHaveKeys 反查。
func TestReferencedKeysExist(t *testing.T) {
	// 允许出现在调用点但故意不在字典里的键：目前没有，保留该表是为了让例外集中可见。
	exempt := map[string]bool{}
	checked := 0
	for _, file := range productionGoFiles(t) {
		fset := token.NewFileSet()
		parsed, err := parser.ParseFile(fset, filepath.Join(moduleRoot(t), file), nil, 0)
		if err != nil {
			t.Fatalf("解析 %s 失败: %v", file, err)
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			key, ok := renderedKeyArg(call)
			if !ok {
				return true
			}
			checked++
			if _, ok := messages[Zh][key]; !ok && !exempt[key] {
				pos := fset.Position(call.Pos())
				t.Errorf("%s:%d 引用了字典里不存在的键 %q", file, pos.Line, key)
			}
			return true
		})
	}
	if checked == 0 {
		t.Fatal("一个键引用都没扫到：判据失效，等于没跑")
	}
	t.Logf("已校验 %d 处静态键引用", checked)
}

// renderedKeyArg 从一次调用里取出「字面量键」参数。
//
// 认两种形态：app 层的 `a.t("key", ...)` 与包级 `i18n.T(locale, "key", ...)`。返回
// false 表示这次调用不是取键（或键是拼出来的，交给 kind 反查那条用例）。
func renderedKeyArg(call *ast.CallExpr) (string, bool) {
	var args []ast.Expr
	switch fun := call.Fun.(type) {
	case *ast.SelectorExpr:
		args = call.Args
		if fun.Sel.Name == "T" && len(args) > 1 {
			args = args[1:] // i18n.T(locale, key, ...)：跳过语言参数
		} else if fun.Sel.Name != "t" {
			return "", false
		}
	default:
		return "", false
	}
	// 取第一个字符串字面量参数：`t("key")` 与 `t("key", args...)` 都是它。
	for _, arg := range args {
		basic, ok := arg.(*ast.BasicLit)
		if !ok {
			continue
		}
		if basic.Kind != token.STRING {
			return "", false
		}
		key, err := strconv.Unquote(basic.Value)
		if err != nil {
			return "", false
		}
		return key, true
	}
	return "", false
}

// keyPrefixShape 匹配「拼动态键用的前缀字面量」：全小写点分、以点结尾，如
// "toolchain.error."。中文与含大写/空格的字符串天然不匹配，因此不必额外排除。
var keyPrefixShape = regexp.MustCompile(`^[a-z][a-z0-9]*(?:\.[a-z0-9]+)*\.$`)

// TestDynamicKeyPrefixesResolve 断言每个动态键前缀都能命中字典键。
//
// 这是上一条用例漏掉的那半：TestKindValuesHaveKeys 从枚举定义处取值、拼它自己写的前缀，
// 因此 render.go 里把 "connector.error." 敲成 "connector.errors." 不会被它发现——那种
// 错误会让界面上显示一串键名。这里改为从源码里取出前缀字面量，要求字典里真有以它开头
// 的键。
func TestDynamicKeyPrefixesResolve(t *testing.T) {
	checked := 0
	for _, file := range productionGoFiles(t) {
		for _, lit := range stringLiterals(t, file) {
			if !keyPrefixShape.MatchString(lit.text) {
				continue
			}
			checked++
			hit := false
			for key := range messages[Zh] {
				if strings.HasPrefix(key, lit.text) {
					hit = true
					break
				}
			}
			if !hit {
				t.Errorf("%s:%d 的前缀 %q 在字典里没有任何键：多半是前缀敲错了",
					lit.file, lit.line, lit.text)
			}
		}
	}
	if checked == 0 {
		t.Fatal("一个动态键前缀都没扫到：判据失效，等于没跑")
	}
	t.Logf("已校验 %d 处动态键前缀", checked)
}

// TestKindValuesHaveKeys 反查「拼出来的键」：每一族枚举值都必须有对应的字典键。
//
// 这些键在源码里是 `"toolchain.error." + string(kind)` 拼出来的，静态扫不到；因此从
// 枚举定义处取值再回来比对——新加一个 kind 却忘了字典，这条会红。
func TestKindValuesHaveKeys(t *testing.T) {
	families := []struct {
		file     string
		typeName string
		prefix   string
	}{
		{"internal/toolchain/install.go", "ErrorKind", "toolchain.error."},
		{"internal/connector/connector.go", "ValidationKind", "connector.error."},
		{"internal/hosttools/hosttools.go", "ErrorKind", "hosttool.error."},
		{"internal/hosttools/hosttools.go", "WarningKind", "hosttool.warning."},
		{"internal/preflight/preflight.go", "DoctorErrorKind", "preflight.doctor."},
	}
	decl := regexp.MustCompile(`(?m)^\s*[A-Z]\w*\s+(\w+)\s*=\s*"([^"]+)"`)
	for _, family := range families {
		src, err := os.ReadFile(filepath.Join(moduleRoot(t), family.file))
		if err != nil {
			t.Fatalf("读 %s 失败: %v", family.file, err)
		}
		found := 0
		for _, match := range decl.FindAllStringSubmatch(string(src), -1) {
			if match[1] != family.typeName {
				continue
			}
			found++
			key := family.prefix + match[2]
			if _, ok := messages[Zh][key]; !ok {
				t.Errorf("%s 的 %s 取值 %q 缺字典键 %q",
					family.file, family.typeName, match[2], key)
			}
		}
		if found == 0 {
			t.Errorf("%s 里没找到 %s 的取值：判据失效，等于没跑", family.file, family.typeName)
		}
	}
}
