package clipboard

import "testing"

func TestUriToPathPercentDecode(t *testing.T) {
	cases := []struct{ in, want string }{
		{"file:///home/Jokul/Desktop/x.png", "/home/Jokul/Desktop/x.png"},
		// DDE 文管对带空格/括号的文件名做百分号编码：空格 %20、括号 %28/%29。
		{"file:///home/Jokul/Downloads/%E7%81%AB%E5%B1%B1%E5%BC%95%E6%93%8E%E9%82%80%E8%AF%B7%E6%B5%B7%E6%8A%A5%20%281%29.png",
			"/home/Jokul/Downloads/火山引擎邀请海报 (1).png"},
		{"file:///tmp/a%20b%28c%29.png", "/tmp/a b(c).png"},
		{"file:///tmp/a+b.png", "/tmp/a+b.png"},            // '+' 不转空格
		{"file:///tmp/%25.png", "/tmp/%.png"},              // 百分号自身
		{"file://localhost/home/u/x.png", "/home/u/x.png"}, // localhost 主机
		{"not a uri", ""},
		{"http://example.com/x.png", ""},
		{"file://otherhost/share/x.png", ""},                // 远程主机拒绝
		{"file:///home/u/bad%zz.png", "/home/u/bad%zz.png"}, // 非法转义原样保留
	}
	for _, c := range cases {
		if got := uriToPath(c.in); got != c.want {
			t.Errorf("uriToPath(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
