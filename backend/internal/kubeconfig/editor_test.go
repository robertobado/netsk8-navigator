package kubeconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// writeTestKubeconfig writes a small but realistic multi-context kubeconfig
// (two clusters, two users — one token-based, one cert-based, so masking
// coverage exercises both secret shapes) to path.
func writeTestKubeconfig(t *testing.T, path string) {
	t.Helper()
	cfg := clientcmdapi.NewConfig()
	cfg.Clusters["cluster-a"] = &clientcmdapi.Cluster{Server: "https://a.example.com"}
	cfg.Clusters["cluster-b"] = &clientcmdapi.Cluster{Server: "https://b.example.com"}
	cfg.AuthInfos["user-a"] = &clientcmdapi.AuthInfo{Token: "super-secret-token"}
	cfg.AuthInfos["user-b"] = &clientcmdapi.AuthInfo{ClientCertificateData: []byte("cert-bytes"), ClientKeyData: []byte("key-bytes")}
	cfg.Contexts["ctx-a"] = &clientcmdapi.Context{Cluster: "cluster-a", AuthInfo: "user-a", Namespace: "default"}
	cfg.Contexts["ctx-b"] = &clientcmdapi.Context{Cluster: "cluster-b", AuthInfo: "user-b", Namespace: "other"}
	cfg.CurrentContext = "ctx-a"
	if err := clientcmd.WriteToFile(*cfg, path); err != nil {
		t.Fatal(err)
	}
}

// newTestEditor writes a fresh test kubeconfig to a temp file, points
// $KUBECONFIG at it (t.Setenv scopes this to the test), and builds an Editor
// against it.
func newTestEditor(t *testing.T) (ed *Editor, path string) {
	t.Helper()
	path = filepath.Join(t.TempDir(), "config")
	writeTestKubeconfig(t, path)
	t.Setenv("KUBECONFIG", path)
	ed, err := NewEditor()
	if err != nil {
		t.Fatal(err)
	}
	return ed, path
}

// A nonexistent kubeconfig path is not itself an error — clientcmd treats
// it as a legitimate empty starting point (the same reason `kubectl config
// set-context` works before ~/.kube/config exists). NewEditor only fails on
// a file that exists but doesn't parse.
func TestNewEditor_FailsOnMalformedKubeconfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(path, []byte("not: [valid: yaml"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KUBECONFIG", path)
	if _, err := NewEditor(); err == nil {
		t.Fatal("expected an error for a malformed kubeconfig")
	}
}

func TestView_MasksSecrets(t *testing.T) {
	ed, _ := newTestEditor(t)
	v, err := ed.View()
	if err != nil {
		t.Fatal(err)
	}
	if v.CurrentContext != "ctx-a" {
		t.Errorf("CurrentContext = %q, want ctx-a", v.CurrentContext)
	}
	if len(v.Contexts) != 2 || len(v.Clusters) != 2 || len(v.Users) != 2 {
		t.Fatalf("unexpected counts: %d contexts, %d clusters, %d users", len(v.Contexts), len(v.Clusters), len(v.Users))
	}

	var userA, userB UserView
	for _, u := range v.Users {
		switch u.Name {
		case "user-a":
			userA = u
		case "user-b":
			userB = u
		}
	}
	if !userA.HasToken {
		t.Error("user-a: HasToken = false, want true")
	}
	if !userB.HasClientCertificateData || !userB.HasClientKeyData {
		t.Error("user-b: expected HasClientCertificateData and HasClientKeyData true")
	}

	// The raw secret values must never appear anywhere in the view, even
	// via a %+v dump of the whole struct tree.
	dump := fmt.Sprintf("%+v", v)
	for _, secret := range []string{"super-secret-token", "cert-bytes", "key-bytes"} {
		if strings.Contains(dump, secret) {
			t.Errorf("View() leaked secret %q", secret)
		}
	}
}

func TestReveal(t *testing.T) {
	ed, _ := newTestEditor(t)

	tok, err := ed.Reveal("user-a", "token")
	if err != nil {
		t.Fatal(err)
	}
	if tok != "super-secret-token" {
		t.Errorf("Reveal token = %q, want super-secret-token", tok)
	}

	if _, err := ed.Reveal("user-a", "password"); err == nil {
		t.Error("expected error revealing an unset field")
	}
	if _, err := ed.Reveal("no-such-user", "token"); err == nil {
		t.Error("expected error for unknown user")
	}
}

func TestSetCurrentContext(t *testing.T) {
	ed, path := newTestEditor(t)

	if err := ed.SetCurrentContext("ctx-b"); err != nil {
		t.Fatal(err)
	}
	cfg := loadFile(t, path)
	if cfg.CurrentContext != "ctx-b" {
		t.Errorf("current-context = %q, want ctx-b", cfg.CurrentContext)
	}

	if err := ed.SetCurrentContext("no-such-context"); err == nil {
		t.Error("expected error for unknown context")
	}
}

func TestEditContext_Rename(t *testing.T) {
	ed, path := newTestEditor(t)

	if err := ed.EditContext("ctx-a", strPtr("ctx-renamed"), nil); err != nil {
		t.Fatal(err)
	}
	cfg := loadFile(t, path)
	if _, exists := cfg.Contexts["ctx-a"]; exists {
		t.Error("old context name ctx-a still present after rename")
	}
	if _, exists := cfg.Contexts["ctx-renamed"]; !exists {
		t.Error("renamed context ctx-renamed not found")
	}
	// ctx-a was current-context — the rename must carry it forward.
	if cfg.CurrentContext != "ctx-renamed" {
		t.Errorf("current-context = %q, want ctx-renamed to follow the rename", cfg.CurrentContext)
	}
}

func TestEditContext_RenameCollision(t *testing.T) {
	ed, _ := newTestEditor(t)
	if err := ed.EditContext("ctx-a", strPtr("ctx-b"), nil); err == nil {
		t.Error("expected error renaming onto an existing context name")
	}
}

func TestEditContext_Namespace(t *testing.T) {
	ed, path := newTestEditor(t)
	if err := ed.EditContext("ctx-b", nil, strPtr("new-ns")); err != nil {
		t.Fatal(err)
	}
	cfg := loadFile(t, path)
	if cfg.Contexts["ctx-b"].Namespace != "new-ns" {
		t.Errorf("namespace = %q, want new-ns", cfg.Contexts["ctx-b"].Namespace)
	}
}

func TestCreateContext(t *testing.T) {
	ed, path := newTestEditor(t)

	if err := ed.CreateContext("ctx-c", "cluster-b", "user-a", "ns-c"); err != nil {
		t.Fatal(err)
	}
	cfg := loadFile(t, path)
	c, ok := cfg.Contexts["ctx-c"]
	if !ok {
		t.Fatal("ctx-c not found after create")
	}
	if c.Cluster != "cluster-b" || c.AuthInfo != "user-a" || c.Namespace != "ns-c" {
		t.Errorf("ctx-c = %+v, want cluster-b/user-a/ns-c", c)
	}

	if err := ed.CreateContext("ctx-c", "cluster-b", "user-a", ""); err == nil {
		t.Error("expected error creating a duplicate context name")
	}
	if err := ed.CreateContext("ctx-d", "no-such-cluster", "user-a", ""); err == nil {
		t.Error("expected error for unknown cluster")
	}
	if err := ed.CreateContext("ctx-e", "cluster-b", "no-such-user", ""); err == nil {
		t.Error("expected error for unknown user")
	}
}

func TestCreateUser(t *testing.T) {
	ed, path := newTestEditor(t)

	if err := ed.CreateUser("user-token", UserAuthSpec{Token: "tok-123"}); err != nil {
		t.Fatal(err)
	}
	if err := ed.CreateUser("user-basic", UserAuthSpec{Username: "alice", Password: "hunter2"}); err != nil {
		t.Fatal(err)
	}
	if err := ed.CreateUser("user-cert", UserAuthSpec{ClientCertificateData: "cert-bytes", ClientKeyData: "key-bytes"}); err != nil {
		t.Fatal(err)
	}

	cfg := loadFile(t, path)
	if got := cfg.AuthInfos["user-token"]; got == nil || got.Token != "tok-123" {
		t.Errorf("user-token = %+v, want Token=tok-123", got)
	}
	if got := cfg.AuthInfos["user-basic"]; got == nil || got.Username != "alice" || got.Password != "hunter2" {
		t.Errorf("user-basic = %+v, want Username=alice Password=hunter2", got)
	}
	if got := cfg.AuthInfos["user-cert"]; got == nil || string(got.ClientCertificateData) != "cert-bytes" || string(got.ClientKeyData) != "key-bytes" {
		t.Errorf("user-cert = %+v, want cert-bytes/key-bytes", got)
	}

	if err := ed.CreateUser("user-token", UserAuthSpec{Token: "other"}); err == nil {
		t.Error("expected error creating a duplicate user name")
	}
	if err := ed.CreateUser("user-empty", UserAuthSpec{}); err == nil {
		t.Error("expected error creating a user with no credentials at all")
	}
	if err := ed.CreateUser("user-half-cert", UserAuthSpec{ClientCertificateData: "cert-only"}); err == nil {
		t.Error("expected error creating a user with a cert but no key")
	}
}

func TestEditUser_RenameRewiresContexts(t *testing.T) {
	ed, path := newTestEditor(t)

	if err := ed.EditUser("user-a", "user-a-renamed"); err != nil {
		t.Fatal(err)
	}
	cfg := loadFile(t, path)
	if _, exists := cfg.AuthInfos["user-a"]; exists {
		t.Error("user-a still present after rename")
	}
	if _, exists := cfg.AuthInfos["user-a-renamed"]; !exists {
		t.Error("user-a-renamed not found after rename")
	}
	// ctx-a referenced user-a — the rename must have rewired it, or ctx-a
	// would now dangle.
	if got := cfg.Contexts["ctx-a"].AuthInfo; got != "user-a-renamed" {
		t.Errorf("ctx-a.AuthInfo = %q, want user-a-renamed", got)
	}

	if err := ed.EditUser("no-such-user", "whatever"); err == nil {
		t.Error("expected error renaming a nonexistent user")
	}
	if err := ed.EditUser("user-a-renamed", "user-b"); err == nil {
		t.Error("expected error renaming onto an existing user name")
	}
}

func TestDeleteUser(t *testing.T) {
	ed, path := newTestEditor(t)

	// user-a is still referenced by ctx-a — must be refused, not silently
	// orphaning ctx-a.
	if err := ed.DeleteUser("user-a"); err == nil {
		t.Error("expected error deleting a user still referenced by a context")
	}

	if err := ed.CreateUser("user-c", UserAuthSpec{Token: "tok"}); err != nil {
		t.Fatal(err)
	}
	if err := ed.DeleteUser("user-c"); err != nil {
		t.Fatalf("DeleteUser on an unreferenced user: %v", err)
	}
	cfg := loadFile(t, path)
	if _, exists := cfg.AuthInfos["user-c"]; exists {
		t.Error("user-c still present after delete")
	}

	if err := ed.DeleteUser("user-c"); err == nil {
		t.Error("expected error deleting an already-deleted user")
	}
}

func TestDeleteContext_OrphanDetection(t *testing.T) {
	ed, path := newTestEditor(t)

	// ctx-b is the only context using cluster-b/user-b — deleting it
	// should report both as orphaned.
	orphanedCluster, orphanedUser, err := ed.DeleteContext("ctx-b")
	if err != nil {
		t.Fatal(err)
	}
	if orphanedCluster != "cluster-b" || orphanedUser != "user-b" {
		t.Errorf("orphans = (%q, %q), want (cluster-b, user-b)", orphanedCluster, orphanedUser)
	}
	cfg := loadFile(t, path)
	if _, exists := cfg.Contexts["ctx-b"]; exists {
		t.Error("ctx-b still present after delete")
	}
	// The cluster/user entries themselves are never auto-deleted.
	if _, exists := cfg.Clusters["cluster-b"]; !exists {
		t.Error("cluster-b was deleted, but DeleteContext must never delete cluster entries")
	}
	if _, exists := cfg.AuthInfos["user-b"]; !exists {
		t.Error("user-b was deleted, but DeleteContext must never delete user entries")
	}

	if _, _, err := ed.DeleteContext("ctx-b"); err == nil {
		t.Error("expected error deleting an already-deleted context")
	}
}

func TestDeleteContext_SharedClusterNotOrphaned(t *testing.T) {
	ed, path := newTestEditor(t)
	if err := ed.CreateContext("ctx-c", "cluster-a", "user-a", ""); err != nil {
		t.Fatal(err)
	}
	// ctx-a and ctx-c both reference cluster-a/user-a — deleting one must
	// not report the other's still-referenced cluster/user as orphaned.
	orphanedCluster, orphanedUser, err := ed.DeleteContext("ctx-a")
	if err != nil {
		t.Fatal(err)
	}
	if orphanedCluster != "" || orphanedUser != "" {
		t.Errorf("orphans = (%q, %q), want none — cluster-a/user-a are still used by ctx-c", orphanedCluster, orphanedUser)
	}
	cfg := loadFile(t, path)
	if cfg.CurrentContext != "" {
		t.Errorf("current-context = %q, want blanked after deleting the active context", cfg.CurrentContext)
	}
}

func TestImportPreview(t *testing.T) {
	ed, _ := newTestEditor(t)

	incoming := clientcmdapi.NewConfig()
	incoming.Clusters["cluster-a"] = &clientcmdapi.Cluster{Server: "https://a-changed.example.com"} // conflict
	incoming.Clusters["cluster-new"] = &clientcmdapi.Cluster{Server: "https://new.example.com"}     // new
	incoming.AuthInfos["user-new"] = &clientcmdapi.AuthInfo{Token: "t"}
	incoming.Contexts["ctx-new"] = &clientcmdapi.Context{Cluster: "cluster-new", AuthInfo: "user-new"}
	incoming.Contexts["ctx-a"] = &clientcmdapi.Context{Cluster: "cluster-a", AuthInfo: "user-a"} // conflict

	raw, err := clientcmd.Write(*incoming)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := ed.PreviewImport(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(preview.AddedContexts, "ctx-new") || !contains(preview.AddedClusters, "cluster-new") || !contains(preview.AddedUsers, "user-new") {
		t.Errorf("missing expected additions: %+v", preview)
	}
	if !contains(preview.ConflictingContexts, "ctx-a") || !contains(preview.ConflictingClusters, "cluster-a") {
		t.Errorf("missing expected conflicts: %+v", preview)
	}

	if _, err := ed.PreviewImport([]byte("not: valid: yaml: [")); err == nil {
		t.Error("expected error previewing malformed YAML")
	}
}

func TestCommitImport(t *testing.T) {
	ed, path := newTestEditor(t)

	incoming := clientcmdapi.NewConfig()
	incoming.Clusters["cluster-a"] = &clientcmdapi.Cluster{Server: "https://should-not-overwrite.example.com"}
	incoming.Clusters["cluster-new"] = &clientcmdapi.Cluster{Server: "https://new.example.com"}
	incoming.AuthInfos["user-new"] = &clientcmdapi.AuthInfo{Token: "t"}
	incoming.Contexts["ctx-new"] = &clientcmdapi.Context{Cluster: "cluster-new", AuthInfo: "user-new"}

	raw, err := clientcmd.Write(*incoming)
	if err != nil {
		t.Fatal(err)
	}
	// Only "ctx-new"/"cluster-new"/"user-new" are new — cluster-a already
	// exists and isn't in overwrite, so it must be left untouched.
	if err := ed.CommitImport(raw, nil); err != nil {
		t.Fatal(err)
	}

	cfg := loadFile(t, path)
	if cfg.Clusters["cluster-a"].Server != "https://a.example.com" {
		t.Errorf("cluster-a.Server = %q, want unchanged (not in overwrite list)", cfg.Clusters["cluster-a"].Server)
	}
	if _, ok := cfg.Contexts["ctx-new"]; !ok {
		t.Error("ctx-new not added by import")
	}

	// Now explicitly overwrite cluster-a.
	if err := ed.CommitImport(raw, []string{"cluster-a"}); err != nil {
		t.Fatal(err)
	}
	cfg = loadFile(t, path)
	if cfg.Clusters["cluster-a"].Server != "https://should-not-overwrite.example.com" {
		t.Errorf("cluster-a.Server = %q, want overwritten", cfg.Clusters["cluster-a"].Server)
	}
}

func TestApply_BacksUpBeforeWrite(t *testing.T) {
	ed, path := newTestEditor(t)
	before, err := os.ReadFile(path) //nolint:gosec // path is this test's own t.TempDir() fixture, not user input
	if err != nil {
		t.Fatal(err)
	}

	if err := ed.SetCurrentContext("ctx-b"); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	var backups []string
	prefix := filepath.Base(path) + ".bak-"
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), prefix) {
			backups = append(backups, e.Name())
		}
	}
	if len(backups) != 1 {
		t.Fatalf("got %d backup files, want 1: %v", len(backups), backups)
	}
	backupData, err := os.ReadFile(filepath.Join(filepath.Dir(path), backups[0]))
	if err != nil {
		t.Fatal(err)
	}
	if string(backupData) != string(before) {
		t.Error("backup contents don't match the pre-write file")
	}
}

func TestApply_PrunesOldBackups(t *testing.T) {
	ed, path := newTestEditor(t)
	for i := 0; i < maxBackupsPerFile+3; i++ {
		if err := ed.SetCurrentContext("ctx-a"); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	var backups []string
	prefix := filepath.Base(path) + ".bak-"
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), prefix) {
			backups = append(backups, e.Name())
		}
	}
	if len(backups) > maxBackupsPerFile {
		t.Errorf("got %d backups, want at most %d", len(backups), maxBackupsPerFile)
	}
}

func TestApply_ConcurrentWritesLeaveValidFile(t *testing.T) {
	ed, path := newTestEditor(t)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			target := "ctx-a"
			if n%2 == 0 {
				target = "ctx-b"
			}
			_ = ed.SetCurrentContext(target)
		}(i)
	}
	wg.Wait()

	cfg, err := clientcmd.LoadFromFile(path)
	if err != nil {
		t.Fatalf("kubeconfig corrupted after concurrent writes: %v", err)
	}
	if cfg.CurrentContext != "ctx-a" && cfg.CurrentContext != "ctx-b" {
		t.Errorf("current-context = %q after concurrent writes, want ctx-a or ctx-b", cfg.CurrentContext)
	}
}

// TestMultiFilePrecedence confirms an edit to an entry that came from the
// second file in $KUBECONFIG's precedence list is written back into that
// same file — not the first — matching clientcmd.ModifyConfig's own
// LocationOfOrigin-driven behavior, which this package deliberately relies
// on rather than reimplementing.
func TestMultiFilePrecedence(t *testing.T) {
	dir := t.TempDir()
	pathA := filepath.Join(dir, "config-a")
	pathB := filepath.Join(dir, "config-b")

	cfgA := clientcmdapi.NewConfig()
	cfgA.Clusters["cluster-1"] = &clientcmdapi.Cluster{Server: "https://one.example.com"}
	cfgA.AuthInfos["user-1"] = &clientcmdapi.AuthInfo{Token: "t1"}
	cfgA.Contexts["ctx-1"] = &clientcmdapi.Context{Cluster: "cluster-1", AuthInfo: "user-1", Namespace: "ns1"}
	cfgA.CurrentContext = "ctx-1"
	if err := clientcmd.WriteToFile(*cfgA, pathA); err != nil {
		t.Fatal(err)
	}

	cfgB := clientcmdapi.NewConfig()
	cfgB.Clusters["cluster-2"] = &clientcmdapi.Cluster{Server: "https://two.example.com"}
	cfgB.AuthInfos["user-2"] = &clientcmdapi.AuthInfo{Token: "t2"}
	cfgB.Contexts["ctx-2"] = &clientcmdapi.Context{Cluster: "cluster-2", AuthInfo: "user-2", Namespace: "ns2"}
	if err := clientcmd.WriteToFile(*cfgB, pathB); err != nil {
		t.Fatal(err)
	}

	t.Setenv("KUBECONFIG", pathA+string(os.PathListSeparator)+pathB)
	ed, err := NewEditor()
	if err != nil {
		t.Fatal(err)
	}

	if err := ed.EditContext("ctx-2", nil, strPtr("ns2-changed")); err != nil {
		t.Fatal(err)
	}

	bCfg := loadFile(t, pathB)
	if bCfg.Contexts["ctx-2"].Namespace != "ns2-changed" {
		t.Errorf("config-b ctx-2 namespace = %q, want ns2-changed", bCfg.Contexts["ctx-2"].Namespace)
	}
	aCfg := loadFile(t, pathA)
	if _, exists := aCfg.Contexts["ctx-2"]; exists {
		t.Error("ctx-2 leaked into config-a — must be written only to its own LocationOfOrigin file")
	}
	if aCfg.Contexts["ctx-1"].Namespace != "ns1" {
		t.Error("config-a's own untouched context was modified")
	}
}

// addDanglingContext writes a context referencing a cluster/user that
// doesn't exist directly to path, bypassing the Editor — the shape of a
// kubeconfig merged from years of `aws eks update-kubeconfig` runs plus an
// old abandoned entry, which a real user hit: it made every edit through
// the app fail with "resulting kubeconfig would be invalid", even edits
// with nothing to do with the broken entry.
func addDanglingContext(t *testing.T, path string) {
	t.Helper()
	raw := loadFile(t, path)
	raw.Contexts["dangling"] = &clientcmdapi.Context{Cluster: "no-such-cluster", AuthInfo: "no-such-user"}
	if err := clientcmd.WriteToFile(*raw, path); err != nil {
		t.Fatal(err)
	}
	if err := clientcmd.Validate(*raw); err == nil {
		t.Fatal("test setup: expected the dangling context to make the file invalid")
	}
}

func TestApply_PreexistingInvalidEntryDoesNotBlockUnrelatedEdits(t *testing.T) {
	ed, path := newTestEditor(t)
	addDanglingContext(t, path)

	// An edit that has nothing to do with "dangling" must still succeed.
	if err := ed.SetCurrentContext("ctx-b"); err != nil {
		t.Fatalf("SetCurrentContext failed because of a pre-existing unrelated problem: %v", err)
	}

	cfg := loadFile(t, path)
	if cfg.CurrentContext != "ctx-b" {
		t.Errorf("current-context = %q, want ctx-b", cfg.CurrentContext)
	}
	// The dangling context is left alone, not silently dropped — apply
	// should only ever refuse or accept a write, never repair one.
	if _, ok := cfg.Contexts["dangling"]; !ok {
		t.Error("pre-existing dangling context was removed by an unrelated edit")
	}
}

func TestApply_NewInvalidReferenceStillBlockedDespitePreexistingOne(t *testing.T) {
	ed, path := newTestEditor(t)
	addDanglingContext(t, path)

	// A NEW invalid reference must still be rejected, even though the file
	// already had a different, pre-existing one.
	if err := ed.CreateContext("ctx-new", "another-no-such-cluster", "user-a", ""); err == nil {
		t.Error("expected error for a context referencing an unknown cluster")
	}
	if _, _, err := ed.DeleteContext("nonexistent"); err == nil {
		t.Error("expected error deleting a context that was never there")
	}
}

func loadFile(t *testing.T, path string) *clientcmdapi.Config {
	t.Helper()
	cfg, err := clientcmd.LoadFromFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func strPtr(s string) *string { return &s }

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
