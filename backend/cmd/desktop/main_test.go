package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestPrintUsage(t *testing.T) {
	var buf bytes.Buffer
	printUsage(&buf)
	out := buf.String()
	for _, want := range []string{"--mcp-stdio", "--mcp-allow-write", "mcp install", "--allow-write", "--version", "--help"} {
		if !strings.Contains(out, want) {
			t.Errorf("usage text missing %q:\n%s", want, out)
		}
	}
}
