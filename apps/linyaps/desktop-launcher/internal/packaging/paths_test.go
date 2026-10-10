package packaging

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadVersion(t *testing.T) {
	dir := t.TempDir()
	pkg := filepath.Join(dir, "package.json")
	if err := os.WriteFile(pkg, []byte(`{"name":"x","version":"1.2.3"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if v := readVersion(pkg); v != "1.2.3" {
		t.Fatalf("readVersion = %q, want 1.2.3", v)
	}
	if v := readVersion(filepath.Join(dir, "missing.json")); v != "" {
		t.Fatalf("readVersion(missing) = %q, want empty", v)
	}
	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte(`not json`), 0o644); err != nil {
		t.Fatal(err)
	}
	if v := readVersion(bad); v != "" {
		t.Fatalf("readVersion(bad json) = %q, want empty", v)
	}
}
