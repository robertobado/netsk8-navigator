// Package kubeconfig safely reads and writes the user's real kubeconfig
// file(s) — contexts, clusters, and users/auth entries. It is distinct from
// internal/kube, which only ever reads the kubeconfig to build clients for
// talking to a cluster, and from internal/config, which persists this app's
// own preferences JSON (a different file entirely). Every write here goes
// through clientcmd.ModifyConfig — the same code `kubectl config` itself
// uses — so multi-file $KUBECONFIG merge/precedence is handled correctly
// rather than reimplemented, and every write is preceded by a backup of
// every file in that precedence, since a bug here can lock a user out of
// every cluster they manage.
package kubeconfig

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// maxBackupsPerFile bounds how many timestamped backups accumulate per
// kubeconfig file — enough to recover from a recent mistake without
// littering ~/.kube/ forever.
const maxBackupsPerFile = 10

// Editor is a concurrency-safe wrapper around clientcmd's PathOptions.
type Editor struct {
	mu          sync.Mutex
	pathOptions clientcmd.ConfigAccess
}

// NewEditor resolves the kubeconfig location(s) (respecting $KUBECONFIG,
// same as everything else in this app) and fails fast if the file can't be
// read, rather than deferring that failure to the first write.
func NewEditor() (*Editor, error) {
	po := clientcmd.NewDefaultPathOptions()
	if _, err := po.GetStartingConfig(); err != nil {
		return nil, fmt.Errorf("reading kubeconfig: %w", err)
	}
	return &Editor{pathOptions: po}, nil
}

// ContextView, ClusterView, and UserView are UI-friendly, secret-masked
// projections of a kubeconfig entry. LocationOfOrigin names the physical
// file the entry came from after $KUBECONFIG merge — surfaced so a
// multi-file setup is legible, and so mutations are unambiguous about which
// file gets touched (clientcmd.ModifyConfig derives that from this field).
type ContextView struct {
	Name             string `json:"name"`
	Cluster          string `json:"cluster"`
	AuthInfo         string `json:"user"`
	Namespace        string `json:"namespace"`
	LocationOfOrigin string `json:"locationOfOrigin"`
	Current          bool   `json:"current"`
}

type ClusterView struct {
	Name                        string `json:"name"`
	Server                      string `json:"server"`
	LocationOfOrigin            string `json:"locationOfOrigin"`
	InsecureSkipTLSVerify       bool   `json:"insecureSkipTLSVerify"`
	HasCertificateAuthorityData bool   `json:"hasCertificateAuthorityData"`
	CertificateAuthority        string `json:"certificateAuthority,omitempty"`
}

// UserView deliberately never carries a raw secret value or Exec.Env (which
// can itself carry credentials for some plugins) — only booleans indicating
// which secret fields are set, plus the exec command and a best-effort
// profile name (mirroring kube.Manager.ExecInfoFor's own extraction). The
// actual secret value is only ever available through Editor.Reveal, a
// separate, explicitly audited call.
type UserView struct {
	Name                     string `json:"name"`
	LocationOfOrigin         string `json:"locationOfOrigin"`
	Username                 string `json:"username,omitempty"`
	HasPassword              bool   `json:"hasPassword"`
	HasToken                 bool   `json:"hasToken"`
	HasClientCertificateData bool   `json:"hasClientCertificateData"`
	HasClientKeyData         bool   `json:"hasClientKeyData"`
	ExecCommand              string `json:"execCommand,omitempty"`
	ExecProfile              string `json:"execProfile,omitempty"`
	AuthProvider             string `json:"authProvider,omitempty"`
}

// View is the full masked snapshot served by GET /api/kubeconfig.
type View struct {
	CurrentContext string        `json:"currentContext"`
	ConfigPaths    []string      `json:"configPaths"`
	Contexts       []ContextView `json:"contexts"`
	Clusters       []ClusterView `json:"clusters"`
	Users          []UserView    `json:"users"`
}

// ImportPreview reports what a candidate kubeconfig (paste/upload) would add
// versus what already exists under the same name, before anything is written.
type ImportPreview struct {
	AddedContexts       []string `json:"addedContexts"`
	AddedClusters       []string `json:"addedClusters"`
	AddedUsers          []string `json:"addedUsers"`
	ConflictingContexts []string `json:"conflictingContexts"`
	ConflictingClusters []string `json:"conflictingClusters"`
	ConflictingUsers    []string `json:"conflictingUsers"`
}

// View returns a masked snapshot of the merged kubeconfig.
func (e *Editor) View() (View, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	cfg, err := e.pathOptions.GetStartingConfig()
	if err != nil {
		return View{}, fmt.Errorf("reading kubeconfig: %w", err)
	}
	return buildView(cfg), nil
}

func buildView(cfg *clientcmdapi.Config) View {
	v := View{CurrentContext: cfg.CurrentContext}
	paths := map[string]bool{}

	for name, c := range cfg.Contexts {
		ns := c.Namespace
		if ns == "" {
			ns = "default"
		}
		v.Contexts = append(v.Contexts, ContextView{
			Name: name, Cluster: c.Cluster, AuthInfo: c.AuthInfo, Namespace: ns,
			LocationOfOrigin: c.LocationOfOrigin, Current: name == cfg.CurrentContext,
		})
		markPath(paths, c.LocationOfOrigin)
	}
	for name, cl := range cfg.Clusters {
		v.Clusters = append(v.Clusters, ClusterView{
			Name: name, Server: cl.Server, LocationOfOrigin: cl.LocationOfOrigin,
			InsecureSkipTLSVerify:       cl.InsecureSkipTLSVerify,
			HasCertificateAuthorityData: len(cl.CertificateAuthorityData) > 0,
			CertificateAuthority:        cl.CertificateAuthority,
		})
		markPath(paths, cl.LocationOfOrigin)
	}
	for name, ai := range cfg.AuthInfos {
		uv := UserView{
			Name: name, LocationOfOrigin: ai.LocationOfOrigin, Username: ai.Username,
			HasPassword:              ai.Password != "",
			HasToken:                 ai.Token != "" || ai.TokenFile != "",
			HasClientCertificateData: len(ai.ClientCertificateData) > 0 || ai.ClientCertificate != "",
			HasClientKeyData:         len(ai.ClientKeyData) > 0 || ai.ClientKey != "",
		}
		if ai.Exec != nil {
			uv.ExecCommand = filepath.Base(ai.Exec.Command)
			uv.ExecProfile = execProfile(ai.Exec)
		}
		if ai.AuthProvider != nil {
			uv.AuthProvider = ai.AuthProvider.Name
		}
		v.Users = append(v.Users, uv)
		markPath(paths, ai.LocationOfOrigin)
	}

	sort.Slice(v.Contexts, func(i, j int) bool { return v.Contexts[i].Name < v.Contexts[j].Name })
	sort.Slice(v.Clusters, func(i, j int) bool { return v.Clusters[i].Name < v.Clusters[j].Name })
	sort.Slice(v.Users, func(i, j int) bool { return v.Users[i].Name < v.Users[j].Name })
	for p := range paths {
		v.ConfigPaths = append(v.ConfigPaths, p)
	}
	sort.Strings(v.ConfigPaths)
	return v
}

func markPath(paths map[string]bool, p string) {
	if p != "" {
		paths[p] = true
	}
}

// execProfile best-effort extracts an AWS-style profile name from an exec
// plugin's env/args — mirrors kube.Manager.ExecInfoFor's own logic. Only the
// profile *name* is surfaced, never the rest of Exec.Env (which can itself
// carry credentials for some plugins).
func execProfile(exec *clientcmdapi.ExecConfig) string {
	for _, e := range exec.Env {
		if e.Name == "AWS_PROFILE" || e.Name == "AWS_DEFAULT_PROFILE" {
			return e.Value
		}
	}
	for i, a := range exec.Args {
		if a == "--profile" && i+1 < len(exec.Args) {
			return exec.Args[i+1]
		}
	}
	return ""
}

// Reveal returns the raw value of one secret field for a user — the only
// path by which a real secret ever leaves this package. Callers must audit
// every call (see internal/api/kubeconfig.go).
func (e *Editor) Reveal(userName, field string) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	cfg, err := e.pathOptions.GetStartingConfig()
	if err != nil {
		return "", fmt.Errorf("reading kubeconfig: %w", err)
	}
	ai, ok := cfg.AuthInfos[userName]
	if !ok {
		return "", fmt.Errorf("user %q not found in kubeconfig", userName)
	}
	switch field {
	case "token":
		if ai.Token == "" {
			return "", fmt.Errorf("user %q has no token", userName)
		}
		return ai.Token, nil
	case "password":
		if ai.Password == "" {
			return "", fmt.Errorf("user %q has no password", userName)
		}
		return ai.Password, nil
	case "clientKeyData":
		if len(ai.ClientKeyData) == 0 {
			return "", fmt.Errorf("user %q has no client-key-data", userName)
		}
		return string(ai.ClientKeyData), nil
	case "clientCertificateData":
		if len(ai.ClientCertificateData) == 0 {
			return "", fmt.Errorf("user %q has no client-certificate-data", userName)
		}
		return string(ai.ClientCertificateData), nil
	default:
		return "", fmt.Errorf("unknown field %q", field)
	}
}

// apply is the single chokepoint every mutation funnels through: fresh
// read, backup every file in the loading precedence, mutate, validate, then
// write via clientcmd.ModifyConfig (which correctly targets whichever file
// each changed entry's LocationOfOrigin names, or the default file for new
// entries — logic we deliberately don't reimplement).
func (e *Editor) apply(mutate func(cfg *clientcmdapi.Config) error) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	cfg, err := e.pathOptions.GetStartingConfig()
	if err != nil {
		return fmt.Errorf("reading kubeconfig: %w", err)
	}
	// Baseline: whatever's already wrong with the file before this edit. A
	// kubeconfig merged from years of `aws eks update-kubeconfig` runs and
	// abandoned manual entries very commonly has a stray dangling context
	// or two already — nothing this edit touched. Blanket-validating the
	// whole file (clientcmd.Validate does, iterating every context/cluster/
	// user) would fail every future edit over that pre-existing mess,
	// permanently locking the file out of the app. Only a validation error
	// this specific edit introduces — not present in the baseline — blocks
	// the write; see newValidationErrors.
	before := validationMessages(clientcmd.Validate(*cfg))

	if err := e.backupAll(); err != nil {
		return fmt.Errorf("backing up kubeconfig before write: %w", err)
	}
	if err := mutate(cfg); err != nil {
		return err
	}
	if newErr := newValidationErrors(before, clientcmd.Validate(*cfg)); newErr != nil {
		return fmt.Errorf("this change would make the kubeconfig invalid: %w", newErr)
	}
	return clientcmd.ModifyConfig(e.pathOptions, *cfg, true)
}

// validationMessages flattens a clientcmd.Validate result — nil, a single
// error, or (in practice, always) the *errConfigurationInvalid aggregate it
// returns — into a set of its error message strings, for newValidationErrors
// to diff against.
func validationMessages(err error) map[string]bool {
	set := map[string]bool{}
	for _, e := range validationErrorList(err) {
		set[e.Error()] = true
	}
	return set
}

// errorLister matches utilerrors.Aggregate's shape (Errors() []error)
// without importing k8s.io/apimachinery just for that one interface —
// clientcmd's own *errConfigurationInvalid already implements it.
type errorLister interface{ Errors() []error }

func validationErrorList(err error) []error {
	if err == nil {
		return nil
	}
	if agg, ok := err.(errorLister); ok {
		return agg.Errors()
	}
	return []error{err}
}

// newValidationErrors reports only the problems present in `after` that
// weren't already in `before` — nil if this edit introduced nothing new.
func newValidationErrors(before map[string]bool, after error) error {
	var fresh []error
	for _, e := range validationErrorList(after) {
		if !before[e.Error()] {
			fresh = append(fresh, e)
		}
	}
	if len(fresh) == 0 {
		return nil
	}
	return errors.Join(fresh...)
}

func (e *Editor) backupAll() error {
	for _, path := range e.pathOptions.GetLoadingPrecedence() {
		if err := backupFile(path); err != nil {
			return err
		}
	}
	return nil
}

func backupFile(path string) error {
	data, err := os.ReadFile(path) //nolint:gosec // path is one of $KUBECONFIG's own resolved locations (GetLoadingPrecedence), not user input
	if err != nil {
		if os.IsNotExist(err) {
			return nil // nothing to back up yet — e.g. a $KUBECONFIG entry that doesn't exist on disk until first write
		}
		return err
	}
	backup := fmt.Sprintf("%s.bak-%s", path, time.Now().UTC().Format("20060102T150405.000000000"))
	if err := os.WriteFile(backup, data, 0o600); err != nil { //nolint:gosec // backup is path plus a fixed suffix, same trust level as path itself
		return err
	}
	return pruneBackups(path)
}

// pruneBackups keeps only the most recent maxBackupsPerFile timestamped
// backups for path — the timestamp format is fixed-width and zero-padded,
// so lexicographic sort is chronological sort.
func pruneBackups(path string) error {
	dir := filepath.Dir(path)
	prefix := filepath.Base(path) + ".bak-"
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var backups []string
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), prefix) {
			backups = append(backups, entry.Name())
		}
	}
	sort.Strings(backups)
	for len(backups) > maxBackupsPerFile {
		_ = os.Remove(filepath.Join(dir, backups[0]))
		backups = backups[1:]
	}
	return nil
}

// SetCurrentContext writes current-context to the file — an explicit,
// standalone action distinct from the app's own client-side "which context
// is Browse pointed at" state.
func (e *Editor) SetCurrentContext(name string) error {
	return e.apply(func(cfg *clientcmdapi.Config) error {
		if _, ok := cfg.Contexts[name]; !ok {
			return fmt.Errorf("context %q not found", name)
		}
		cfg.CurrentContext = name
		return nil
	})
}

// EditContext renames a context and/or changes its default namespace.
// newName/namespace are nil when that field isn't being changed.
func (e *Editor) EditContext(name string, newName, namespace *string) error {
	return e.apply(func(cfg *clientcmdapi.Config) error {
		c, ok := cfg.Contexts[name]
		if !ok {
			return fmt.Errorf("context %q not found", name)
		}
		if namespace != nil {
			c.Namespace = *namespace
		}
		if newName != nil && *newName != "" && *newName != name {
			if _, exists := cfg.Contexts[*newName]; exists {
				return fmt.Errorf("context %q already exists", *newName)
			}
			delete(cfg.Contexts, name)
			cfg.Contexts[*newName] = c
			if cfg.CurrentContext == name {
				cfg.CurrentContext = *newName
			}
		}
		return nil
	})
}

// CreateContext composes a new context from an existing cluster and user —
// it never creates a brand-new cluster/user entry itself; see CreateUser
// for that.
func (e *Editor) CreateContext(name, cluster, authInfo, namespace string) error {
	return e.apply(func(cfg *clientcmdapi.Config) error {
		if name == "" {
			return fmt.Errorf("context name is required")
		}
		if _, exists := cfg.Contexts[name]; exists {
			return fmt.Errorf("context %q already exists", name)
		}
		if _, ok := cfg.Clusters[cluster]; !ok {
			return fmt.Errorf("cluster %q not found", cluster)
		}
		if _, ok := cfg.AuthInfos[authInfo]; !ok {
			return fmt.Errorf("user %q not found", authInfo)
		}
		cfg.Contexts[name] = &clientcmdapi.Context{Cluster: cluster, AuthInfo: authInfo, Namespace: namespace}
		return nil
	})
}

// DeleteContext removes a context only — matching `kubectl config
// delete-context`, it never deletes the cluster/user entries it referenced,
// reporting them as orphaned instead when nothing else references them.
// Deleting the active context blanks current-context, exactly like kubectl.
func (e *Editor) DeleteContext(name string) (orphanedCluster, orphanedUser string, err error) {
	err = e.apply(func(cfg *clientcmdapi.Config) error {
		c, ok := cfg.Contexts[name]
		if !ok {
			return fmt.Errorf("context %q not found", name)
		}
		cluster, user := c.Cluster, c.AuthInfo
		delete(cfg.Contexts, name)
		if cfg.CurrentContext == name {
			cfg.CurrentContext = ""
		}
		if !clusterReferenced(cfg, cluster) {
			orphanedCluster = cluster
		}
		if !userReferenced(cfg, user) {
			orphanedUser = user
		}
		return nil
	})
	if err != nil {
		return "", "", err
	}
	return orphanedCluster, orphanedUser, nil
}

func clusterReferenced(cfg *clientcmdapi.Config, cluster string) bool {
	for _, c := range cfg.Contexts {
		if c.Cluster == cluster {
			return true
		}
	}
	return false
}

func userReferenced(cfg *clientcmdapi.Config, user string) bool {
	for _, c := range cfg.Contexts {
		if c.AuthInfo == user {
			return true
		}
	}
	return false
}

// UserAuthSpec is CreateUser's credential input — exactly one of Token,
// (Username and/or Password), or (ClientCertificateData and
// ClientKeyData) should be set. Exec-plugin and auth-provider users aren't
// hand-authorable here: those are generated by cloud CLI tooling (aws eks
// update-kubeconfig, gcloud, ...), never reasonably typed into a form, and
// stay reachable only by pasting/importing a whole kubeconfig (see
// PreviewImport/CommitImport).
type UserAuthSpec struct {
	Token                 string
	Username              string
	Password              string
	ClientCertificateData string
	ClientKeyData         string
}

// CreateUser adds a brand-new user (AuthInfo) entry from hand-typed
// credentials — the counterpart CreateContext deliberately doesn't provide.
func (e *Editor) CreateUser(name string, spec UserAuthSpec) error {
	return e.apply(func(cfg *clientcmdapi.Config) error {
		name = strings.TrimSpace(name)
		if name == "" {
			return fmt.Errorf("user name is required")
		}
		if _, exists := cfg.AuthInfos[name]; exists {
			return fmt.Errorf("user %q already exists", name)
		}
		ai := clientcmdapi.NewAuthInfo()
		switch {
		case spec.Token != "":
			ai.Token = spec.Token
		case spec.Username != "" || spec.Password != "":
			ai.Username = spec.Username
			ai.Password = spec.Password
		case spec.ClientCertificateData != "" && spec.ClientKeyData != "":
			ai.ClientCertificateData = []byte(spec.ClientCertificateData)
			ai.ClientKeyData = []byte(spec.ClientKeyData)
		default:
			return fmt.Errorf("provide a token, a username/password, or both a client certificate and client key")
		}
		cfg.AuthInfos[name] = ai
		return nil
	})
}

// EditUser renames a user entry and rewires every context that referenced
// the old name to the new one. Unlike EditContext's rename (a context's own
// name is never referenced by anything else in the file), a user's name
// *is* referenced elsewhere — by context.AuthInfo — and kubectl itself has
// no rename-user of its own to mirror the logic from, so this does the
// rewiring by hand rather than leaving contexts dangling for apply()'s
// validation diff to catch after the fact.
func (e *Editor) EditUser(name, newName string) error {
	return e.apply(func(cfg *clientcmdapi.Config) error {
		ai, ok := cfg.AuthInfos[name]
		if !ok {
			return fmt.Errorf("user %q not found", name)
		}
		newName = strings.TrimSpace(newName)
		if newName == "" || newName == name {
			return nil
		}
		if _, exists := cfg.AuthInfos[newName]; exists {
			return fmt.Errorf("user %q already exists", newName)
		}
		delete(cfg.AuthInfos, name)
		cfg.AuthInfos[newName] = ai
		for _, c := range cfg.Contexts {
			if c.AuthInfo == name {
				c.AuthInfo = newName
			}
		}
		return nil
	})
}

// DeleteUser removes a user entry — refusing up front, with one clear
// error naming every affected context, rather than deleting it and letting
// apply()'s validation diff reject the write with a vaguer aggregate error
// (or, worse, silently orphaning those contexts if it were ever relaxed).
func (e *Editor) DeleteUser(name string) error {
	return e.apply(func(cfg *clientcmdapi.Config) error {
		if _, ok := cfg.AuthInfos[name]; !ok {
			return fmt.Errorf("user %q not found", name)
		}
		if using := contextsUsingUser(cfg, name); len(using) > 0 {
			sort.Strings(using)
			return fmt.Errorf("user %q is still used by context(s): %s", name, strings.Join(using, ", "))
		}
		delete(cfg.AuthInfos, name)
		return nil
	})
}

func contextsUsingUser(cfg *clientcmdapi.Config, user string) []string {
	var names []string
	for name, c := range cfg.Contexts {
		if c.AuthInfo == user {
			names = append(names, name)
		}
	}
	return names
}

// PreviewImport parses raw (a full kubeconfig YAML, e.g. pasted or
// uploaded) and reports what it would add versus what conflicts with
// existing names, without writing anything.
func (e *Editor) PreviewImport(raw []byte) (ImportPreview, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	incoming, err := clientcmd.Load(raw)
	if err != nil {
		return ImportPreview{}, fmt.Errorf("parsing kubeconfig: %w", err)
	}
	cfg, err := e.pathOptions.GetStartingConfig()
	if err != nil {
		return ImportPreview{}, fmt.Errorf("reading kubeconfig: %w", err)
	}
	return diffImport(cfg, incoming), nil
}

func diffImport(cfg, incoming *clientcmdapi.Config) ImportPreview {
	var p ImportPreview
	for name := range incoming.Contexts {
		if _, exists := cfg.Contexts[name]; exists {
			p.ConflictingContexts = append(p.ConflictingContexts, name)
		} else {
			p.AddedContexts = append(p.AddedContexts, name)
		}
	}
	for name := range incoming.Clusters {
		if _, exists := cfg.Clusters[name]; exists {
			p.ConflictingClusters = append(p.ConflictingClusters, name)
		} else {
			p.AddedClusters = append(p.AddedClusters, name)
		}
	}
	for name := range incoming.AuthInfos {
		if _, exists := cfg.AuthInfos[name]; exists {
			p.ConflictingUsers = append(p.ConflictingUsers, name)
		} else {
			p.AddedUsers = append(p.AddedUsers, name)
		}
	}
	sort.Strings(p.AddedContexts)
	sort.Strings(p.AddedClusters)
	sort.Strings(p.AddedUsers)
	sort.Strings(p.ConflictingContexts)
	sort.Strings(p.ConflictingClusters)
	sort.Strings(p.ConflictingUsers)
	return p
}

// CommitImport merges raw into the kubeconfig: names not already present are
// always added; names that already exist are only replaced when listed in
// overwrite (as surfaced by a prior PreviewImport and confirmed by the user).
func (e *Editor) CommitImport(raw []byte, overwrite []string) error {
	incoming, err := clientcmd.Load(raw)
	if err != nil {
		return fmt.Errorf("parsing kubeconfig: %w", err)
	}
	allow := make(map[string]bool, len(overwrite))
	for _, n := range overwrite {
		allow[n] = true
	}
	return e.apply(func(cfg *clientcmdapi.Config) error {
		mergeImport(cfg, incoming, allow)
		return nil
	})
}

// mergeImport is split out of CommitImport's mutate closure — inlined, its
// three loops pushed CommitImport's own cognitive complexity over the lint
// limit, the same reasoning behind every registerXTool split in
// internal/api/mcp_tools_read.go.
func mergeImport(cfg, incoming *clientcmdapi.Config, allow map[string]bool) {
	for name, c := range incoming.Contexts {
		if _, exists := cfg.Contexts[name]; exists && !allow[name] {
			continue
		}
		cfg.Contexts[name] = c
	}
	for name, cl := range incoming.Clusters {
		if _, exists := cfg.Clusters[name]; exists && !allow[name] {
			continue
		}
		cfg.Clusters[name] = cl
	}
	for name, ai := range incoming.AuthInfos {
		if _, exists := cfg.AuthInfos[name]; exists && !allow[name] {
			continue
		}
		cfg.AuthInfos[name] = ai
	}
}
