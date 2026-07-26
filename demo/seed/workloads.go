package main

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type demoDeployment struct {
	namespace, name, image string
	replicas               int32
	chaos                  bool
	servicePort            int32 // 0 = no matching Service
	cpu, memory            string
}

var demoDeployments = []demoDeployment{
	{namespace: "production", name: "web-frontend", image: "nginx:1.27", replicas: 3, servicePort: 80, cpu: "50m", memory: "64Mi"},
	{namespace: "production", name: "api-gateway", image: "nginx:1.27", replicas: 2, servicePort: 8080, cpu: "100m", memory: "128Mi"},
	{namespace: "production", name: "billing-worker", image: "redis:7", replicas: 1, chaos: true, cpu: "80m", memory: "96Mi"},
	{namespace: "staging", name: "web-frontend", image: "nginx:1.27", replicas: 1, servicePort: 80, cpu: "40m", memory: "64Mi"},
	{namespace: "staging", name: "flaky-service", image: "redis:7", replicas: 1, chaos: true, cpu: "60m", memory: "80Mi"},
	{namespace: "monitoring", name: "grafana", image: "grafana/grafana:11.0.0", replicas: 1, servicePort: 3000, cpu: "120m", memory: "180Mi"},
}

type demoStatefulSet struct {
	namespace, name, image string
	replicas               int32
	servicePort            int32
	cpu, memory            string
}

var demoStatefulSets = []demoStatefulSet{
	{namespace: "production", name: "postgres-primary", image: "postgres:16", replicas: 1, servicePort: 5432, cpu: "250m", memory: "512Mi"},
	// No Service (servicePort: 0): a real one would fool the backend's
	// Prometheus auto-discovery (backend/internal/api/monitoring.go's
	// matchSource matches by name, not by actually querying it), which
	// picks the richer time-series UI path over the working metrics-server
	// gauges and then has nothing to show since this "prometheus" is just
	// a seeded placeholder pod, not a real one.
	{namespace: "monitoring", name: "prometheus", image: "prom/prometheus:v3.0.0", replicas: 1, cpu: "300m", memory: "768Mi"},
}

type demoDaemonSet struct {
	namespace, name, image string
	cpu, memory            string
}

var demoDaemonSets = []demoDaemonSet{
	{namespace: "production", name: "log-agent", image: "busybox:1.36", cpu: "20m", memory: "32Mi"},
	{namespace: "monitoring", name: "node-exporter", image: "prom/node-exporter:v1.8.0", cpu: "15m", memory: "24Mi"},
}

// seedWorkloads creates every Deployment/StatefulSet/DaemonSet/Job/CronJob
// (plus their matching Service/ConfigMap/Secret/PVC) listed above. It only
// creates high-level objects — the real controller-manager and scheduler
// kwokctl runs expand them into Pods (see package doc in main.go).
func seedWorkloads(ctx context.Context, client kubernetes.Interface) error {
	for _, d := range demoDeployments {
		dep := buildDeployment(d.namespace, d.name, d.image, d.replicas, d.chaos, d.cpu, d.memory)
		if _, err := client.AppsV1().Deployments(d.namespace).Create(ctx, dep, metav1.CreateOptions{}); err != nil && !isAlreadyExists(err) {
			return fmt.Errorf("creating deployment %s/%s: %w", d.namespace, d.name, err)
		}
		if d.servicePort != 0 {
			if err := createService(ctx, client, d.namespace, d.name, d.servicePort); err != nil {
				return err
			}
		}
	}

	for _, s := range demoStatefulSets {
		set := buildStatefulSet(s.namespace, s.name, s.image, s.replicas, s.cpu, s.memory)
		if _, err := client.AppsV1().StatefulSets(s.namespace).Create(ctx, set, metav1.CreateOptions{}); err != nil && !isAlreadyExists(err) {
			return fmt.Errorf("creating statefulset %s/%s: %w", s.namespace, s.name, err)
		}
		if s.servicePort != 0 {
			if err := createService(ctx, client, s.namespace, s.name, s.servicePort); err != nil {
				return err
			}
		}
	}

	for _, ds := range demoDaemonSets {
		set := buildDaemonSet(ds.namespace, ds.name, ds.image, ds.cpu, ds.memory)
		if _, err := client.AppsV1().DaemonSets(ds.namespace).Create(ctx, set, metav1.CreateOptions{}); err != nil && !isAlreadyExists(err) {
			return fmt.Errorf("creating daemonset %s/%s: %w", ds.namespace, ds.name, err)
		}
	}

	job := buildJob("production", "db-migrate", "busybox:1.36", "100m", "64Mi")
	if _, err := client.BatchV1().Jobs("production").Create(ctx, job, metav1.CreateOptions{}); err != nil && !isAlreadyExists(err) {
		return fmt.Errorf("creating job db-migrate: %w", err)
	}
	cron := buildCronJob("production", "nightly-backup", "busybox:1.36", "0 2 * * *", "50m", "64Mi")
	if _, err := client.BatchV1().CronJobs("production").Create(ctx, cron, metav1.CreateOptions{}); err != nil && !isAlreadyExists(err) {
		return fmt.Errorf("creating cronjob nightly-backup: %w", err)
	}

	cm := buildConfigMap("production", "app-config", map[string]string{"LOG_LEVEL": "info", "FEATURE_FLAGS": "checkout-v2=on"})
	if _, err := client.CoreV1().ConfigMaps("production").Create(ctx, cm, metav1.CreateOptions{}); err != nil && !isAlreadyExists(err) {
		return fmt.Errorf("creating configmap app-config: %w", err)
	}
	secret := buildSecret("production", "app-secrets", map[string]string{"DATABASE_URL": "postgres://demo:demo@postgres-primary:5432/app"})
	if _, err := client.CoreV1().Secrets("production").Create(ctx, secret, metav1.CreateOptions{}); err != nil && !isAlreadyExists(err) {
		return fmt.Errorf("creating secret app-secrets: %w", err)
	}

	pvc := buildPVC("production", "postgres-data", "20Gi")
	if _, err := client.CoreV1().PersistentVolumeClaims("production").Create(ctx, pvc, metav1.CreateOptions{}); err != nil && !isAlreadyExists(err) {
		return fmt.Errorf("creating pvc postgres-data: %w", err)
	}
	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{Name: "postgres-data-pv"},
		Spec: corev1.PersistentVolumeSpec{
			Capacity:                      corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("20Gi")},
			AccessModes:                   []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			PersistentVolumeReclaimPolicy: corev1.PersistentVolumeReclaimRetain,
			PersistentVolumeSource:        corev1.PersistentVolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/data/postgres-data-pv"}},
			ClaimRef:                      &corev1.ObjectReference{Namespace: "production", Name: "postgres-data"},
		},
	}
	if _, err := client.CoreV1().PersistentVolumes().Create(ctx, pv, metav1.CreateOptions{}); err != nil && !isAlreadyExists(err) {
		return fmt.Errorf("creating pv postgres-data-pv: %w", err)
	}
	return nil
}

func createService(ctx context.Context, client kubernetes.Interface, ns, name string, port int32) error {
	svc := buildService(ns, name, port)
	if _, err := client.CoreV1().Services(ns).Create(ctx, svc, metav1.CreateOptions{}); err != nil && !isAlreadyExists(err) {
		return fmt.Errorf("creating service %s/%s: %w", ns, name, err)
	}
	return nil
}
