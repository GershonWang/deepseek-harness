package gitcred

import (
	"os"
	"testing"
)

func TestGitCredentials_RoundTrip(t *testing.T) {
	home := t.TempDir()
	if err := Write(home, "u1", "tok1"); err != nil {
		t.Fatalf("write: %v", err)
	}
	user, token, found := Read(home)
	if !found || user != "u1" || token != "tok1" {
		t.Fatalf("read: user=%q token=%q found=%v", user, token, found)
	}
	data, _ := os.ReadFile(Path(home))
	if string(data) != "https://u1:tok1@github.com\n" {
		t.Fatalf("file content: %q", data)
	}
}

func TestGitCredentials_OverwriteAndClear(t *testing.T) {
	home := t.TempDir()
	_ = Write(home, "u1", "tok1")
	_ = Write(home, "u2", "tok2")
	if _, token, _ := Read(home); token != "tok2" {
		t.Fatalf("overwrite failed: %q", token)
	}
	if err := Clear(home); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if _, _, found := Read(home); found {
		t.Fatal("expected cleared")
	}
	if _, err := os.Stat(Path(home)); !os.IsNotExist(err) {
		t.Fatalf("file should be removed, stat err: %v", err)
	}
}

func TestGitCredentials_KeepsOtherHosts(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(Path(home), []byte("https://a:b@gitlab.example.com\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Write(home, "u1", "tok1"); err != nil {
		t.Fatal(err)
	}
	user, token, found := Read(home)
	if !found || user != "u1" || token != "tok1" {
		t.Fatalf("read: user=%q token=%q found=%v", user, token, found)
	}
	data, _ := os.ReadFile(Path(home))
	if string(data) != "https://a:b@gitlab.example.com\nhttps://u1:tok1@github.com\n" {
		t.Fatalf("file content: %q", data)
	}
}

func TestGitCredentials_RejectsNewlineInjection(t *testing.T) {
	home := t.TempDir()
	// 令牌/用户名里的换行被剥离，不能注入伪造凭据行。
	if err := Write(home, "u1", "tok1\nhttps://evil:x@github.com"); err != nil {
		t.Fatalf("write: %v", err)
	}
	data, _ := os.ReadFile(Path(home))
	if string(data) != "https://u1:tok1https://evil:x@github.com@github.com\n" {
		t.Fatalf("file content: %q", data)
	}
}

func TestGitCredentials_Writes0600(t *testing.T) {
	home := t.TempDir()
	// 预置一个宽松权限的旧文件，写入后应被收紧到 0600。
	if err := os.WriteFile(Path(home), []byte("https://a:b@gitlab.example.com\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Write(home, "u1", "tok1"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(Path(home))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("expected 0600, got %o", perm)
	}
}
