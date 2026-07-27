# Cluster de demonstração (kwok)

Um cluster Kubernetes inteiramente sintético — nodes, pods, deployments,
RBAC, logs "ao vivo" — para rodar o Netsk8 Navigator sem precisar de um
cluster real. Serve tanto pra demo/site do projeto quanto pra testar o
fluxo completo (backend + frontend) localmente ou em CI.

Motor: [kwok](https://kwok.sigs.k8s.io/) (`kwokctl`) — sobe um
`kube-apiserver`+`etcd` reais mais um controller que simula o
comportamento do kubelet (ciclo de vida de pod/node, heartbeats). Como fala
o protocolo real da API do Kubernetes, o backend do Netsk8 Navigator não
precisa de nenhuma mudança: só aponta `KUBECONFIG` pro kubeconfig do
cluster kwok, exatamente como apontaria pra um cluster de verdade.

## 1. Instalar o kwokctl

```bash
go install sigs.k8s.io/kwok/cmd/kwokctl@v0.8.0
```

(Ou baixe o binário pré-compilado em
[github.com/kubernetes-sigs/kwok/releases](https://github.com/kubernetes-sigs/kwok/releases).)

## 2. Subir o cluster

```bash
kwokctl create cluster --name netsk8-demo --runtime binary \
  --config demo/kwok/stages.yaml \
  --config demo/kwok/metrics.yaml \
  --enable-crds=Logs --enable-crds=ClusterLogs \
  --enable-crds=Metric --enable-crds=ClusterResourceUsage \
  --enable metrics-server
```

- `--config demo/kwok/stages.yaml`: **substitui** por completo os `Stage`
  padrão do kwok (não soma) — por isso esse arquivo carrega o conjunto
  básico completo (ciclo de vida de pod/node) mais o estágio `node-not-ready`
  usado pelo `demo/seed` (ver `demo/kwok/stages.yaml` para os detalhes e a
  proveniência de cada trecho; o caos de pod é aplicado pelo próprio
  `demo/seed`, não por um `Stage` — ver a seção de dados falsos abaixo).
- `--enable-crds=Logs,ClusterLogs`: sem isso, `kubectl logs`/nosso
  `GetLogs` respondem sempre "no logs found" — são CRDs do próprio kwok
  (`kwok.x-k8s.io/v1alpha1`), desligadas por padrão.
- `--config demo/kwok/metrics.yaml` e `--enable-crds=Metric,ClusterResourceUsage`
  e `--enable metrics-server`: sobe um metrics-server real (v0.8.1) e ensina
  o `kwok-controller` a servir `/metrics/nodes/{node}/metrics/resource` com
  números derivados das anotações `kwok.x-k8s.io/usage-cpu`/`usage-memory`
  que `demo/seed` põe em cada pod. **Armadilha**: `--config` só aplica
  recursos `Stage` na criação do cluster — os CRs `Metric`/
  `ClusterResourceUsage` do `metrics.yaml` **não** são criados
  automaticamente, apesar de aparecerem no log de criação do `kwokctl`
  (esse log só confirma que os *tipos* de CRD foram habilitados). É preciso
  aplicá-los à parte, uma vez por cluster:

  ```bash
  kubectl apply -f demo/kwok/metrics.yaml
  ```

  Sem esse passo, `kubectl top nodes`/`nodeusage`/`podusage` ficam
  silenciosamente zerados (o metrics-server sobe, escuta, mas nunca tem o
  que reportar).

Depois, crie alguns nodes (o kwok não vem com nenhum por padrão):

```bash
kwokctl scale node --name netsk8-demo --replicas 2
```

(O `demo/seed` abaixo também cria os próprios nodes nomeados — este passo
extra é só se você quiser nodes adicionais sem nome/label específicos.)

## 3. Popular com dados falsos

```bash
kwokctl get kubeconfig --name netsk8-demo > /tmp/netsk8-demo-kubeconfig.yaml
cd demo/seed
go run . --kubeconfig=/tmp/netsk8-demo-kubeconfig.yaml
```

Isso cria (via `k8s.io/client-go`, contra a API real do cluster kwok):

- 4 nodes (`node-a1`, `node-a2`, `node-b1`, `node-b2` — o último com o
  label de caos `node-not-ready.stage.kwok.x-k8s.io`, então fica
  `NotReady`).
- Namespaces `production`/`staging`/`monitoring`.
- Deployments/StatefulSets/DaemonSets/Job/CronJob com nomes e imagens
  realistas (`web-frontend`, `api-gateway`, `postgres-primary`,
  `prometheus`, `grafana`, `node-exporter`, ...) — só os objetos de alto
  nível; o `kube-controller-manager`/scheduler reais que o `kwokctl` já
  roda expandem isso em ReplicaSets e Pods sozinhos, e os `Stage` do kwok
  levam os Pods a `Running` como um kubelet de verdade levaria.
- Duas Deployments propositalmente quebradas (`billing-worker` em
  `production`, `flaky-service` em `staging`, marcadas com o label
  `netsk8-navigator.dev/demo-chaos`) — o próprio `demo/seed` (não um
  `Stage` do kwok) as detecta assim que ficam `Running` e faz um patch
  direto no status de um dos containers pra `CrashLoopBackOff`, mantendo o
  pod em `Running` (ver `breakPod` em `demo/seed/logs.go`). Alimentam o
  carrossel de Issues com pods de verdade travados em crash loop, sem o
  ReplicaSet ficar recriando pods indefinidamente (o que acontecia com a
  abordagem antiga, baseada no `Stage` `pod-container-running-failed` do
  próprio kwok, que derrubava a fase inteira do pod pra `Failed`).
- ServiceAccounts/Role/RoleBinding/ClusterRole/ClusterRoleBinding, pra
  "Effective permissions" ter conteúdo de verdade.
- Services/ConfigMap/Secret/PVC+PV correspondentes.

Por padrão (`--daemon=true`) o processo **continua rodando**: ele observa
todo Pod do cluster e, pra cada um, escreve um arquivo de log em formato
CRI (o mesmo formato que um runtime de containers de verdade grava) e cria
um recurso `Logs` do kwok apontando pra ele — e então fica anexando uma
linha nova a cada poucos segundos, pra que a aba de Logs (single-pod e
multi-pod) do Navigator realmente pareça "ao vivo". Pra rodar só o seed
uma vez sem o gerador de logs (ex. num smoke test de CI), use
`--daemon=false`.

## 4. Rodar o Navigator apontado pro cluster kwok

```bash
cd backend
KUBECONFIG=/tmp/netsk8-demo-kubeconfig.yaml DEMO_MODE=true go run .
```

`DEMO_MODE=true` (opcional, mas recomendado pra demo pública): desativa
Terminal e Port-forward — o kwok não roda kubelet/containers de verdade,
então essas duas features não têm o que atender — e ativa o balãozinho
flutuante com link pro repositório na UI. Sem essa env var o backend
funciona normalmente contra o cluster kwok, só que exec/port-forward
falhariam com o erro cru da API em vez de ficarem escondidos.

Depois, `pnpm dev` no `frontend/` (ou use o binário único com a SPA
embutida) e navegue normalmente — Overview, Workloads, drawer de Pod →
Logs, Nodes, RBAC (Effective permissions), Issues (carrossel deve mostrar
os pods/node propositalmente quebrados), Topologia.

## O que não funciona (por design)

- **Exec/Terminal e Port-forward**: sem kubelet real, não há shell nem
  socket de verdade pra anexar. `DEMO_MODE=true` esconde essas abas; sem a
  flag, as chamadas falham com o erro nativo da API do Kubernetes.
- **Métricas (CPU/memória)**: `metrics.k8s.io` e a descoberta de
  Prometheus (`backend/internal/api/usage.go`/`monitoring.go`) já são
  "best-effort" — sem nenhum dos dois instalados, a UI mostra
  "unavailable" graciosamente. Não incluímos um metrics-server/Prometheus
  fake nesta entrega.

## Bônus opcional: releases Helm reais

Helm só precisa de Secrets + discovery/RESTMapper — funciona sem nada
especial contra o cluster kwok. Pra ter releases de verdade na view Helm:

```bash
helm install demo-nginx oci://registry-1.docker.io/bitnamicharts/nginx \
  --kubeconfig /tmp/netsk8-demo-kubeconfig.yaml --namespace production
```

Não está automatizado no `demo/seed` (evitar depender de rede/registries
externos num seed/smoke test de CI); é um passo manual pra quem quiser mais
realismo na demo local.
