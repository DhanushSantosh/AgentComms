package interactiveserve

import "testing"

func TestCommandNameStripsWindowsExecutableSpelling(t *testing.T) {
	cases := []struct {
		path, goos, want string
	}{
		{"claude", "windows", "claude"},
		{"claude.exe", "windows", "claude"},
		{`C:\Users\me\.local\bin\claude.exe`, "windows", "claude"},
		{`C:\Users\me\AppData\Roaming\npm\opencode.cmd`, "windows", "opencode"},
		{"Codex.EXE", "windows", "codex"},
		{"C:/tools/codex.bat", "windows", "codex"},
		{"claude", "linux", "claude"},
		{"/usr/local/bin/claude", "linux", "claude"},
		// On unix an extension is part of the name and a backslash is an
		// ordinary filename character.
		{"/opt/claude.exe", "linux", "claude.exe"},
		{`weird\name`, "darwin", `weird\name`},
	}
	for _, tc := range cases {
		if got := commandNameFor(tc.path, tc.goos); got != tc.want {
			t.Errorf("commandNameFor(%q, %s) = %q, want %q", tc.path, tc.goos, got, tc.want)
		}
	}
}
