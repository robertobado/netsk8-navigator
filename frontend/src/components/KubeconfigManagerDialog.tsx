import { useState, type ReactNode } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Check, Eye, EyeOff, KeyRound, Pencil, Plug, Star, Trash2, Upload, X } from 'lucide-react'
import {
  commitKubeconfigImport,
  createKubeconfigContext,
  createKubeconfigUser,
  deleteKubeconfigContext,
  deleteKubeconfigUser,
  editKubeconfigContext,
  editKubeconfigUser,
  kubeconfigView,
  pingContext,
  previewKubeconfigImport,
  revealKubeconfigSecret,
  setCurrentContext,
  type CreateKubeconfigUserInput,
  type ImportPreview,
  type KubeconfigContextView,
  type KubeconfigUserView,
  type PingResult,
  type RevealField,
} from '@/lib/api'
import { useAppPrefs, setAppPrefs } from '@/lib/preferences'
import { useMcpGate, setMcpGate } from '@/lib/mcpGate'
import { Switch } from '@/components/Switch'
import { useT } from '@/lib/i18n'
import { cn, shortContext } from '@/lib/utils'

interface Props {
  open: boolean
  onClose: () => void
  activeCtx: string | undefined
  onSelectContext: (name: string) => void
}

export function KubeconfigManagerDialog({ open, onClose, activeCtx, onSelectContext }: Readonly<Props>) {
  const t = useT()
  const queryClient = useQueryClient()
  const { contexts: contextPrefs } = useAppPrefs()
  const mcp = useMcpGate()
  const [error, setError] = useState<string | null>(null)
  const [editing, setEditing] = useState<string | null>(null)
  const [editName, setEditName] = useState('')
  const [editNamespace, setEditNamespace] = useState('')
  const [confirmingDelete, setConfirmingDelete] = useState<string | null>(null)
  const [notice, setNotice] = useState<string | null>(null)
  const [pings, setPings] = useState<Record<string, PingResult | 'loading'>>({})
  const [importOpen, setImportOpen] = useState(false)
  const [editingUser, setEditingUser] = useState<string | null>(null)
  const [editUserName, setEditUserName] = useState('')
  const [confirmingDeleteUser, setConfirmingDeleteUser] = useState<string | null>(null)

  const viewQ = useQuery({ queryKey: ['kubeconfig'], queryFn: kubeconfigView, enabled: open })

  const refresh = async () => {
    await queryClient.invalidateQueries({ queryKey: ['kubeconfig'] })
    await queryClient.invalidateQueries({ queryKey: ['contexts'] })
  }

  const run = async (action: () => Promise<void>) => {
    setError(null)
    try {
      await action()
      await refresh()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    }
  }

  if (!open) return null
  const view = viewQ.data

  const toggleFavorite = (name: string) => {
    const favorites = contextPrefs.favorites.includes(name) ? contextPrefs.favorites.filter((n) => n !== name) : [...contextPrefs.favorites, name]
    setAppPrefs({ contexts: { ...contextPrefs, favorites } })
  }

  // The MCP read/write pins go through the dedicated gate endpoint (see
  // lib/mcpGate.ts); run() surfaces a failed write in the dialog's error
  // banner rather than letting the switch drift from the backend.
  const toggleReadDisabled = (name: string) => {
    const disabled = mcp.readDisabledContexts.includes(name)
    const readDisabledContexts = disabled ? mcp.readDisabledContexts.filter((n) => n !== name) : [...mcp.readDisabledContexts, name]
    void run(() => setMcpGate({ readDisabledContexts }))
  }
  const toggleWriteDisabled = (name: string) => {
    const pinned = mcp.readOnlyContexts.includes(name)
    const readOnlyContexts = pinned ? mcp.readOnlyContexts.filter((n) => n !== name) : [...mcp.readOnlyContexts, name]
    void run(() => setMcpGate({ readOnlyContexts }))
  }

  const startEdit = (name: string, namespace: string) => {
    setEditing(name)
    setEditName(name)
    setEditNamespace(namespace)
  }
  const saveEdit = (oldName: string) =>
    run(async () => {
      const newName = editName.trim()
      await editKubeconfigContext(oldName, { newName: newName !== oldName ? newName : undefined, namespace: editNamespace })
      if (oldName === activeCtx && newName && newName !== oldName) onSelectContext(newName)
      setEditing(null)
    })

  const doDelete = (name: string) =>
    run(async () => {
      const { orphanedCluster, orphanedUser } = await deleteKubeconfigContext(name)
      setConfirmingDelete(null)
      if (orphanedCluster || orphanedUser) {
        setNotice(
          [orphanedCluster && `cluster "${orphanedCluster}"`, orphanedUser && `user "${orphanedUser}"`].filter(Boolean).join(' and ') +
            ' are no longer used by any context',
        )
      }
    })

  const doPing = async (name: string) => {
    setPings((p) => ({ ...p, [name]: 'loading' }))
    try {
      const result = await pingContext(name)
      setPings((p) => ({ ...p, [name]: result }))
    } catch (e) {
      setPings((p) => ({ ...p, [name]: { reachable: false, error: e instanceof Error ? e.message : String(e) } }))
    }
  }

  const startEditUser = (name: string) => {
    setEditingUser(name)
    setEditUserName(name)
  }
  const saveEditUser = (oldName: string) =>
    run(async () => {
      const newName = editUserName.trim()
      if (newName && newName !== oldName) await editKubeconfigUser(oldName, newName)
      setEditingUser(null)
    })
  const doDeleteUser = (name: string) =>
    run(async () => {
      await deleteKubeconfigUser(name)
      setConfirmingDeleteUser(null)
    })

  return (
    <>
      <div aria-hidden="true" className="fixed inset-0 z-[90] bg-black/50 backdrop-blur-sm" onClick={onClose} />
      <div className="pointer-events-none fixed inset-0 z-[90] flex items-center justify-center p-4">
        {/* Wider than every other dialog in the app (max-w-3xl elsewhere) —
            this is the one screen showing dense tabular config (an 8-column
            context table, now a user table with rename/delete/create too)
            rather than a single form or YAML editor. */}
        <div className="pointer-events-auto flex h-[85vh] w-full max-w-6xl flex-col overflow-hidden rounded-2xl border bg-card shadow-2xl">
          <div className="flex items-center justify-between border-b px-5 py-3.5">
            <h2 className="flex items-center gap-2 text-sm font-semibold">
              <KeyRound className="size-4" /> {t('kubeconfig.title', 'Manage kubeconfig')}
            </h2>
            <button
              type="button"
              onClick={onClose}
              className="rounded-lg p-1.5 text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
              aria-label={t('Close')}
            >
              <X className="size-4" />
            </button>
          </div>

          <div className="min-h-0 flex-1 space-y-4 overflow-y-auto p-4">
            {error && (
              <p className="rounded-lg border border-[color:var(--err)]/30 bg-[color:var(--err)]/5 px-3 py-2 text-xs text-[color:var(--err)]">{error}</p>
            )}
            {notice && (
              <p className="flex items-center justify-between gap-2 rounded-lg border bg-background/50 px-3 py-2 text-xs text-muted-foreground">
                {notice}
                <button type="button" onClick={() => setNotice(null)} className="shrink-0 hover:text-foreground">
                  <X className="size-3.5" />
                </button>
              </p>
            )}

            {viewQ.isLoading && <p className="text-sm text-muted-foreground">{t('Loading...')}</p>}

            {view && (
              <>
                <section className="space-y-2">
                  <h3 className="text-xs font-medium text-muted-foreground uppercase tracking-wider">{t('kubeconfig.contexts', 'Contexts')}</h3>
                  <div className="overflow-x-auto rounded-xl border">
                    <table className="w-full text-sm">
                      <thead className="bg-background/40">
                        <tr className="border-b text-left text-xs text-muted-foreground">
                          <th className="px-3 py-2 font-medium" />
                          <th className="px-3 py-2 font-medium">{t('Name')}</th>
                          <th className="px-3 py-2 font-medium">{t('kubeconfig.cluster', 'Cluster')}</th>
                          <th className="px-3 py-2 font-medium">{t('kubeconfig.user', 'User')}</th>
                          <th className="px-3 py-2 font-medium">{t('ns.label')}</th>
                          <th className="px-3 py-2 text-center font-medium">MCP {t('kubeconfig.read', 'read')}</th>
                          <th className="px-3 py-2 text-center font-medium">MCP {t('kubeconfig.write', 'write')}</th>
                          <th className="px-3 py-2 font-medium" />
                        </tr>
                      </thead>
                      <tbody>
                        {view.contexts.map((c) => (
                          <ContextRow
                            key={c.name}
                            c={c}
                            isEditing={editing === c.name}
                            editName={editName}
                            onEditNameChange={setEditName}
                            editNamespace={editNamespace}
                            onEditNamespaceChange={setEditNamespace}
                            favorited={contextPrefs.favorites.includes(c.name)}
                            onToggleFavorite={() => toggleFavorite(c.name)}
                            readChecked={!mcp.readDisabledContexts.includes(c.name)}
                            readDisabled={!mcp.enabled}
                            onToggleRead={() => toggleReadDisabled(c.name)}
                            writeChecked={!mcp.readOnlyContexts.includes(c.name)}
                            writeDisabled={!mcp.allowWrite}
                            onToggleWrite={() => toggleWriteDisabled(c.name)}
                            confirmingDelete={confirmingDelete === c.name}
                            ping={pings[c.name]}
                            onStartEdit={() => startEdit(c.name, c.namespace)}
                            onSaveEdit={() => saveEdit(c.name)}
                            onCancelEdit={() => setEditing(null)}
                            onRequestDelete={() => setConfirmingDelete(c.name)}
                            onConfirmDelete={() => doDelete(c.name)}
                            onCancelDelete={() => setConfirmingDelete(null)}
                            onPing={() => doPing(c.name)}
                            onSetCurrent={() => run(() => setCurrentContext(c.name))}
                          />
                        ))}
                      </tbody>
                    </table>
                  </div>
                </section>

                <NewContextForm view={view} onCreate={(input) => run(() => createKubeconfigContext(input))} />

                <UsersSection
                  users={view.users}
                  isEditing={editingUser}
                  editName={editUserName}
                  onEditNameChange={setEditUserName}
                  confirmingDelete={confirmingDeleteUser}
                  onStartEdit={startEditUser}
                  onSaveEdit={saveEditUser}
                  onCancelEdit={() => setEditingUser(null)}
                  onRequestDelete={setConfirmingDeleteUser}
                  onConfirmDelete={doDeleteUser}
                  onCancelDelete={() => setConfirmingDeleteUser(null)}
                />
                <NewUserForm onCreate={(input) => run(() => createKubeconfigUser(input))} />

                <ImportSection open={importOpen} onOpenChange={setImportOpen} onCommitted={refresh} />
              </>
            )}
          </div>
        </div>
      </div>
    </>
  )
}

// ContextRow and RowActions are split out of the table body's .map() (rather
// than inlined, like every other row-shaped list in this file) specifically
// because the row's per-state action buttons pushed the enclosing dialog
// component's own cognitive complexity well past the lint limit — the same
// reasoning behind every registerXTool split in mcp_tools_read.go.
function ContextRow({
  c,
  isEditing,
  editName,
  onEditNameChange,
  editNamespace,
  onEditNamespaceChange,
  favorited,
  onToggleFavorite,
  readChecked,
  readDisabled,
  onToggleRead,
  writeChecked,
  writeDisabled,
  onToggleWrite,
  confirmingDelete,
  ping,
  onStartEdit,
  onSaveEdit,
  onCancelEdit,
  onRequestDelete,
  onConfirmDelete,
  onCancelDelete,
  onPing,
  onSetCurrent,
}: Readonly<{
  c: KubeconfigContextView
  isEditing: boolean
  editName: string
  onEditNameChange: (v: string) => void
  editNamespace: string
  onEditNamespaceChange: (v: string) => void
  favorited: boolean
  onToggleFavorite: () => void
  readChecked: boolean
  readDisabled: boolean
  onToggleRead: () => void
  writeChecked: boolean
  writeDisabled: boolean
  onToggleWrite: () => void
  confirmingDelete: boolean
  ping: PingResult | 'loading' | undefined
  onStartEdit: () => void
  onSaveEdit: () => void
  onCancelEdit: () => void
  onRequestDelete: () => void
  onConfirmDelete: () => void
  onCancelDelete: () => void
  onPing: () => void
  onSetCurrent: () => void
}>) {
  const t = useT()
  return (
    <tr className="border-b border-border/50 align-top last:border-0">
      <td className="px-3 py-2">
        <button
          type="button"
          onClick={onToggleFavorite}
          aria-label={favorited ? t('kubeconfig.unfavorite', 'Remove favorite') : t('kubeconfig.favorite', 'Favorite')}
          className={cn('transition-colors', favorited ? 'text-[color:var(--warn)]' : 'text-muted-foreground hover:text-foreground')}
        >
          <Star className="size-4" fill={favorited ? 'currentColor' : 'none'} />
        </button>
      </td>
      <td className="px-3 py-2">
        {isEditing ? (
          <input
            value={editName}
            onChange={(e) => onEditNameChange(e.target.value)}
            className="w-full min-w-32 rounded border bg-background/60 px-1.5 py-0.5 text-xs"
          />
        ) : (
          <span className="font-medium">
            {shortContext(c.name)}
            {c.current && <span className="ml-1.5 rounded bg-primary/15 px-1.5 py-0.5 text-[10px] font-medium text-primary">{t('current')}</span>}
          </span>
        )}
      </td>
      <td className="px-3 py-2 text-muted-foreground">{c.cluster}</td>
      <td className="px-3 py-2 text-muted-foreground">{c.user}</td>
      <td className="px-3 py-2">
        {isEditing ? (
          <input
            value={editNamespace}
            onChange={(e) => onEditNamespaceChange(e.target.value)}
            className="w-full min-w-24 rounded border bg-background/60 px-1.5 py-0.5 text-xs"
          />
        ) : (
          <span className="text-muted-foreground">{c.namespace}</span>
        )}
      </td>
      <td className="px-3 py-2 text-center">
        <Switch checked={readChecked} onChange={onToggleRead} label={`MCP read — ${c.name}`} disabled={readDisabled} />
      </td>
      <td className="px-3 py-2 text-center">
        <Switch
          checked={writeChecked}
          onChange={onToggleWrite}
          label={`MCP write — ${c.name}`}
          disabled={writeDisabled}
          activeClassName="bg-[color:var(--err)]"
        />
      </td>
      <td className="px-3 py-2">
        <RowActions
          current={c.current}
          isEditing={isEditing}
          confirmingDelete={confirmingDelete}
          ping={ping}
          onSaveEdit={onSaveEdit}
          onCancelEdit={onCancelEdit}
          onConfirmDelete={onConfirmDelete}
          onCancelDelete={onCancelDelete}
          onPing={onPing}
          onSetCurrent={onSetCurrent}
          onStartEdit={onStartEdit}
          onRequestDelete={onRequestDelete}
        />
      </td>
    </tr>
  )
}

// Each of the three states below (editing / confirming delete / normal) is
// returned early rather than nested in a ternary chain — the ping indicator
// within the "normal" state does the same (if/else into a variable) instead
// of a second nested ternary, per the same lint rule.
function RowActions({
  current,
  isEditing,
  confirmingDelete,
  ping,
  onSaveEdit,
  onCancelEdit,
  onConfirmDelete,
  onCancelDelete,
  onPing,
  onSetCurrent,
  onStartEdit,
  onRequestDelete,
}: Readonly<{
  current: boolean
  isEditing: boolean
  confirmingDelete: boolean
  ping: PingResult | 'loading' | undefined
  onSaveEdit: () => void
  onCancelEdit: () => void
  onConfirmDelete: () => void
  onCancelDelete: () => void
  onPing: () => void
  onSetCurrent: () => void
  onStartEdit: () => void
  onRequestDelete: () => void
}>) {
  const t = useT()

  if (isEditing) {
    return (
      <div className="flex items-center justify-end gap-1">
        <button type="button" onClick={onSaveEdit} className="rounded p-1 text-[color:var(--ok)] hover:bg-accent" aria-label={t('Save')}>
          <Check className="size-3.5" />
        </button>
        <button type="button" onClick={onCancelEdit} className="rounded p-1 text-muted-foreground hover:bg-accent" aria-label={t('Cancel')}>
          <X className="size-3.5" />
        </button>
      </div>
    )
  }

  if (confirmingDelete) {
    return (
      <div className="flex items-center justify-end gap-1">
        <button type="button" onClick={onConfirmDelete} className="rounded bg-[color:var(--err)]/90 px-1.5 py-0.5 text-[11px] text-white">
          {t('Confirm')}
        </button>
        <button type="button" onClick={onCancelDelete} className="text-[11px] text-muted-foreground hover:text-foreground">
          {t('Cancel')}
        </button>
      </div>
    )
  }

  let pingIndicator: ReactNode = null
  if (ping === 'loading') {
    pingIndicator = <span className="size-2 shrink-0 animate-pulse rounded-full bg-muted-foreground" title={t('kubeconfig.pinging', 'Testing…')} />
  } else if (ping) {
    pingIndicator = (
      <span
        className={cn('size-2 shrink-0 rounded-full', ping.reachable ? 'bg-[color:var(--ok)]' : 'bg-[color:var(--err)]')}
        title={ping.reachable ? `${ping.latencyMs}ms` : ping.error}
      />
    )
  }

  return (
    <div className="flex items-center justify-end gap-1">
      {pingIndicator}
      <button
        type="button"
        onClick={onPing}
        className="rounded p-1 text-muted-foreground hover:bg-accent hover:text-foreground"
        title={t('kubeconfig.testConnection', 'Test connection')}
      >
        <Plug className="size-3.5" />
      </button>
      {!current && (
        <button
          type="button"
          onClick={onSetCurrent}
          className="rounded p-1 text-muted-foreground hover:bg-accent hover:text-foreground"
          title={t('kubeconfig.setCurrent', 'Set as current context')}
        >
          <Check className="size-3.5" />
        </button>
      )}
      <button type="button" onClick={onStartEdit} className="rounded p-1 text-muted-foreground hover:bg-accent hover:text-foreground" title={t('Edit')}>
        <Pencil className="size-3.5" />
      </button>
      <button
        type="button"
        onClick={onRequestDelete}
        className="rounded p-1 text-muted-foreground hover:bg-accent hover:text-[color:var(--err)]"
        title={t('Delete')}
      >
        <Trash2 className="size-3.5" />
      </button>
    </div>
  )
}

function NewContextForm({
  view,
  onCreate,
}: Readonly<{
  view: { clusters: { name: string }[]; users: { name: string }[] }
  onCreate: (input: { name: string; cluster: string; user: string; namespace?: string }) => void
}>) {
  const t = useT()
  const [name, setName] = useState('')
  const [cluster, setCluster] = useState('')
  const [user, setUser] = useState('')
  const [namespace, setNamespace] = useState('')

  const submit = (e: React.FormEvent) => {
    e.preventDefault()
    if (!name.trim() || !cluster || !user) return
    onCreate({ name: name.trim(), cluster, user, namespace: namespace.trim() || undefined })
    setName('')
    setNamespace('')
  }

  return (
    <section className="space-y-2 rounded-xl border bg-background/40 p-3">
      <h3 className="text-xs font-medium text-muted-foreground uppercase tracking-wider">
        {t('kubeconfig.newContext', 'New context from existing cluster + user')}
      </h3>
      <form onSubmit={submit} className="flex flex-wrap items-end gap-2">
        <label className="flex flex-col gap-1 text-xs text-muted-foreground">
          {t('Name')}
          <input value={name} onChange={(e) => setName(e.target.value)} className="w-32 rounded-lg border bg-background/60 px-2 py-1 text-xs" required />
        </label>
        <label className="flex flex-col gap-1 text-xs text-muted-foreground">
          {t('kubeconfig.cluster', 'Cluster')}
          <select value={cluster} onChange={(e) => setCluster(e.target.value)} className="w-36 rounded-lg border bg-background/60 px-2 py-1 text-xs" required>
            <option value="" disabled>
              —
            </option>
            {view.clusters.map((c) => (
              <option key={c.name} value={c.name}>
                {c.name}
              </option>
            ))}
          </select>
        </label>
        <label className="flex flex-col gap-1 text-xs text-muted-foreground">
          {t('kubeconfig.user', 'User')}
          <select value={user} onChange={(e) => setUser(e.target.value)} className="w-36 rounded-lg border bg-background/60 px-2 py-1 text-xs" required>
            <option value="" disabled>
              —
            </option>
            {view.users.map((u) => (
              <option key={u.name} value={u.name}>
                {u.name}
              </option>
            ))}
          </select>
        </label>
        <label className="flex flex-col gap-1 text-xs text-muted-foreground">
          {t('ns.label')}
          <input
            value={namespace}
            onChange={(e) => setNamespace(e.target.value)}
            placeholder="default"
            className="w-28 rounded-lg border bg-background/60 px-2 py-1 text-xs"
          />
        </label>
        <button type="submit" className="rounded-lg bg-primary px-3 py-1.5 text-xs font-medium text-primary-foreground">
          {t('kubeconfig.create', 'Create')}
        </button>
      </form>
    </section>
  )
}

function UsersSection({
  users,
  isEditing,
  editName,
  onEditNameChange,
  confirmingDelete,
  onStartEdit,
  onSaveEdit,
  onCancelEdit,
  onRequestDelete,
  onConfirmDelete,
  onCancelDelete,
}: Readonly<{
  users: KubeconfigUserView[]
  isEditing: string | null
  editName: string
  onEditNameChange: (v: string) => void
  confirmingDelete: string | null
  onStartEdit: (name: string) => void
  onSaveEdit: (name: string) => void
  onCancelEdit: () => void
  onRequestDelete: (name: string) => void
  onConfirmDelete: (name: string) => void
  onCancelDelete: () => void
}>) {
  const t = useT()
  return (
    <section className="space-y-2">
      <h3 className="text-xs font-medium text-muted-foreground uppercase tracking-wider">{t('kubeconfig.users', 'Users')}</h3>
      <div className="overflow-x-auto rounded-xl border">
        <table className="w-full text-sm">
          <thead className="bg-background/40">
            <tr className="border-b text-left text-xs text-muted-foreground">
              <th className="px-3 py-2 font-medium">{t('Name')}</th>
              <th className="px-3 py-2 font-medium">{t('kubeconfig.authMethod', 'Auth')}</th>
              <th className="px-3 py-2 font-medium" />
            </tr>
          </thead>
          <tbody>
            {users.map((u) => (
              <UserRow
                key={u.name}
                u={u}
                isEditing={isEditing === u.name}
                editName={editName}
                onEditNameChange={onEditNameChange}
                confirmingDelete={confirmingDelete === u.name}
                onStartEdit={() => onStartEdit(u.name)}
                onSaveEdit={() => onSaveEdit(u.name)}
                onCancelEdit={onCancelEdit}
                onRequestDelete={() => onRequestDelete(u.name)}
                onConfirmDelete={() => onConfirmDelete(u.name)}
                onCancelDelete={onCancelDelete}
              />
            ))}
          </tbody>
        </table>
      </div>
    </section>
  )
}

// Split out of UsersSection's .map() for the same reason ContextRow is:
// the row's editing/confirm-delete states pushed the enclosing component's
// cognitive complexity past the lint limit.
function UserRow({
  u,
  isEditing,
  editName,
  onEditNameChange,
  confirmingDelete,
  onStartEdit,
  onSaveEdit,
  onCancelEdit,
  onRequestDelete,
  onConfirmDelete,
  onCancelDelete,
}: Readonly<{
  u: KubeconfigUserView
  isEditing: boolean
  editName: string
  onEditNameChange: (v: string) => void
  confirmingDelete: boolean
  onStartEdit: () => void
  onSaveEdit: () => void
  onCancelEdit: () => void
  onRequestDelete: () => void
  onConfirmDelete: () => void
  onCancelDelete: () => void
}>) {
  const t = useT()
  return (
    <tr className="border-b border-border/50 align-top last:border-0">
      <td className="px-3 py-2">
        {isEditing ? (
          <input
            value={editName}
            onChange={(e) => onEditNameChange(e.target.value)}
            className="w-full min-w-32 rounded border bg-background/60 px-1.5 py-0.5 text-xs"
          />
        ) : (
          <span className="font-medium">{u.name}</span>
        )}
      </td>
      <td className="px-3 py-2">
        <div className="flex flex-wrap items-center gap-1.5">
          {u.execCommand && <span className="rounded bg-muted px-1.5 py-0.5 text-[10px] text-muted-foreground">exec: {u.execCommand}</span>}
          {u.authProvider && <span className="rounded bg-muted px-1.5 py-0.5 text-[10px] text-muted-foreground">{u.authProvider}</span>}
          {u.username && <span className="rounded bg-muted px-1.5 py-0.5 text-[10px] text-muted-foreground">user: {u.username}</span>}
          {u.hasToken && <RevealSecret user={u.name} field="token" label={t('kubeconfig.token', 'Token')} />}
          {u.hasPassword && <RevealSecret user={u.name} field="password" label={t('kubeconfig.password', 'Password')} />}
          {u.hasClientKeyData && <RevealSecret user={u.name} field="clientKeyData" label={t('kubeconfig.clientKey', 'Client key')} />}
          {u.hasClientCertificateData && <RevealSecret user={u.name} field="clientCertificateData" label={t('kubeconfig.clientCert', 'Client cert')} />}
        </div>
      </td>
      <td className="px-3 py-2">
        <UserRowActions
          isEditing={isEditing}
          confirmingDelete={confirmingDelete}
          onSaveEdit={onSaveEdit}
          onCancelEdit={onCancelEdit}
          onConfirmDelete={onConfirmDelete}
          onCancelDelete={onCancelDelete}
          onStartEdit={onStartEdit}
          onRequestDelete={onRequestDelete}
        />
      </td>
    </tr>
  )
}

function UserRowActions({
  isEditing,
  confirmingDelete,
  onSaveEdit,
  onCancelEdit,
  onConfirmDelete,
  onCancelDelete,
  onStartEdit,
  onRequestDelete,
}: Readonly<{
  isEditing: boolean
  confirmingDelete: boolean
  onSaveEdit: () => void
  onCancelEdit: () => void
  onConfirmDelete: () => void
  onCancelDelete: () => void
  onStartEdit: () => void
  onRequestDelete: () => void
}>) {
  const t = useT()

  if (isEditing) {
    return (
      <div className="flex items-center justify-end gap-1">
        <button type="button" onClick={onSaveEdit} className="rounded p-1 text-[color:var(--ok)] hover:bg-accent" aria-label={t('Save')}>
          <Check className="size-3.5" />
        </button>
        <button type="button" onClick={onCancelEdit} className="rounded p-1 text-muted-foreground hover:bg-accent" aria-label={t('Cancel')}>
          <X className="size-3.5" />
        </button>
      </div>
    )
  }

  if (confirmingDelete) {
    return (
      <div className="flex items-center justify-end gap-1">
        <button type="button" onClick={onConfirmDelete} className="rounded bg-[color:var(--err)]/90 px-1.5 py-0.5 text-[11px] text-white">
          {t('Confirm')}
        </button>
        <button type="button" onClick={onCancelDelete} className="text-[11px] text-muted-foreground hover:text-foreground">
          {t('Cancel')}
        </button>
      </div>
    )
  }

  return (
    <div className="flex items-center justify-end gap-1">
      <button type="button" onClick={onStartEdit} className="rounded p-1 text-muted-foreground hover:bg-accent hover:text-foreground" title={t('Edit')}>
        <Pencil className="size-3.5" />
      </button>
      <button
        type="button"
        onClick={onRequestDelete}
        className="rounded p-1 text-muted-foreground hover:bg-accent hover:text-[color:var(--err)]"
        title={t('Delete')}
      >
        <Trash2 className="size-3.5" />
      </button>
    </div>
  )
}

type NewUserAuthType = 'token' | 'basic' | 'cert'

// New-user credential form: unlike NewContextForm (which only composes
// existing entries), this hand-authors a brand-new AuthInfo — so it's
// deliberately limited to the three credential shapes someone could
// reasonably type/paste in a browser (token, username+password, client
// cert+key). Exec-plugin and auth-provider users (aws/gcloud/az CLI output)
// aren't offered here; see UserAuthSpec's doc comment on the backend.
function NewUserForm({ onCreate }: Readonly<{ onCreate: (input: CreateKubeconfigUserInput) => void }>) {
  const t = useT()
  const [name, setName] = useState('')
  const [authType, setAuthType] = useState<NewUserAuthType>('token')
  const [token, setToken] = useState('')
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [cert, setCert] = useState('')
  const [key, setKey] = useState('')

  const reset = () => {
    setName('')
    setToken('')
    setUsername('')
    setPassword('')
    setCert('')
    setKey('')
  }

  const submit = (e: React.FormEvent) => {
    e.preventDefault()
    if (!name.trim()) return
    const input: CreateKubeconfigUserInput = { name: name.trim() }
    if (authType === 'token') {
      if (!token) return
      input.token = token
    } else if (authType === 'basic') {
      if (!username && !password) return
      input.username = username
      input.password = password
    } else {
      if (!cert || !key) return
      input.clientCertificateData = cert
      input.clientKeyData = key
    }
    onCreate(input)
    reset()
  }

  return (
    <section className="space-y-2 rounded-xl border bg-background/40 p-3">
      <h3 className="text-xs font-medium text-muted-foreground uppercase tracking-wider">{t('kubeconfig.newUser', 'New user')}</h3>
      <form onSubmit={submit} className="space-y-2">
        <div className="flex flex-wrap items-end gap-2">
          <label className="flex flex-col gap-1 text-xs text-muted-foreground">
            {t('Name')}
            <input value={name} onChange={(e) => setName(e.target.value)} className="w-40 rounded-lg border bg-background/60 px-2 py-1 text-xs" required />
          </label>
          <label className="flex flex-col gap-1 text-xs text-muted-foreground">
            {t('kubeconfig.authMethod', 'Auth')}
            <select
              value={authType}
              onChange={(e) => setAuthType(e.target.value as NewUserAuthType)}
              className="w-40 rounded-lg border bg-background/60 px-2 py-1 text-xs"
            >
              <option value="token">{t('kubeconfig.token', 'Token')}</option>
              <option value="basic">{t('kubeconfig.usernamePassword', 'Username / password')}</option>
              <option value="cert">{t('kubeconfig.clientCertKey', 'Client certificate / key')}</option>
            </select>
          </label>
          {authType === 'token' && (
            <label className="flex flex-col gap-1 text-xs text-muted-foreground">
              {t('kubeconfig.token', 'Token')}
              <input
                type="password"
                value={token}
                onChange={(e) => setToken(e.target.value)}
                className="w-56 rounded-lg border bg-background/60 px-2 py-1 text-xs font-mono"
                required
              />
            </label>
          )}
          {authType === 'basic' && (
            <>
              <label className="flex flex-col gap-1 text-xs text-muted-foreground">
                {t('kubeconfig.username', 'Username')}
                <input value={username} onChange={(e) => setUsername(e.target.value)} className="w-40 rounded-lg border bg-background/60 px-2 py-1 text-xs" />
              </label>
              <label className="flex flex-col gap-1 text-xs text-muted-foreground">
                {t('kubeconfig.password', 'Password')}
                <input
                  type="password"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  className="w-40 rounded-lg border bg-background/60 px-2 py-1 text-xs"
                />
              </label>
            </>
          )}
          <button type="submit" className="rounded-lg bg-primary px-3 py-1.5 text-xs font-medium text-primary-foreground">
            {t('kubeconfig.create', 'Create')}
          </button>
        </div>
        {authType === 'cert' && (
          <div className="flex flex-wrap gap-2">
            <label className="flex min-w-56 flex-1 flex-col gap-1 text-xs text-muted-foreground">
              {t('kubeconfig.clientCert', 'Client cert')} (PEM)
              <textarea
                value={cert}
                onChange={(e) => setCert(e.target.value)}
                rows={3}
                className="w-full rounded-lg border bg-background/60 px-2 py-1.5 font-mono text-[11px]"
              />
            </label>
            <label className="flex min-w-56 flex-1 flex-col gap-1 text-xs text-muted-foreground">
              {t('kubeconfig.clientKey', 'Client key')} (PEM)
              <textarea
                value={key}
                onChange={(e) => setKey(e.target.value)}
                rows={3}
                className="w-full rounded-lg border bg-background/60 px-2 py-1.5 font-mono text-[11px]"
              />
            </label>
          </div>
        )}
      </form>
    </section>
  )
}

// Backend masks every secret by default (View() never carries a raw value) —
// the first click here does a real, audited round-trip via
// revealKubeconfigSecret rather than toggling display of something already
// present, unlike MCPControls' token reveal (which has the value locally
// the whole time and only toggles CSS visibility).
function RevealSecret({ user, field, label }: Readonly<{ user: string; field: RevealField; label: string }>) {
  const t = useT()
  const [value, setValue] = useState<string | null>(null)
  const [revealed, setRevealed] = useState(false)
  const [loading, setLoading] = useState(false)

  const toggle = async () => {
    if (revealed) {
      setRevealed(false)
      return
    }
    if (value === null) {
      setLoading(true)
      try {
        setValue(await revealKubeconfigSecret(user, field))
      } catch {
        setValue(t('kubeconfig.revealFailed', 'failed to load'))
      } finally {
        setLoading(false)
      }
    }
    setRevealed(true)
  }

  return (
    <button
      type="button"
      onClick={() => void toggle()}
      className="inline-flex items-center gap-1 rounded bg-muted px-1.5 py-0.5 text-[10px] text-muted-foreground transition-colors hover:bg-accent"
    >
      {revealed ? <EyeOff className="size-3" /> : <Eye className="size-3" />}
      {label}
      {revealed && value !== null && <span className="max-w-32 truncate font-mono">{loading ? '…' : value}</span>}
    </button>
  )
}

function ImportSection({ open, onOpenChange, onCommitted }: Readonly<{ open: boolean; onOpenChange: (v: boolean) => void; onCommitted: () => Promise<void> }>) {
  const t = useT()
  const [yaml, setYaml] = useState('')
  const [preview, setPreview] = useState<ImportPreview | null>(null)
  const [overwrite, setOverwrite] = useState<Set<string>>(new Set())
  const [error, setError] = useState<string | null>(null)

  if (!open) {
    return (
      <button type="button" onClick={() => onOpenChange(true)} className="flex items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground">
        <Upload className="size-3.5" /> {t('kubeconfig.importAnother', 'Import another kubeconfig')}
      </button>
    )
  }

  const conflicts = preview ? [...preview.conflictingContexts, ...preview.conflictingClusters, ...preview.conflictingUsers] : []

  const doPreview = async () => {
    setError(null)
    try {
      setPreview(await previewKubeconfigImport(yaml))
      setOverwrite(new Set())
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    }
  }
  const doCommit = async () => {
    setError(null)
    try {
      await commitKubeconfigImport(yaml, [...overwrite])
      setYaml('')
      setPreview(null)
      onOpenChange(false)
      await onCommitted()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    }
  }

  return (
    <section className="space-y-2 rounded-xl border bg-background/40 p-3">
      <div className="flex items-center justify-between">
        <h3 className="text-xs font-medium text-muted-foreground uppercase tracking-wider">{t('kubeconfig.import', 'Import kubeconfig')}</h3>
        <button type="button" onClick={() => onOpenChange(false)} className="text-muted-foreground hover:text-foreground">
          <X className="size-3.5" />
        </button>
      </div>
      {error && <p className="text-xs text-[color:var(--err)]">{error}</p>}
      <textarea
        value={yaml}
        onChange={(e) => {
          setYaml(e.target.value)
          setPreview(null)
        }}
        placeholder={t('kubeconfig.pasteYaml', 'Paste a kubeconfig YAML here')}
        rows={6}
        className="w-full rounded-lg border bg-background/60 px-2 py-1.5 font-mono text-xs"
      />
      {!preview ? (
        <button
          type="button"
          onClick={() => void doPreview()}
          disabled={!yaml.trim()}
          className="rounded-lg border px-3 py-1.5 text-xs font-medium disabled:opacity-40"
        >
          {t('kubeconfig.preview', 'Preview')}
        </button>
      ) : (
        <div className="space-y-2">
          <p className="text-xs text-muted-foreground">
            {t('kubeconfig.willAdd', 'Will add')}: {preview.addedContexts.length + preview.addedClusters.length + preview.addedUsers.length}
            {conflicts.length > 0 && ` · ${t('kubeconfig.conflicts', 'conflicts')}: ${conflicts.length}`}
          </p>
          {conflicts.length > 0 && (
            <div className="space-y-1">
              <p className="text-[11px] text-muted-foreground">
                {t('kubeconfig.selectOverwrite', 'Select names to overwrite (unchecked entries are skipped):')}
              </p>
              <div className="flex flex-wrap gap-2">
                {conflicts.map((name) => (
                  <label key={name} className="flex items-center gap-1 text-[11px]">
                    <input
                      type="checkbox"
                      checked={overwrite.has(name)}
                      onChange={(e) =>
                        setOverwrite((prev) => {
                          const next = new Set(prev)
                          if (e.target.checked) next.add(name)
                          else next.delete(name)
                          return next
                        })
                      }
                    />
                    {name}
                  </label>
                ))}
              </div>
            </div>
          )}
          <button type="button" onClick={() => void doCommit()} className="rounded-lg bg-primary px-3 py-1.5 text-xs font-medium text-primary-foreground">
            {t('kubeconfig.commitImport', 'Commit merge')}
          </button>
        </div>
      )}
    </section>
  )
}
