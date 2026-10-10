package packaging

import (
	"os"
	"path/filepath"
	"testing"
)

// 未打包态（prefix 为空）必须返回空，避免把 FONTCONFIG_FILE 指到相对路径。
func TestFontConfigPathEmptyPrefix(t *testing.T) {
	if got := fontConfigPath(""); got != "" {
		t.Errorf("prefix 为空时应返回空串，得到 %q", got)
	}
}

// 打包态应落在 <prefix>/etc/fonts/dsh-fonts.conf，与构建脚本生成位置一致。
func TestFontConfigPathUnderPrefix(t *testing.T) {
	prefix := t.TempDir()
	want := filepath.Join(prefix, "etc", "fonts", "dsh-fonts.conf")
	if got := fontConfigPath(prefix); got != want {
		t.Errorf("配置路径不符：want %q, got %q", want, got)
	}
}

// 配置缺失时必须不注入：指向缺失路径会让 fontconfig 退回内置默认，反而丢掉
// 宿主既有的字体解析与中文回退。
func TestFontConfigEnvSkipsWhenMissing(t *testing.T) {
	prefix := t.TempDir()
	if got := fontConfigEnv(prefix); got != "" {
		t.Errorf("配置缺失时应返回空串，得到 %q", got)
	}
}

// 配置存在时应返回其绝对路径。
func TestFontConfigEnvWhenPresent(t *testing.T) {
	prefix := t.TempDir()
	conf := fontConfigPath(prefix)
	if err := os.MkdirAll(filepath.Dir(conf), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(conf, []byte("<fontconfig/>\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := fontConfigEnv(prefix); got != conf {
		t.Errorf("配置存在时应返回其路径：want %q, got %q", conf, got)
	}
}

// 未打包态调用不得改动环境变量。
func TestConfigureFontConfigLeavesEnvWhenUnpackaged(t *testing.T) {
	t.Setenv(fontConfigEnvName, "/sentinel/keep-me")
	if got := fontConfigEnv(""); got != "" {
		t.Fatalf("未打包态不应产出注入值，得到 %q", got)
	}
	ConfigureFontConfig()
	if got := os.Getenv(fontConfigEnvName); got != "/sentinel/keep-me" {
		t.Errorf("未打包态不应改动 %s，得到 %q", fontConfigEnvName, got)
	}
}
