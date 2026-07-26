// Command seed populates a kwok-backed cluster (see ../kwok and
// docs/DEMO_CLUSTER.md) with realistic-looking, fully synthetic workloads —
// so netsk8-navigator has something to show without touching a real cluster.
//
// It only creates high-level objects (Deployments, StatefulSets, ...); the
// real kube-controller-manager and scheduler that kwokctl runs expand those
// into ReplicaSets and Pods on their own, and kwok's Stage resources
// (../kwok/stages.yaml) drive the Pods to Running (or, for a labeled few, to
// a simulated failure) exactly as a real kubelet would.
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"path/filepath"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

func main() {
	kubeconfig := flag.String("kubeconfig", os.Getenv("KUBECONFIG"), "path to the kwok cluster's kubeconfig")
	logDir := flag.String("log-dir", "./demo-logs", "directory to write each simulated pod's log file into")
	daemon := flag.Bool("daemon", true, "keep running after seeding, appending fake log lines to every pod's log file (set false for a one-shot seed, e.g. in CI)")
	flag.Parse()

	if *kubeconfig == "" {
		*kubeconfig = clientcmd.RecommendedHomeFile
	}
	restCfg, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		log.Fatalf("building rest config from %s: %v", *kubeconfig, err)
	}
	client, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		log.Fatalf("building clientset: %v", err)
	}
	dynClient, err := dynamicClientFor(restCfg)
	if err != nil {
		log.Fatalf("building dynamic client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	log.Print("seeding namespaces, nodes and RBAC...")
	if err := seedNamespaces(ctx, client); err != nil {
		log.Fatalf("seeding namespaces: %v", err)
	}
	if err := seedNodes(ctx, client); err != nil {
		log.Fatalf("seeding nodes: %v", err)
	}
	if err := seedRBAC(ctx, client); err != nil {
		log.Fatalf("seeding RBAC: %v", err)
	}

	log.Print("seeding workloads...")
	if err := seedWorkloads(ctx, client); err != nil {
		log.Fatalf("seeding workloads: %v", err)
	}

	if err := os.MkdirAll(*logDir, 0o755); err != nil {
		log.Fatalf("creating log dir %s: %v", *logDir, err)
	}
	absLogDir, err := filepath.Abs(*logDir)
	if err != nil {
		log.Fatalf("resolving log dir: %v", err)
	}

	if !*daemon {
		log.Print("seed complete (--daemon=false, exiting)")
		return
	}

	log.Printf("seed complete — watching pods and simulating live logs under %s (Ctrl+C to stop)", absLogDir)
	runLogDaemon(context.Background(), client, dynClient, absLogDir)
}
