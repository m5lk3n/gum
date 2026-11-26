package server

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReplaced(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "gum")
	if err := os.WriteFile(bin, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	// running stands in for /proc/self/exe: it keeps referring to the old file.
	running := filepath.Join(dir, "running")
	if err := os.Link(bin, running); err != nil {
		t.Fatal(err)
	}
	if replaced(running, bin) {
		t.Fatal("unchanged binary reported as replaced")
	}

	// Replace the binary the way dpkg does: write a new file, rename it over the old one.
	tmp := filepath.Join(dir, "gum.dpkg-new")
	if err := os.WriteFile(tmp, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, bin); err != nil {
		t.Fatal(err)
	}
	if !replaced(running, bin) {
		t.Fatal("replaced binary not reported")
	}

	if err := os.Remove(bin); err != nil {
		t.Fatal(err)
	}
	if replaced(running, bin) {
		t.Fatal("removed binary reported as replaced")
	}
}

func TestRestartPendingSelf(t *testing.T) {
	// The test binary itself hasn't been replaced.
	if restartPending() {
		t.Fatal("restart reported as pending for the running test binary")
	}
}
