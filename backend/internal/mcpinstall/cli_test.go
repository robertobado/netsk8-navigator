package mcpinstall

import "testing"

func TestHasFlag(t *testing.T) {
	if !HasFlag([]string{"--allow-write"}, "--allow-write") {
		t.Error("want true when the flag is present")
	}
	if HasFlag([]string{"--other"}, "--allow-write") {
		t.Error("want false when the flag is absent")
	}
	if HasFlag(nil, "--allow-write") {
		t.Error("want false for nil args")
	}
}

// RunCLI's usage-error and SelfStdioEntry-error branches both call
// os.Exit/log.Fatalf, which would kill the test process — only the
// successful "install" path is exercised here.
func TestRunCLI_Install(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("APPDATA", home)
	t.Setenv("USERPROFILE", home)

	RunCLI([]string{"install"}) // must not panic or exit
	RunCLI([]string{"install", "--allow-write"})
}
