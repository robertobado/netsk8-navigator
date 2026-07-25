<p align="center">
  <img src="Icone Netsk8s.png" width="96" alt="Netsk8 Navigator" />
</p>

<h1 align="center">Netsk8 Navigator</h1>

<p align="center">
  Um navegador de clusters Kubernetes pelo browser — pense em Lens ou k9s, só que web.<br/>
  O nome é uma homenagem ao velho Netscape Navigator.
</p>

---

## O que é

Netsk8 Navigator é uma SPA que lê seu `kubeconfig` e navega qualquer cluster
Kubernetes (recursos padrão + CRDs) através de um backend Go fino que fala
diretamente com a API do cluster. Sem instalar nada no cluster, sem agente,
sem estado além das suas preferências locais.

- Tabelas com todos os recursos padrão (Workloads, Rede, Config, Storage,
  RBAC, Governança, Cluster) — filtráveis, com expansão de linha para
  relações (Node → workloads, Namespace → recursos, ConfigMap/Secret →
  consumidores, …).
- Browser genérico de CRDs de rota (Gateway API, Traefik IngressRoute, Istio
  VirtualService, Contour).
- Detalhe + manifesto YAML (editor Monaco) para leitura e edição in-place.
- Logs, exec e eventos de pods em tempo real (SSE/WebSocket).
- Métricas de CPU/memória (cluster, node, pod) quando o cluster expõe
  `metrics-server` ou Prometheus.
- Multi-cluster: troca de contexto do kubeconfig sem reiniciar nada.
- Multi-versão: cada recurso é resolvido via discovery/RESTMapper no
  momento da requisição, então funciona em qualquer versão do Kubernetes
  que o cluster sirva.

## Arquitetura

```text
backend/    Go — client-go dynamic client + discovery RESTMapper, API REST fina
frontend/   React + Vite + TypeScript — Tailwind v4, TanStack Table/Query, Monaco
```

O design é **catalog-driven**: adicionar um recurso padrão é uma entrada no
catálogo do backend (`backend/internal/api/catalog.go`) + uma entrada no
catálogo do frontend (`frontend/src/lib/resources.tsx`) — nenhum handler ou
view novos. Detalhes completos do padrão de extensão em
[ARCHITECTURE.md](ARCHITECTURE.md).

## Rodando localmente

Pré-requisitos: Go 1.26+, Node 20+, [pnpm](https://pnpm.io).

```bash
# Backend — API em http://127.0.0.1:8080 (lê ~/.kube/config, ou $KUBECONFIG)
cd backend
go run .

# Frontend — dev server em http://localhost:5173, com proxy /api → :8080
cd frontend
pnpm install
pnpm dev
```

Abra `http://localhost:5173`.

### Comandos úteis

```bash
# Backend
cd backend && go build ./... && go vet ./... && go test ./...

# Frontend
cd frontend && pnpm exec tsc -b && pnpm build   # typecheck + build
cd frontend && pnpm exec oxlint src              # lint (linter oficial do projeto)
```

## Binário único (sem Vite, sem processo separado)

Buildar o frontend antes do backend embute a SPA no binário Go
(`internal/web`, via `go:embed`) — um processo só, uma porta só, API e UI
juntas:

```bash
cd frontend && pnpm install && pnpm build   # gera backend/internal/web/dist
cd ../backend && go build -o netsk8-navigator .
ADDR=127.0.0.1:8080 ./netsk8-navigator      # abra http://127.0.0.1:8080
```

Sem o passo do `pnpm build`, `go build` continua funcionando normalmente —
só não há UI embutida (o log diz "no embedded frontend build"); é o
caminho normal quando você só quer a API, ou está iterando no backend em
dev.

## Docker

```bash
docker build -t netsk8-navigator .
docker run --rm -p 127.0.0.1:8080:8080 \
  -v "$HOME/.kube:/kube:ro" -e KUBECONFIG=/kube/config \
  netsk8-navigator
```

Mapeie a porta só para `127.0.0.1` do host (como acima) para manter o
mesmo modelo de segurança — sem isso, `-p 8080:8080` expõe o backend
sem autenticação para qualquer coisa na sua rede.

Se `~/.kube/config` for um **symlink para fora de `~/.kube`** (comum com
ferramentas que trocam de contexto trocando o link, ex. ambientes com
vários clusters), o mount acima não resolve o alvo — o container só
enxerga o próprio `~/.kube`. Monte o arquivo real:

```bash
docker run --rm -p 127.0.0.1:8080:8080 \
  -v "$(readlink -f ~/.kube/config):/kube/config:ro" -e KUBECONFIG=/kube/config \
  netsk8-navigator
```

## Modelo de segurança

Este backend **não tem autenticação, não tem TLS, e usa CORS `*`**. Ele
também pode mutar o cluster (aplicar manifests via o editor Monaco), abrir
um `exec` em qualquer pod, e retornar valores decodificados de `Secret`s.
Ele foi pensado para uso **local, na sua máquina, com o seu próprio
kubeconfig** — o mesmo nível de confiança de rodar `kubectl` diretamente.

- Por padrão o backend escuta apenas em `127.0.0.1:8080` (loopback). Só
  mude `ADDR` para expor em outra interface se você entender as
  implicações — isso equivale a dar a qualquer processo/máquina que
  alcance a porta o mesmo acesso que suas credenciais de kubeconfig têm.
- Não rode isso como serviço compartilhado ou atrás de um proxy exposto à
  internet sem colocar autenticação/autorização na frente.

## Licença

[MIT](LICENSE)
