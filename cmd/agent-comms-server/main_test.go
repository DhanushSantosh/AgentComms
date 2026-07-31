package main

import "testing"

func TestParseMigrationCommandRequiresDatabaseURL(t *testing.T) {
	if _, err := parseMigrationCommand("", []string{"plan"}); err == nil {
		t.Fatal("expected an error when AGENT_COMMS_DATABASE_URL is empty")
	}
}

func TestParseMigrationCommandRequiresValidUsage(t *testing.T) {
	cases := [][]string{
		{},
		{"plan", "extra"},
		{"apply"},
		{"apply", "--yes"},
	}
	for _, args := range cases {
		if _, err := parseMigrationCommand("postgres://example", args); err == nil {
			t.Fatalf("expected a usage error for args %v", args)
		}
	}
}

func TestParseMigrationCommandRejectsUnknownSubcommand(t *testing.T) {
	if _, err := parseMigrationCommand("postgres://example", []string{"repair"}); err == nil {
		t.Fatal("expected an error for an unknown migration subcommand")
	}
}

func TestParseMigrationCommandPlanIsAlwaysAllowed(t *testing.T) {
	command, err := parseMigrationCommand("postgres://example", []string{"plan"})
	if err != nil {
		t.Fatal(err)
	}
	if command != "plan" {
		t.Fatalf("command=%q, want plan", command)
	}
}

func TestParseMigrationCommandApplyRequiresBothFlags(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"missing both", []string{"apply", "--yes", "--other"}},
		{"missing allow-disruptive", []string{"apply", "--yes", "--maybe"}},
		{"missing yes", []string{"apply", "--maybe", "--allow-disruptive"}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := parseMigrationCommand("postgres://example", testCase.args); err == nil {
				t.Fatalf("expected apply to be rejected without both required flags: %v", testCase.args)
			}
		})
	}
}

func TestParseMigrationCommandApplySucceedsWithBothFlagsInEitherOrder(t *testing.T) {
	for _, args := range [][]string{
		{"apply", "--yes", "--allow-disruptive"},
		{"apply", "--allow-disruptive", "--yes"},
	} {
		command, err := parseMigrationCommand("postgres://example", args)
		if err != nil {
			t.Fatalf("args %v: unexpected error: %v", args, err)
		}
		if command != "apply" {
			t.Fatalf("args %v: command=%q, want apply", args, command)
		}
	}
}
