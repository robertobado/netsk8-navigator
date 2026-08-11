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
	root, mode, err := loadFlatConfig(path)
	if err != nil {
		return failResult(label, err)
	}

	serversJSON, err := mergedServersJSON(root, entry)
	if err != nil {
		return failResult(label, err)
	}
	root["mcpServers"] = serversJSON

	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return failResult(label, err)
	}

	if err := writeAtomicPreservingMode(path, out, mode); err != nil {
		return failResult(label, err)
	}
	return Result{Client: label, Status: "installed"}
}

// loadFlatConfig reads path's existing top-level JSON object (or a fresh
// empty one, at the default 0600 mode, if it doesn't exist yet) along with
// its current permission bits, for installFlatConfig to merge into.
func loadFlatConfig(path string) (map[string]json.RawMessage, os.FileMode, error) {
	mode := os.FileMode(0o600)
	root := map[string]json.RawMessage{}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return root, mode, nil
		}
		return nil, 0, err
	}
	mode = info.Mode().Perm()
	raw, err := os.ReadFile(path) //nolint:gosec // path is one of our own resolved client-config locations, not user input
	if err != nil {
		return nil, 0, fmt.Errorf("reading %s: %w", path, err)
	}
	if len(raw) == 0 {
		return root, mode, nil
	}
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, 0, fmt.Errorf("%s is not valid JSON: %w", path, err)
	}
	return root, mode, nil
}

// mergedServersJSON returns root's "mcpServers" object with entry merged in
// under serverName, marshaled back to raw JSON.
func mergedServersJSON(root map[string]json.RawMessage, entry Entry) (json.RawMessage, error) {
	servers := map[string]json.RawMessage{}
	if raw, ok := root["mcpServers"]; ok {
		if err := json.Unmarshal(raw, &servers); err != nil {
			return nil, fmt.Errorf("existing mcpServers is not an object: %w", err)
		}
	}
	entryJSON, err := json.MarshalIndent(struct {
		Command string   `json:"command"`
		Args    []string `json:"args"`
	}{entry.Command, entry.Args}, "", "  ")
	if err != nil {
		return nil, err
	}
	servers[serverName] = entryJSON
	return json.Marshal(servers)
}

func failResult(label string, err error) Result {
	return Result{Client: label, Status: "failed: " + err.Error()}
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
