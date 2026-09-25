package webviewperm

import "testing"

func TestAllowPermission(t *testing.T) {
	tests := []struct {
		name  string
		facts PermissionFacts
		want  bool
	}{
		{"仅麦克风", PermissionFacts{IsUserMedia: true, IsAudio: true}, true},
		{"仅摄像头", PermissionFacts{IsUserMedia: true, IsVideo: true}, false},
		{"麦克风加摄像头", PermissionFacts{IsUserMedia: true, IsAudio: true, IsVideo: true}, false},
		{"仅屏幕共享", PermissionFacts{IsUserMedia: true, IsDisplay: true}, false},
		{"麦克风加屏幕共享", PermissionFacts{IsUserMedia: true, IsAudio: true, IsDisplay: true}, false},
		{"三者都要", PermissionFacts{IsUserMedia: true, IsAudio: true, IsVideo: true, IsDisplay: true}, false},
		{"非媒体权限请求", PermissionFacts{}, false},
		{"有音频标志但非媒体请求", PermissionFacts{IsAudio: true}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := AllowPermission(tt.facts); got != tt.want {
				t.Fatalf("AllowPermission(%+v) = %v, 期望 %v", tt.facts, got, tt.want)
			}
		})
	}
}
