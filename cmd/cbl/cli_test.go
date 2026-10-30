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

func TestShow(t *testing.T) {
	t.Setenv("CBL_HOME", t.TempDir())
	mustRun(t, "add", "ps2-avmulti", "rca*3", "--name", "grey ps2 av", "--for", "PS2", "--where", "drawer 2")

	out := mustRun(t, "show", "grey ps2 av")
	for _, want := range []string{"PlayStation AV Multi Out (M)", "(o) yellow  video", "grey ps2 av", "drawer 2", "for PS2", "FREE", "Carries: Composite video"} {
		if !strings.Contains(out, want) {
			t.Errorf("show item missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "\x1b[") {
		t.Error("colour codes written to a non-terminal")
	}

	out = mustRun(t, "show", "aux", "rca")
	if !strings.Contains(out, "3.5 mm to RCA stereo breakout") || !strings.Contains(out, "You don't own one.") {
		t.Errorf("show type:\n%s", out)
	}
	out = mustRun(t, "show", "usb-c hub with card reader")
	if !strings.Contains(out, "SD card (F)") {
		t.Errorf("show adapter type:\n%s", out)
	}
	if _, err := run(t, "show", "toaster"); err == nil {
		t.Error("show of an unknown thing succeeded")
	}
}

func TestDevice(t *testing.T) {
	t.Setenv("CBL_HOME", t.TempDir())
	mustRun(t, "add", "ps2-avmulti", "rca*3", "--for", "PS2")
	mustRun(t, "add", "2-prong", "barrel", "--variant", "5v-2a", "--for", "PSP")

	out := mustRun(t, "device", "add", "ps2", "--name", "PS2")
	if !strings.Contains(out, `Set the model of "PS2"`) {
		t.Errorf("device add on a placeholder: %s", out)
	}
	out = mustRun(t, "device", "PS2")
	for _, want := range []string{"Sony PlayStation 2 (slim)", `your "PS2"`, "AV Multi Out", "✓ PlayStation AV cable", "dedicated",
		"Can't yet: Component video", "⚠ Don't use AC adaptor 5 V 2 A (PSP)", "Ethernet → Ethernet cable"} {
		if !strings.Contains(out, want) {
			t.Errorf("device PS2 missing %q:\n%s", want, out)
		}
	}
	// PSP is still a placeholder: fall back to the catalog with a hint.
	out = mustRun(t, "device", "psp")
	if !strings.Contains(out, "cbl device add psp --name") {
		t.Errorf("placeholder hint missing:\n%s", out)
	}
	out = mustRun(t, "device", "crt")
	if !strings.Contains(out, "Ports not confirmed") {
		t.Errorf("unconfirmed note missing:\n%s", out)
	}
	out = mustRun(t, "device", "ls")
	if !strings.Contains(out, "Sony PlayStation 2 (slim)") || !strings.Contains(out, "(none yet)") {
		t.Errorf("device ls:\n%s", out)
	}
	if _, err := run(t, "device", "toaster"); err == nil {
		t.Error("unknown device accepted")
	}
}

func TestSystems(t *testing.T) {
	t.Setenv("CBL_HOME", t.TempDir())
	mustRun(t, "add", "hdmi", "hdmi", "--name", "grey hdmi")
	mustRun(t, "device", "add", "mac-mini-m2-2023", "--name", "Mac mini")
	mustRun(t, "device", "add", "apple-tv-hd", "--name", "Apple TV")
	mustRun(t, "system", "new", "Desk")
	mustRun(t, "system", "new", "Lounge")

	out := mustRun(t, "system", "add-link", "Desk", "grey hdmi", "--from", "Mac mini")
	if !strings.Contains(out, "Mac mini · HDMI") { // the only port it fits
		t.Errorf("port not filled in: %s", out)
	}
	mustRun(t, "system", "add-link", "Lounge", "hdmi", "hdmi", "--from", "Apple TV:HDMI")
	if _, err := run(t, "system", "add-link", "Lounge", "hdmi", "hdmi", "--from", "Apple TV:Ethernet"); err == nil {
		t.Error("HDMI accepted into an Ethernet port")
	}
	out = mustRun(t, "system", "ls")
	if !strings.Contains(out, `not with "Lounge"`) {
		t.Errorf("conflict not shown:\n%s", out)
	}

	out = mustRun(t, "system", "up", "Desk")
	if !strings.Contains(out, "✓ HDMI cable") {
		t.Errorf("up: %s", out)
	}
	if out := mustRun(t, "ls", "--in-use"); !strings.Contains(out, "grey hdmi") {
		t.Errorf("cable not in use after up:\n%s", out)
	}
	if _, err := run(t, "rm", "grey hdmi"); err == nil {
		t.Error("removed a cable that is in use")
	}

	// The one HDMI cable is busy: Lounge can't come up...
	out, err := run(t, "system", "up", "Lounge")
	if err == nil || !strings.Contains(out, `in use by "Desk"`) || !strings.Contains(out, "--switch") {
		t.Errorf("conflict not explained; err=%v\n%s", err, out)
	}
	// ...unless it switches Desk off, or wishes for another cable.
	out, err = run(t, "system", "up", "Lounge", "--wish")
	if err == nil || !strings.Contains(out, "Added 1 × HDMI cable to your wishlist") {
		t.Errorf("--wish: err=%v\n%s", err, out)
	}
	out = mustRun(t, "system", "up", "Lounge", "--switch")
	if !strings.Contains(out, `Took "Desk" down.`) || !strings.Contains(out, `"Lounge" is up`) {
		t.Errorf("--switch:\n%s", out)
	}

	// A second cable lets both be up; Desk's pinned one is busy, so it swaps.
	mustRun(t, "add", "hdmi", "hdmi")
	out = mustRun(t, "system", "up", "Desk")
	if !strings.Contains(out, `busy in "Lounge"`) {
		t.Errorf("swap not reported:\n%s", out)
	}
	mustRun(t, "system", "down", "Lounge")
	mustRun(t, "system", "down", "Desk")
	if out := mustRun(t, "ls", "--in-use"); !strings.Contains(out, "No cables match") {
		t.Errorf("cables still in use after down:\n%s", out)
	}
	out = mustRun(t, "system", "show", "Desk")
	if !strings.Contains(out, "would use") || !strings.Contains(out, "pinned") {
		t.Errorf("show:\n%s", out)
	}
	mustRun(t, "system", "unlink", "Lounge", "1")
	mustRun(t, "system", "rm", "Lounge")
}
