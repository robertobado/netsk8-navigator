<p align="center">
  <img src="docs/logo-animated.svg" width="190" alt="Netsk8 Navigator" />
</p>

<h1 align="center">Netsk8 Navigator</h1>

<p align="center">
  A Kubernetes cluster navigator in the browser — think Lens or k9s, but web.<br/>
  The name is a tribute to the old Netscape Navigator.
</p>

<p align="center">
  <a href="https://github.com/robertobado/netsk8-navigator/actions/workflows/ci.yml"><img src="https://github.com/robertobado/netsk8-navigator/actions/workflows/ci.yml/badge.svg" alt="CI" /></a>
  <a href="https://github.com/robertobado/netsk8-navigator/releases/latest"><img src="https://img.shields.io/github/v/release/robertobado/netsk8-navigator" alt="Latest release" /></a>
  <a href="LICENSE"><img src="https://img.shields.io/github/license/robertobado/netsk8-navigator" alt="License" /></a>
</p>

<p align="center">
  <a href="https://sonarcloud.io/summary/new_code?id=robertobado_netsk8-navigator"><img src="https://sonarcloud.io/api/project_badges/measure?project=robertobado_netsk8-navigator&metric=alert_status" alt="Quality Gate Status" /></a>
  <a href="https://sonarcloud.io/summary/new_code?id=robertobado_netsk8-navigator"><img src="https://sonarcloud.io/api/project_badges/measure?project=robertobado_netsk8-navigator&metric=reliability_rating" alt="Reliability Rating" /></a>
  <a href="https://sonarcloud.io/summary/new_code?id=robertobado_netsk8-navigator"><img src="https://sonarcloud.io/api/project_badges/measure?project=robertobado_netsk8-navigator&metric=security_rating" alt="Security Rating" /></a>
  <a href="https://sonarcloud.io/summary/new_code?id=robertobado_netsk8-navigator"><img src="https://sonarcloud.io/api/project_badges/measure?project=robertobado_netsk8-navigator&metric=sqale_rating" alt="Maintainability Rating" /></a>
  <a href="https://sonarcloud.io/summary/new_code?id=robertobado_netsk8-navigator"><img src="https://sonarcloud.io/api/project_badges/measure?project=robertobado_netsk8-navigator&metric=coverage" alt="Coverage" /></a>
</p>

<p align="center">
  <img src="docs/screenshots/overview.png" alt="Netsk8 Navigator — cluster overview with live metrics and a live pods table" width="100%" />
</p>

---

## What it is

Netsk8 Navigator is an SPA that reads your `kubeconfig` and browses any
Kubernetes cluster (standard resources + CRDs) through a thin Go backend
that talks directly to the cluster API. Nothing installed on the cluster,
no agent, no state beyond your local preferences — the same trust model as
running `kubectl` from your own machine.

- 🗂️ **All standard resources** (Workloads, Network, Config, Storage, RBAC,
  Governance, Cluster) in filterable, sortable tables, with row expansion
  for relationships — Node → workloads, Namespace → resources, ConfigMap/
  Secret → consumers, ServiceAccount → bindings, and more.
- 🧩 **Generic CRD browser** for route resources (Gateway API, Traefik
  IngressRoute, Istio VirtualService, Contour HTTPProxy).
- 📝 **Detail view + YAML manifest editor** (Monaco) for reading and
  editing any object in place.
- 🔌 **Live logs, exec, and events** over SSE/WebSocket — pods stream in
  real time, no manual refresh.
- 📊 **CPU/memory metrics** at cluster, node, and pod level when the
  cluster exposes `metrics-server` or Prometheus.
- 🕸️ **Cluster topology** — a live graph of how workloads, pods, and
  services connect within a namespace.
- 🌐 **Multi-cluster & multi-version** — switch kubeconfig context without
  restarting anything; every resource is resolved via discovery/RESTMapper
  at request time, so it works against whatever Kubernetes version the
  cluster actually serves.

## See it in action

<table>
  <tr>
    <td width="50%"><img src="docs/screenshots/deployments.png" alt="Deployments table with live CPU/memory columns" /><br/><sub>Filterable, sortable resource tables</sub></td>
    <td width="50%"><img src="docs/screenshots/topology.png" alt="Cluster topology graph for a namespace" /><br/><sub>Live topology: workloads → pods → services</sub></td>
  </tr>
  <tr>
    <td width="50%"><img src="docs/screenshots/manifest.png" alt="YAML manifest editor powered by Monaco" /><br/><sub>Read/edit any object's YAML in place</sub></td>
    <td width="50%"><img src="docs/screenshots/events.png" alt="Live cluster events feed" /><br/><sub>Cluster-wide events, live and filterable</sub></td>
  </tr>
</table>

## Quick start

Ready-to-run binaries (Linux/macOS/Windows) + a Docker image on every
[release](https://github.com/robertobado/netsk8-navigator/releases) — no
need for Go/Node installed. Download the file for your platform, extract it
and run:

```bash
tar xzf netsk8-navigator_*_darwin_arm64.tar.gz   # or linux_amd64, windows_amd64.zip, etc.
./netsk8-navigator
```

Prefer Docker, or building from source? Keep reading.

### Docker

```bash
docker build -t netsk8-navigator .
docker run --rm -p 127.0.0.1:8080:8080 \
  -v "$HOME/.kube:/kube:ro" -e KUBECONFIG=/kube/config \
  netsk8-navigator
```

Map the port to `127.0.0.1` on the host only (as above) to keep the same
security model — without that, `-p 8080:8080` exposes the backend without
authentication to anything on your network.

If `~/.kube/config` is a **symlink pointing outside `~/.kube`** (common
with tools that switch contexts by swapping the link, e.g. environments
with multiple clusters), the mount above won't resolve the target — the
container only sees its own `~/.kube`. Mount the real file instead:

```bash
docker run --rm -p 127.0.0.1:8080:8080 \
  -v "$(readlink -f ~/.kube/config):/kube/config:ro" -e KUBECONFIG=/kube/config \
  netsk8-navigator
```

### Docker Compose

Equivalent to the `docker run` above, declaratively — see
[`docker-compose.yml`](docker-compose.yml) at the repo root:

```bash
docker compose up
```

It mounts `~/.kube/config` read-only and keeps the same loopback-only port
binding by default. Point it at a different kubeconfig file (e.g. the
resolved target of a symlinked one) without editing the file:

```bash
KUBECONFIG_HOST_PATH=$(readlink -f ~/.kube/config) docker compose up
```

### From source

Prerequisites: Go 1.26+, Node 20+, [pnpm](https://pnpm.io).

```bash
# Backend — API at http://127.0.0.1:8080 (reads ~/.kube/config, or $KUBECONFIG)
cd backend
go run .

# Frontend — dev server at http://localhost:5173, proxying /api → :8080
cd frontend
pnpm install
pnpm dev
```

Open `http://localhost:5173`.

```bash
# Useful commands
cd backend && go build ./... && go vet ./... && go test ./...          # backend
cd frontend && pnpm exec tsc -b && pnpm build                          # typecheck + build
cd frontend && pnpm exec oxlint src                                    # lint (the project's linter)
```

### Single binary (no Vite, no separate process)

Building the frontend before the backend embeds the SPA into the Go binary
(`internal/web`, via `go:embed`) — one process, one port, API and UI
together:

```bash
cd frontend && pnpm install && pnpm build   # generates backend/internal/web/dist
cd ../backend && go build -o netsk8-navigator .
ADDR=127.0.0.1:8080 ./netsk8-navigator      # open http://127.0.0.1:8080
```

Without the `pnpm build` step, `go build` still works normally — there's
just no embedded UI (the log says "no embedded frontend build"); this is
the normal path when you only want the API, or you're iterating on the
backend in dev.

## Architecture

```text
backend/    Go — client-go dynamic client + discovery RESTMapper, thin REST API
frontend/   React + Vite + TypeScript — Tailwind v4, TanStack Table/Query, Monaco
```

The design is **catalog-driven**: adding a standard resource is one entry
in the backend catalog (`backend/internal/api/catalog.go`) + one entry in
the frontend catalog (`frontend/src/lib/resources.tsx`) — no new handlers
or views. Full details on the extension pattern in
[ARCHITECTURE.md](ARCHITECTURE.md).

## Security model

This backend **has no authentication, no TLS, and uses CORS `*`**. It can
also mutate the cluster (apply manifests via the Monaco editor), open an
`exec` session in any pod, and return decoded `Secret` values. It's meant
for **local use, on your own machine, with your own kubeconfig** — the
same trust level as running `kubectl` directly.

- By default the backend listens only on `127.0.0.1:8080` (loopback). Only
  change `ADDR` to expose it on another interface if you understand the
  implications — that grants any process/machine that can reach the port
  the same access your kubeconfig credentials have.
- Don't run this as a shared service or behind an internet-facing proxy
  without putting authentication/authorization in front of it.

## Kubernetes (Helm)

A chart is included at [`charts/netsk8-navigator`](charts/netsk8-navigator):

```bash
helm install netsk8-navigator ./charts/netsk8-navigator
kubectl port-forward svc/netsk8-navigator 8080:8080
```

**Kubeconfig:** by default the app runs with no kubeconfig at all, so the
backend falls back to the pod's own in-cluster service account — it browses
whatever cluster it's deployed into, no configuration needed. To instead
point it at one or more *other* clusters (same as running the binary
locally with `$KUBECONFIG` set), create a Secret from an existing
kubeconfig file and point the chart at it:

```bash
kubectl create secret generic my-kubeconfig --from-file=config=$HOME/.kube/config
helm install netsk8-navigator ./charts/netsk8-navigator \
  --set kubeconfig.enabled=true \
  --set kubeconfig.secretName=my-kubeconfig
```

**RBAC:** the chart's default `ClusterRole` grants the same broad,
kubectl-equivalent access described in *Security model* below —
cluster-wide read/write on every resource, since that's what lets the app
browse anything, apply manifests, exec into pods, and read Secret values.
Set `rbac.create=false` and bind your own, more restricted Role to
`serviceAccount.name` if you want a scoped-down deployment (e.g. read-only,
or limited to specific namespaces).

**Security:** this backend still has no built-in authentication once
deployed to a cluster. `service.type` defaults to `ClusterIP` (not reachable
outside the cluster on its own) — if you enable the chart's `ingress` or
switch to a `LoadBalancer`, put your own authentication in front of it
(an authenticated Ingress, an OAuth2 proxy, a NetworkPolicy restricting
who can reach it, ...).

See [`values.yaml`](charts/netsk8-navigator/values.yaml) for every option
(resources, ingress, node selectors, security contexts, ...).

## Contributing

Issues and pull requests are welcome — the catalog-driven design in
[ARCHITECTURE.md](ARCHITECTURE.md) makes adding a new resource type a small,
self-contained change. Before opening a PR, make sure `go build ./... && go
vet ./... && go test ./...` (backend) and `pnpm exec tsc -b && pnpm build &&
pnpm exec oxlint src` (frontend) all pass — the same checks run in CI.

Want to help translate the app instead? See [TRANSLATIONS.md](TRANSLATIONS.md) —
no Go or build tooling knowledge needed, just one file to edit.

## License

[MIT](LICENSE)
