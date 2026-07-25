# Architecture & Refactor Roadmap

This document describes how **Netsk8 Navigator** is structured and the phased
refactor that prepares it for the near-term goals (open source, native + Docker
builds, automated tests, i18n, app/cluster preferences, standard resources + CRDs,
and multi–Kubernetes-version support).

## Overview

```
netk8s-navigator/
├── backend/                 Go API server (reads kubeconfig, talks to clusters)
│   ├── main.go              wiring: Manager + HTTP server
│   └── internal/
│       ├── kube/            cluster access (clients, discovery, conversions)
│       └── api/             HTTP handlers (REST + SSE + WebSocket)
└── frontend/                React + Vite + TS SPA
    └── src/
        ├── lib/             api client, hooks, utils
        └── components/      views, drawers, tables, charts
```

The backend reads the local kubeconfig and exposes a REST API the SPA consumes
(proxied in dev via Vite `/api → :8080`). Go was chosen so exec-based credential
plugins (EKS/GKE/OIDC) resolve natively.

## The generic resource layer (foundation)

`kube.Manager` caches, per context: the typed `*kubernetes.Clientset`, a
`dynamic.Interface`, and a **discovery-backed `RESTMapper`**.

- `Manager.DynamicFor(ctx)` — one dynamic client for every resource (core + CRDs).
- `Manager.RESTMapperFor(ctx)` — resolves a resource to the GVR the cluster
  actually serves.
- `Manager.ResolveResource(ctx, plural)` → `kube.Resource{GVR, Namespaced}`.

**Why this matters:** `apiVersion`/`kind` and scope change across Kubernetes
versions and between core resources and CRDs. Resolving the GVR at request time
(instead of hardcoding it) is what makes the app version-agnostic and CRD-ready.

`internal/api/manifest.go` already uses this: get/apply work on `unstructured`
objects for any slug, no per-kind `switch`. `internal/api/crd.go` uses the same
dynamic client for route CRDs (HTTPRoute, IngressRoute, VirtualService, …),
discovered dynamically via `/routekinds`.

## Refactor roadmap (phased)

Each phase keeps `go build ./...`, `tsc -b`, and `pnpm build` green.

### ✅ Phase 1 — shared dynamic/RESTMapper foundation (done)
- Dynamic client + RESTMapper on `Manager`; `ResolveResource`.
- `manifest.go` rewritten to be generic + multi-version (removed the hardcoded
  GVK switches). Unifies typed-kind and CRD manifest handling.

### ✅ Phase 2 — generic resource registry, backend (done)
Adding a standard resource is now one catalog entry, not a new handler.
- `api/catalog.go`: `resourceCatalog` maps a plural resource → a **projection**
  that reuses the typed `kube.To*View` builders (dynamic object → typed via
  `runtime.DefaultUnstructuredConverter` → view). One generic
  `GET /resources/{resource}` lister replaced the four per-kind handlers;
  `resources.go` is gone.
- `detail.go`: `detailBuilders` + `detailFrom[T]` adapt the rich typed detail
  builders to the dynamic path. The per-kind `buildDetail` Get-switch is gone;
  `handleDetail` fetches via the shared `getUnstructured` (multi-version).
- Everything now flows through the dynamic client + RESTMapper (multi-version,
  CRD-ready). Pods keep the SSE watch path.

### ✅ Phase 2b — frontend catalog-driven (done)
- `lib/resources.tsx`: a single `RESOURCES` catalog (`{key, label, icon, group,
  resource, manifest, facets, columns, usage?}`) is the source of truth.
- `ResourceNav`, `CommandPalette`, and `App` routing all derive from it; the
  sidebar composes catalog groups + specials (overview/pods/topology) + the
  discovered route CRDs. `ResourceView` is now generic (`{def}`), fetching via the
  generic `api.list(ctx, resource, ns)`.
- **Net effect:** adding a standard resource is now one entry in the backend
  catalog + one in `RESOURCES` — no new handlers, no new views.

### ✅ Phase 3 — preferences layer (done); screens are a follow-up feature
- `internal/config`: a concurrency-safe store persisted via `os.UserConfigDir()`
  (`netsk8/config.json` — cross-platform, ready for native builds). Payloads are
  opaque JSON so preferences evolve without backend changes.
- REST: `GET/PUT /api/preferences` and `GET/PUT /api/contexts/{ctx}/preferences`.
- Frontend `lib/preferences.ts`: one typed `AppPreferences`, localStorage-first
  (no load flash) + best-effort mirror to the API, hydrated once at startup.
  Metrics-refresh and the Vanta background now live here (their ad-hoc stores are
  gone).
- **Follow-ups:** move remaining app-level `netsk8.*` keys (and cluster default
  namespace) into prefs; build the **Preferences** screen (app) + per-cluster
  settings panel. Per-table sort order stays widget-local.

### ✅ Phase 4 — i18n scaffolding (done); string extraction is incremental
- `lib/i18n.ts`: a light `t(key, fallback)` layer with `pt-BR` + `en` dictionaries,
  language read from app preferences, a `useT()` hook (re-renders on switch), and a
  sidebar `LanguageToggle`. Falls back pt-BR → key, so un-keyed strings still show.
- Migrated a representative slice (nav groups/labels, header, empty state, control
  labels) to prove the pattern. **Remaining:** extract the rest view by view — no
  behavior change, just add keys to both dicts and swap the literal for `t('…')`.
- Backend responses stay locale-neutral (codes/enums); formatting is UI-side.

### Phase 5 — standard resources + more CRDs
- Once Phase 2 lands, add entries for the remaining core kinds (StatefulSets,
  DaemonSets, Jobs, CronJobs, PVs/PVCs, Secrets [values masked], Namespaces,
  Nodes list, RBAC, NetworkPolicies, …) and common CRDs.
- Multi-version handled automatically by `ResolveResource`.

## Cross-cutting: testability, builds, open source

- **Testability:** pure logic (projections, `waitingReason`, detail builders,
  `extractHosts`/`extractRefs`, status triage) lives in functions independent of
  HTTP/clients — unit-testable with `unstructured`/typed fixtures. Handlers should
  stay thin. Table-driven Go tests + a few Vitest component tests are the target.
- **Builds:** a single Go binary can embed the built SPA (`embed.FS`) and serve it,
  giving one artifact per OS/arch via `GOOS/GOARCH` (+ Docker). The dev flow keeps
  Vite; production serves the embedded assets.
- **Open source:** add `LICENSE` (project owner's choice), root `README.md`,
  `CONTRIBUTING.md`, and CI (build + test + lint) before publishing.
