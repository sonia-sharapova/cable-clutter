package main

import (
	"bytes"
	"strings"
	"testing"
)

// run executes cbl with args against a fresh library in a temp dir per test.
func run(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	root := newRoot()
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

func mustRun(t *testing.T, args ...string) string {
	t.Helper()
	out, err := run(t, args...)
	if err != nil {
		t.Fatalf("cbl %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return out
}

func TestInventoryCommands(t *testing.T) {
	t.Setenv("CBL_HOME", t.TempDir())

	out := mustRun(t, "add", "usb-a", "usb-c", "--qty", "4")
	if !strings.Contains(out, "Added 4 × USB-A to USB-C") {
		t.Errorf("add output: %s", out)
	}
	mustRun(t, "add", "ps2-avmulti", "rca*3", "--for", "PS2", "--where", "drawer 2", "--name", "grey ps2 av")
	mustRun(t, "add", "aux", "rca")
	mustRun(t, "adapter", "add", "us to eu plug adapter", "--qty", "3")

	out = mustRun(t, "ls")
	for _, want := range []string{"PlayStation AV cable", "grey ps2 av", "drawer 2", "PS2", "3.5 mm to RCA", "6 cables: 6 free, 0 reserved, 0 in use"} {
		if !strings.Contains(out, want) {
			t.Errorf("ls missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "US to EU") {
		t.Error("cable ls lists adapters")
	}
	out = mustRun(t, "ls", "--summary")
	if !strings.Contains(out, "usb-a:m|usb-c:m") || !strings.Contains(out, "4      4") {
		t.Errorf("summary:\n%s", out)
	}
	out = mustRun(t, "ls", "--type", "aux")
	if !strings.Contains(out, "1 cables") {
		t.Errorf("ls --type aux:\n%s", out)
	}
	out = mustRun(t, "adapter", "ls")
	if !strings.Contains(out, "3 adapters: 3 free") {
		t.Errorf("adapter ls:\n%s", out)
	}

	mustRun(t, "rm", "grey ps2 av")
	out = mustRun(t, "ls")
	if strings.Contains(out, "grey ps2 av") {
		t.Errorf("rm left the item:\n%s", out)
	}
}

func TestAddExplainsProblems(t *testing.T) {
	t.Setenv("CBL_HOME", t.TempDir())

	out, err := run(t, "add", "2-prong", "barrel")
	if err == nil || !strings.Contains(out, "AC adaptor 8.5 V") {
		t.Errorf("ambiguous add should list candidates; err=%v\n%s", err, out)
	}
	if _, err := run(t, "add", "usb-c hub with card reader"); err == nil || !strings.Contains(err.Error(), "cbl adapter add") {
		t.Errorf("adding an adapter as a cable should point to `cbl adapter add`, got %v", err)
	}
	if _, err := run(t, "add", "toaster", "usb-c"); err == nil {
		t.Error("unknown connector accepted")
	}
	if _, err := run(t, "rm", "nothing-here"); err == nil {
		t.Error("rm of a missing item succeeded")
	}
}
