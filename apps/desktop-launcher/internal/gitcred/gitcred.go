// Package gitcred 读写 git store 凭据文件（~/.git-credentials）。
// 只处理 github.com 条目，保留其它 host 的行；绝不把令牌明文暴露给调用方之外。
package gitcred

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Path 返回 git store 凭据文件路径。
func Path(home string) string { return filepath.Join(home, ".git-credentials") }

// splitCredential 把 git store 行 "https://user:token@host" 拆成
// user/token/host；非该格式（空行/其它 scheme/缺段）时 ok=false。
func splitCredential(line string) (user, token, host string, ok bool) {
	rest, found := strings.CutPrefix(strings.TrimSpace(line), "https://")
	if !found {
		return "", "", "", false
	}
	cred, host, found := strings.Cut(rest, "@")
	if !found {
		return "", "", "", false
	}
	user, token, found = strings.Cut(cred, ":")
	if !found {
		return "", "", "", false
	}
	return user, token, host, true
}

// Read 读取 github.com 条目；无条目时 found=false。
func Read(home string) (user, token string, found bool) {
	data, err := os.ReadFile(Path(home))
	if err != nil {
		return "", "", false
	}
	for _, line := range strings.Split(string(data), "\n") {
		user, token, host, ok := splitCredential(line)
		if ok && host == "github.com" {
			return user, token, true
		}
	}
	return "", "", false
}

// Write 写入/覆盖 github.com 条目，保留其它 host 行。
// user/token 中的换行会被剥离，防止注入伪造凭据行。
func Write(home, user, token string) error {
	user = sanitize(user)
	token = sanitize(token)
	if user == "" || token == "" {
		return fmt.Errorf("用户名与令牌不能为空")
	}
	path := Path(home)
	lines := []string{}
	if data, err := os.ReadFile(path); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if _, _, host, ok := splitCredential(line); ok && host == "github.com" {
				continue // 丢弃旧 github.com 行
			}
			if strings.TrimSpace(line) != "" {
				lines = append(lines, strings.TrimSpace(line))
			}
		}
	}
	lines = append(lines, fmt.Sprintf("https://%s:%s@github.com", user, token))
	if err := os.MkdirAll(home, 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600) // 收紧既有文件的权限，git 拒绝 group/world 可读
}

// Clear 删除 github.com 条目；文件为空/无条目时删除文件。
func Clear(home string) error {
	path := Path(home)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	kept := []string{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if _, _, host, ok := splitCredential(line); ok && host == "github.com" {
			continue
		}
		kept = append(kept, line)
	}
	if len(kept) == 0 {
		return os.Remove(path)
	}
	return os.WriteFile(path, []byte(strings.Join(kept, "\n")+"\n"), 0o600)
}

// sanitize 剥离可能破坏行结构或凭据分隔符的字符。
func sanitize(s string) string {
	s = strings.TrimSpace(s)
	s = strings.NewReplacer("\n", "", "\r", "").Replace(s)
	return s
}
