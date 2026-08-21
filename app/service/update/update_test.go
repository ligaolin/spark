package update

import "testing"

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int // >0: a newer; <0: a older; 0: equal
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.0.1", "1.0.0", 1},
		{"1.0.0", "1.0.1", -1},
		{"1.1.0", "1.0.9", 1},
		{"2.0.0", "1.9.9", 1},
		{"v1.2.3", "1.2.2", 1},
		{"1.2.3", "v1.2.4", -1},
		// 预发布版本比正式版旧
		{"1.2.3", "1.2.3-beta.1", 1},
		{"1.2.3-beta.1", "1.2.3", -1},
		{"1.2.3-beta.2", "1.2.3-beta.1", 1},
		// dev 视为 0.0.0，任何正式发布都更新
		{"1.0.0", "dev", 1},
		{"dev", "1.0.0", -1},
		{"dev", "dev", 0},
		// 位数不同
		{"1.2", "1.2.0", 0},
		{"1.2.0.1", "1.2.0", 1},
	}
	for _, c := range cases {
		got := compareVersions(c.a, c.b)
		if c.want == 0 && got != 0 {
			t.Errorf("compareVersions(%q, %q) = %d, want 0", c.a, c.b, got)
		}
		if c.want > 0 && got <= 0 {
			t.Errorf("compareVersions(%q, %q) = %d, want >0", c.a, c.b, got)
		}
		if c.want < 0 && got >= 0 {
			t.Errorf("compareVersions(%q, %q) = %d, want <0", c.a, c.b, got)
		}
	}
}

func TestIsWindowsAsset(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"Spark-1.0.0-windows-amd64.exe", true},
		{"Spark-1.0.0-windows-amd64.exe.sig", false},
		{"Spark-1.0.0-linux-amd64", false},
		{"Spark-1.0.0-macos.zip", false},
		{"spark-WINDOWS-x64.exe", true},
	}
	for _, c := range cases {
		if got := isWindowsAsset(c.name); got != c.want {
			t.Errorf("isWindowsAsset(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}
