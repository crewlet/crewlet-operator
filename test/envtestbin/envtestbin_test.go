package envtestbin

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestDirPicksTheNewestReleaseForThisPlatform(t *testing.T) {
	platform := "-" + runtime.GOOS + "-" + runtime.GOARCH
	otherPlatform := "-plan9-mips"
	for _, c := range []struct {
		name string
		dirs []string
		want string
	}{
		{name: "no directory", want: ""},
		{name: "one release", dirs: []string{"1.37.0" + platform}, want: "1.37.0" + platform},
		{
			// Lexical order puts 1.36.10 before 1.36.9 and 1.9.0 after
			// 1.37.0.
			name: "the newest of several, not the first",
			dirs: []string{"1.36.10" + platform, "1.37.0" + platform, "1.36.9" + platform, "1.9.0" + platform},
			want: "1.37.0" + platform,
		},
		{
			name: "another platform's binaries are not used",
			dirs: []string{"1.36.0" + platform, "1.37.0" + otherPlatform},
			want: "1.36.0" + platform,
		},
		{
			name: "a name that is not a version is not used",
			dirs: []string{"latest" + platform, "1.36.0" + platform},
			want: "1.36.0" + platform,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			base := t.TempDir()
			for _, d := range c.dirs {
				if err := os.Mkdir(filepath.Join(base, d), 0o750); err != nil {
					t.Fatal(err)
				}
			}
			got, err := Dir(base)
			if err != nil {
				t.Fatal(err)
			}
			want := ""
			if c.want != "" {
				want = filepath.Join(base, c.want)
			}
			if got != want {
				t.Errorf("Dir() = %q, want %q", got, want)
			}
		})
	}
}

func TestDirOfAMissingDirectoryIsEmpty(t *testing.T) {
	got, err := Dir(filepath.Join(t.TempDir(), "absent"))
	if err != nil || got != "" {
		t.Errorf("Dir() = %q, %v; want \"\", nil", got, err)
	}
}
