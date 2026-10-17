package main

import "testing"

// These tests cover only the paths that touch nothing: the refusal, the
// usage errors and the help. `run` with --i-am-the-user would run the
// Task Suite (taking over the screen); it is user-started only and is
// never exercised by a test.

func TestRunCmdRefusesWithoutTheUserStart(t *testing.T) {
	// No flag: refused before anything runs.
	if code := runCmd(nil); code != 1 {
		t.Errorf("runCmd without --i-am-the-user = %d, want 1 (refusal)", code)
	}
	// Other flags do not smuggle a start past the refusal.
	if code := runCmd([]string{"--harness", "pi"}); code != 1 {
		t.Errorf("runCmd with flags but no user start = %d, want 1", code)
	}
}

func TestRunCmdUsageErrors(t *testing.T) {
	cases := [][]string{
		{"--bogus"},
		{"--harness"},
		{"--time-limit"},
		{"--time-limit", "not-a-duration"},
		{"--max-turns", "0"},
	}
	for _, args := range cases {
		if code := runCmd(args); code != 2 {
			t.Errorf("runCmd(%v) = %d, want 2 (usage error)", args, code)
		}
	}
}

func TestRunUnknownCommand(t *testing.T) {
	if code := run([]string{"nope"}); code != 2 {
		t.Errorf("unknown command = %d, want 2", code)
	}
	if code := run(nil); code != 2 {
		t.Errorf("no command = %d, want 2", code)
	}
}

func TestRunHelp(t *testing.T) {
	if code := run([]string{"help"}); code != 0 {
		t.Errorf("help = %d, want 0", code)
	}
}
