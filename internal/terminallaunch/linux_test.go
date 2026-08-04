//go:build linux

package terminallaunch

import (
	"errors"
	"reflect"
	"testing"
)

func fakeLookPath(available ...string) func(string) (string, error) {
	set := make(map[string]bool, len(available))
	for _, a := range available {
		set[a] = true
	}
	return func(name string) (string, error) {
		if set[name] {
			return "/usr/bin/" + name, nil
		}
		return "", errors.New("not found")
	}
}

func TestSelectEmulatorPrefersEarlierEntries(t *testing.T) {
	i, err := selectEmulator(fakeLookPath("xterm", "kitty"))
	if err != nil {
		t.Fatal(err)
	}
	if linuxEmulators[i].name != "kitty" {
		t.Fatalf("expected kitty (earlier in preference order than xterm), got %s", linuxEmulators[i].name)
	}
}

func TestSelectEmulatorErrorsWithNothingAvailable(t *testing.T) {
	_, err := selectEmulator(fakeLookPath())
	if err == nil {
		t.Fatal("expected an error when no emulator is available")
	}
}

func TestLinuxEmulatorArgvIncludesDirAndCommand(t *testing.T) {
	cases := []struct {
		name string
		want []string
	}{
		{"gnome-terminal", []string{"gnome-terminal", "--working-directory=/proj", "--", "agent-comms", "runtime", "interactive-serve"}},
		{"konsole", []string{"konsole", "--workdir", "/proj", "-e", "agent-comms", "runtime", "interactive-serve"}},
		{"kitty", []string{"kitty", "--directory", "/proj", "agent-comms", "runtime", "interactive-serve"}},
		{"xterm", []string{"xterm", "-e", "agent-comms", "runtime", "interactive-serve"}},
	}
	argv := []string{"agent-comms", "runtime", "interactive-serve"}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var build func(dir string, argv []string) []string
			for _, e := range linuxEmulators {
				if e.name == c.name {
					build = e.build
				}
			}
			if build == nil {
				t.Fatalf("no emulator registered named %s", c.name)
			}
			got := build("/proj", argv)
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("got %v, want %v", got, c.want)
			}
		})
	}
}

func TestOpenRejectsEmptyArgv(t *testing.T) {
	if err := Open("/proj", nil); err == nil {
		t.Fatal("expected an error for an empty command")
	}
}
