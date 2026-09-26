// 工具 ID 改名迁移的测试。
//
// 迁移会真实移动用户磁盘上的目录与软链，出错就是丢数据或留下无法回收的孤儿安装，
// 因此这里逐条钉住它的行为契约：目录搬家、软链重建、幂等、目标已存在时不覆盖。
package toolchain

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMigrateLegacyToolIDs_MovesDirAndLink 固定迁移的主路径：旧 ID 的版本目录搬到
// 新 ID 名下，current 软链也随之指向新位置，并且旧软链被清掉（否则它会一直指着
// 已不存在的旧路径，成为自愈每次启动都要处理一次的坏链）。
func TestMigrateLegacyToolIDs_MovesDirAndLink(t *testing.T) {
	dir := t.TempDir()
	mkToolVersion(t, dir, "jdk21", "21.0.12.1", "java")
	if err := SetActiveVersion(dir, "jdk21", "21.0.12.1"); err != nil {
		t.Fatal(err)
	}

	done := MigrateLegacyToolIDs(dir)
	if len(done) == 0 {
		t.Fatal("有旧 ID 安装时应报告迁移结果")
	}

	if !IsInstalled(dir, "jdk", "21.0.12.1") {
		t.Fatalf("版本目录应搬到新 ID 名下；新 ID 已装版本=%v", ListVersions(dir, "jdk"))
	}
	if got := ListVersions(dir, "jdk21"); len(got) != 0 {
		t.Fatalf("旧 ID 名下不应残留版本目录: %v", got)
	}
	if got := ActiveVersion(dir, "jdk"); got != "21.0.12.1" {
		t.Fatalf("新 ID 的激活版本应为 21.0.12.1, got %q", got)
	}
	if _, err := os.Lstat(currentLink(dir, "jdk21")); !os.IsNotExist(err) {
		t.Fatal("旧 ID 的 current 软链应被清除，避免留下坏链")
	}
}

// TestMigrateLegacyToolIDs_Idempotent 固定幂等性：第二次调用不得再搬运（目录已不在
// 旧 ID 名下），也不得删除或覆盖新 ID 下已有的安装。启动流程每次都会调迁移，不幂等
// 就等于每次启动都动一遍用户的安装目录。
func TestMigrateLegacyToolIDs_Idempotent(t *testing.T) {
	dir := t.TempDir()
	mkToolVersion(t, dir, "jdk21", "21.0.12.1", "java")
	if err := SetActiveVersion(dir, "jdk21", "21.0.12.1"); err != nil {
		t.Fatal(err)
	}
	MigrateLegacyToolIDs(dir)
	first := ListVersions(dir, "jdk")

	second := MigrateLegacyToolIDs(dir)
	if len(second) != 0 {
		t.Fatalf("已迁移完成后再次调用应无事可做, got %v", second)
	}
	if got := ListVersions(dir, "jdk"); len(got) != len(first) {
		t.Fatalf("重复调用不应改变已装版本: %v -> %v", first, got)
	}
	if got := ActiveVersion(dir, "jdk"); got != "21.0.12.1" {
		t.Fatalf("重复调用不应改变激活版本, got %q", got)
	}
}

// TestMigrateLegacyToolIDs_KeepsExistingTarget 固定冲突取舍：新 ID 下已存在同名版本时
// 保留既有那份并跳过。两个目录都是完整安装，覆盖其中任何一个都会让用户失去当前可用的
// 那一份；保留现役版本、把旧的那份留在原地由用户自行清理，是唯一不丢数据的选择。
func TestMigrateLegacyToolIDs_KeepsExistingTarget(t *testing.T) {
	dir := t.TempDir()
	mkToolVersion(t, dir, "jdk21", "21.0.12.1", "java")
	mkToolVersion(t, dir, "jdk", "21.0.12.1", "java")
	// 在新 ID 那份里放一个标记文件，用于证明它没被旧 ID 的内容覆盖。
	marker := filepath.Join(versionDir(dir, "jdk", "21.0.12.1"), "marker")
	if err := os.WriteFile(marker, []byte("existing"), 0o644); err != nil {
		t.Fatal(err)
	}

	done := MigrateLegacyToolIDs(dir)
	if len(done) == 0 {
		t.Fatal("目标已存在时也应报告结果，让日志能解释为何没搬")
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("新 ID 下既有的安装不应被覆盖: %v", err)
	}
	if !IsInstalled(dir, "jdk21", "21.0.12.1") {
		t.Fatal("目标已存在时应保留旧 ID 那份原地不动，而不是删除它")
	}
}

// TestMigrateLegacyToolIDs_NoLegacyInstall 固定无旧安装时的行为：不做任何事、不报错。
// 全新安装的用户没有 jdk21 目录，迁移必须静默略过，而不是报一条无意义的「迁移失败」。
func TestMigrateLegacyToolIDs_NoLegacyInstall(t *testing.T) {
	dir := t.TempDir()
	mkToolVersion(t, dir, "jdk", "21.0.12.1", "java")

	if done := MigrateLegacyToolIDs(dir); len(done) != 0 {
		t.Fatalf("无旧 ID 安装时不应报告任何迁移, got %v", done)
	}
	if got := ListVersions(dir, "jdk"); len(got) != 1 || got[0] != "21.0.12.1" {
		t.Fatalf("新 ID 的安装不应被动到: %v", got)
	}
}

// TestMigrateLegacyToolIDs_MultipleVersions 固定多版本搬迁：旧 ID 名下装了多个版本时
// 每个都要搬，不能只搬激活的那一个——未激活的版本同样是用户磁盘上的有效安装。
func TestMigrateLegacyToolIDs_MultipleVersions(t *testing.T) {
	dir := t.TempDir()
	for _, v := range []string{"21.0.12.1", "8u504"} {
		mkToolVersion(t, dir, "jdk21", v, "java")
	}
	if err := SetActiveVersion(dir, "jdk21", "8u504"); err != nil {
		t.Fatal(err)
	}

	MigrateLegacyToolIDs(dir)

	got := ListVersions(dir, "jdk")
	if len(got) != 2 {
		t.Fatalf("两个版本都应搬到新 ID 名下, got %v", got)
	}
	for _, v := range []string{"21.0.12.1", "8u504"} {
		if !IsInstalled(dir, "jdk", v) {
			t.Fatalf("版本 %s 应已搬入新 ID", v)
		}
	}
	if got := ActiveVersion(dir, "jdk"); got != "8u504" {
		t.Fatalf("激活版本应随迁移保留为 8u504, got %q", got)
	}
}
