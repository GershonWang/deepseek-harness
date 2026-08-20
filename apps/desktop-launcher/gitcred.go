package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// gitCredentialsPath 返回 git store 凭据文件路径。
func gitCredentialsPath(home string) string { return filepath.Join(home, ".git-credentials") }

// splitCredential 把 git store 行 "https://user:token@host" 拆成
// user/token/host;非该格式(空行/其它 scheme/缺段)时 ok=false。
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

// ReadGitCredentials 读取 github.com 条目;无条目时 found=false。
func ReadGitCredentials(home string) (user, token string, found bool) {
	data, err := os.ReadFile(gitCredentialsPath(home))
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

// WriteGitCredentials 写入/覆盖 github.com 条目,保留其它 host 行。
func WriteGitCredentials(home, user, token string) error {
	path := gitCredentialsPath(home)
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
	data := []byte(strings.Join(lines, "\n") + "\n")
	return os.WriteFile(path, data, 0o600)
}

// ClearGitCredentials 删除 github.com 条目;文件为空/无条目时删除文件。
func ClearGitCredentials(home string) error {
	path := gitCredentialsPath(home)
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
