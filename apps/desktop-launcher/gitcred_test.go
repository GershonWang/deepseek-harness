package main

import (
	"os"
	"path/filepath"
	"testing"
)

func gitcredFile(home string) string { return filepath.Join(home, ".git-credentials") }

func TestGitCredentials_RoundTrip(t *testing.T) {
	home := t.TempDir()
	if err := WriteGitCredentials(home, "u1", "tok1"); err != nil {
		t.Fatalf("write: %v", err)
	}
	user, token, found := ReadGitCredentials(home)
	if !found || user != "u1" || token != "tok1" {
		t.Fatalf("read: user=%q token=%q found=%v", user, token, found)
	}
	data, _ := os.ReadFile(gitcredFile(home))
	if string(data) != "https://u1:tok1@github.com\n" {
		t.Fatalf("file content: %q", data)
	}
}

func TestGitCredentials_OverwriteAndClear(t *testing.T) {
	home := t.TempDir()
	_ = WriteGitCredentials(home, "u1", "tok1")
	_ = WriteGitCredentials(home, "u2", "tok2")
	if _, token, _ := ReadGitCredentials(home); token != "tok2" {
		t.Fatalf("overwrite failed: %q", token)
	}
	if err := ClearGitCredentials(home); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if _, _, found := ReadGitCredentials(home); found {
		t.Fatal("expected cleared")
	}
	if _, err := os.Stat(gitcredFile(home)); !os.IsNotExist(err) {
		t.Fatalf("file should be removed, stat err: %v", err)
	}
}

func TestGitCredentials_KeepsOtherHosts(t *testing.T) {
	home := t.TempDir()
	file := gitcredFile(home)
	if err := os.WriteFile(file, []byte("https://a:b@gitlab.example.com\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteGitCredentials(home, "u1", "tok1"); err != nil {
		t.Fatal(err)
	}
	user, token, found := ReadGitCredentials(home)
	if !found || user != "u1" || token != "tok1" {
		t.Fatalf("read: user=%q token=%q found=%v", user, token, found)
	}
	data, _ := os.ReadFile(file)
	if string(data) != "https://a:b@gitlab.example.com\nhttps://u1:tok1@github.com\n" {
		t.Fatalf("file content: %q", data)
	}
}
