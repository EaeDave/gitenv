package update

import "testing"

func TestIsNewer(t *testing.T) {
	cases := []struct {
		current, latest string
		want            bool
	}{
		{"v0.1.0", "v0.2.0", true},
		{"v0.2.0", "v0.1.0", false},
		{"0.1.0", "0.1.0", false},
		{"v1.0.0", "v1.0.1", true},
		{"v1.2.3", "v1.10.0", true},
		{"v0.1.0-rc1", "v0.1.0", false},
		{"dev", "v0.1.0", false},
		{"v0.1.0", "not-a-version", false},
	}
	for _, tc := range cases {
		if got := IsNewer(tc.current, tc.latest); got != tc.want {
			t.Errorf("IsNewer(%q, %q) = %v, want %v", tc.current, tc.latest, got, tc.want)
		}
	}
}

func TestIsRelease(t *testing.T) {
	for _, v := range []string{"v0.1.0", "1.2.3", "v2.0.0-beta"} {
		if !IsRelease(v) {
			t.Errorf("IsRelease(%q) = false, want true", v)
		}
	}
	for _, v := range []string{"dev", "", "latest"} {
		if IsRelease(v) {
			t.Errorf("IsRelease(%q) = true, want false", v)
		}
	}
}

func TestChecksumFor(t *testing.T) {
	sums := "aaa111  gitenv_linux_amd64\nbbb222  gitenv_windows_amd64.exe\n"
	if got, ok := checksumFor(sums, "gitenv_windows_amd64.exe"); !ok || got != "bbb222" {
		t.Fatalf("checksumFor windows = %q, %v", got, ok)
	}
	if got, ok := checksumFor(sums, "gitenv_darwin_arm64"); ok {
		t.Fatalf("checksumFor missing asset returned %q, %v", got, ok)
	}
}
