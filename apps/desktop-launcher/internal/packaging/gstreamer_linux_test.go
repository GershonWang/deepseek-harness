//go:build linux

package packaging

import (
	"os"
	"testing"
)

func TestGStreamerPluginPath(t *testing.T) {
	sep := string(os.PathListSeparator)
	tests := []struct {
		name     string
		existing string
		dir      string
		want     string
	}{
		{"环境未设置时直接用包内目录", "", "/pkg/gst", "/pkg/gst"},
		{"已有取值时追加在后面", "/usr/lib/gst", "/pkg/gst", "/pkg/gst" + sep + "/usr/lib/gst"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := gstreamerPluginPath(tt.existing, tt.dir); got != tt.want {
				t.Fatalf("gstreamerPluginPath(%q, %q) = %q, 期望 %q", tt.existing, tt.dir, got, tt.want)
			}
		})
	}
}
