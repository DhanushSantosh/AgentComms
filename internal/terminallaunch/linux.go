//go:build linux

package terminallaunch

import (
	"fmt"
	"os/exec"
	"strings"
)

// linuxEmulator describes one supported terminal emulator: its executable
// name (looked up in PATH) and how to build the full argv -- executable
// plus flags -- for running argv inside dir as the new window's working
// directory. Order matters: earlier entries are tried first.
//
// x-terminal-emulator (Debian/Ubuntu's update-alternatives symlink to
// whichever terminal is configured as default) is tried first since it
// best reflects an explicit operator choice when present; the rest follow
// in roughly descending popularity.
var linuxEmulators = []struct {
	name  string
	build func(dir string, argv []string) []string
}{
	{"x-terminal-emulator", func(dir string, argv []string) []string {
		return append([]string{"x-terminal-emulator", "-e"}, argv...)
	}},
	{"gnome-terminal", func(dir string, argv []string) []string {
		return append([]string{"gnome-terminal", "--working-directory=" + dir, "--"}, argv...)
	}},
	{"konsole", func(dir string, argv []string) []string {
		return append([]string{"konsole", "--workdir", dir, "-e"}, argv...)
	}},
	{"xfce4-terminal", func(dir string, argv []string) []string {
		return append([]string{"xfce4-terminal", "--working-directory=" + dir, "-x"}, argv...)
	}},
	{"kitty", func(dir string, argv []string) []string {
		return append([]string{"kitty", "--directory", dir}, argv...)
	}},
	{"foot", func(dir string, argv []string) []string {
		return append([]string{"foot", "--working-directory=" + dir}, argv...)
	}},
	{"alacritty", func(dir string, argv []string) []string {
		return append([]string{"alacritty", "--working-directory", dir, "-e"}, argv...)
	}},
	{"wezterm", func(dir string, argv []string) []string {
		return append([]string{"wezterm", "start", "--cwd", dir, "--"}, argv...)
	}},
	{"xterm", func(dir string, argv []string) []string {
		return append([]string{"xterm", "-e"}, argv...)
	}},
}

// selectEmulator returns the first emulator in preference order whose
// executable lookup succeeds via lookPath, so the search order itself can
// be tested without any real terminal emulators installed.
func selectEmulator(lookPath func(string) (string, error)) (int, error) {
	var tried []string
	for i, e := range linuxEmulators {
		if _, err := lookPath(e.name); err == nil {
			return i, nil
		}
		tried = append(tried, e.name)
	}
	return -1, fmt.Errorf("no supported terminal emulator found in PATH (tried: %s)", strings.Join(tried, ", "))
}

// Open launches argv inside a new terminal-emulator window rooted at dir.
// It does not wait for the window to exit -- the caller's own process is
// expected to return immediately, leaving the new window as the dedicated
// session.
func Open(dir string, argv []string) error {
	if len(argv) == 0 {
		return fmt.Errorf("no command to launch")
	}
	i, err := selectEmulator(exec.LookPath)
	if err != nil {
		return err
	}
	full := linuxEmulators[i].build(dir, argv)
	cmd := exec.Command(full[0], full[1:]...)
	cmd.Dir = dir
	return cmd.Start()
}
