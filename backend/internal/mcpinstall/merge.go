package mcpinstall

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// configDirIfExists returns the full path to base/subdir/filename, and
// whether base/subdir itself already exists — used as a cheap "is this
// client installed on this machine at all" signal, so we skip rather than
// create a config directory for a client the user doesn't have.
func configDirIfExists(base, subdir, filename string) (string, bool) {
	if base == "" {
		return "", false
	}
	dir := filepath.Join(base, subdir)
	info, err := os.Stat(dir) //nolint:gosec // dir is built from an OS-resolved home dir + our own fixed subdir names, not user input
	if err != nil || !info.IsDir() {
		return "", false
	}
	return filepath.Join(dir, filename), true
}

// installFlatConfig merges entry into path's top-level "mcpServers" map. It
// decodes the rest of the file as raw JSON so every unrelated top-level key
// round-trips byte-for-byte untouched, and preserves the file's existing
// permission bits (0600 for a freshly-created file) — the two failure modes
// named explicitly in the feedback this implements: overwriting unrelated
// state, and loosening permissions on a file that may hold credentials.
func installFlatConfig(label, path string, entry Entry) Result {
	mode := os.FileMode(0o600)
	root := map[string]json.RawMessage{}
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
		raw, err := os.ReadFile(path) //nolint:gosec // path is one of our own resolved client-config locations, not user input
		if err != nil {
			return Result{Client: label, Status: "failed: reading " + path + ": " + err.Error()}
		}
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &root); err != nil {
				return Result{Client: label, Status: "failed: " + path + " is not valid JSON: " + err.Error()}
			}
		}
	} else if !os.IsNotExist(err) {
		return Result{Client: label, Status: "failed: " + err.Error()}
	}

	servers := map[string]json.RawMessage{}
	if raw, ok := root["mcpServers"]; ok {
		if err := json.Unmarshal(raw, &servers); err != nil {
			return Result{Client: label, Status: "failed: existing mcpServers is not an object: " + err.Error()}
		}
	}

	entryJSON, err := json.MarshalIndent(struct {
		Command string   `json:"command"`
		Args    []string `json:"args"`
	}{entry.Command, entry.Args}, "", "  ")
	if err != nil {
		return Result{Client: label, Status: "failed: " + err.Error()}
	}
	servers[serverName] = entryJSON

	serversJSON, err := json.Marshal(servers)
	if err != nil {
		return Result{Client: label, Status: "failed: " + err.Error()}
	}
	root["mcpServers"] = serversJSON

	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return Result{Client: label, Status: "failed: " + err.Error()}
	}

	if err := writeAtomicPreservingMode(path, out, mode); err != nil {
		return Result{Client: label, Status: "failed: " + err.Error()}
	}
	return Result{Client: label, Status: "installed"}
}

// writeAtomicPreservingMode writes data to path via a temp-file-then-rename
// (matching internal/config.Store.save()'s own pattern), explicitly setting
// mode on the temp file first so the final file never transiently exists at
// whatever the process umask would otherwise produce.
func writeAtomicPreservingMode(path string, data []byte, mode os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	if err := os.Chmod(tmp, mode); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("renaming %s: %w", tmp, err)
	}
	return nil
}
