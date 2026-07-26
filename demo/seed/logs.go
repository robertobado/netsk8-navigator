package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

// criTimeFormat is the fixed-width RFC3339Nano layout the CRI log format
// (and kwok's Logs simulation, which parses it the same way a real
// container runtime's log file would be) expects: see
// k8s.io/cri-client/pkg/logs.RFC3339NanoFixed.
const criTimeFormat = "2006-01-02T15:04:05.000000000Z07:00"

// formatCRILine renders one line the way a real container runtime's log
// file does: "<timestamp> <stream> F <content>\n" ("F" = a full, i.e. not
// split-across-writes, line — the only kind we ever emit here).
func formatCRILine(t time.Time, content string) string {
	return fmt.Sprintf("%s stdout F %s\n", t.UTC().Format(criTimeFormat), content)
}

var logLineTemplates = []string{
	"handling request %s",
	"GET /healthz 200 OK",
	"connected to database",
	"cache miss for key demo:%s",
	"processed job in 42ms",
	"heartbeat ok",
	"refreshed config from configmap",
	"WARN retrying upstream call (attempt 2)",
}

// randomLogLine picks a plausible line for appName, occasionally
// interpolating a short id so consecutive lines aren't all identical.
func randomLogLine(appName string) string {
	tmpl := logLineTemplates[rand.Intn(len(logLineTemplates))]
	if !containsVerb(tmpl) {
		return fmt.Sprintf("[%s] %s", appName, tmpl)
	}
	return fmt.Sprintf("[%s] %s", appName, fmt.Sprintf(tmpl, randomID()))
}

func containsVerb(tmpl string) bool {
	for i := 0; i < len(tmpl)-1; i++ {
		if tmpl[i] == '%' && tmpl[i+1] == 's' {
			return true
		}
	}
	return false
}

func randomID() string {
	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 6)
	for i := range b {
		b[i] = alphabet[rand.Intn(len(alphabet))]
	}
	return string(b)
}

// runLogDaemon watches every Pod cluster-wide and, for each one not already
// tracked, starts simulating a live log stream for it: an initial file with
// a few startup lines, a Logs CR pointing kwok at that file (so
// `kubectl logs`/our backend's follow=true GetLogs works), and a goroutine
// that keeps appending plausible lines until the pod is deleted or ctx ends.
func runLogDaemon(ctx context.Context, client kubernetes.Interface, dynClient dynamic.Interface, logDir string) {
	tracked := map[string]context.CancelFunc{}
	var mu sync.Mutex

	for {
		w, err := client.CoreV1().Pods("").Watch(ctx, metav1.ListOptions{})
		if err != nil {
			log.Printf("watch pods: %v (retrying in 5s)", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
				continue
			}
		}
		consumeWatch(ctx, w, dynClient, logDir, tracked, &mu)
		if ctx.Err() != nil {
			return
		}
	}
}

func consumeWatch(ctx context.Context, w watch.Interface, dynClient dynamic.Interface, logDir string, tracked map[string]context.CancelFunc, mu *sync.Mutex) {
	defer w.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-w.ResultChan():
			if !ok {
				return // channel closed (watch expired) — caller re-establishes it
			}
			pod, isPod := ev.Object.(*corev1.Pod)
			if !isPod {
				continue
			}
			key := pod.Namespace + "/" + pod.Name

			mu.Lock()
			switch ev.Type {
			case watch.Added:
				if _, exists := tracked[key]; !exists {
					podCtx, cancel := context.WithCancel(ctx)
					tracked[key] = cancel
					mu.Unlock()
					startSimulatedLog(podCtx, dynClient, logDir, pod)
					continue
				}
			case watch.Deleted:
				if cancel, exists := tracked[key]; exists {
					cancel()
					delete(tracked, key)
				}
			}
			mu.Unlock()
		}
	}
}

// startSimulatedLog writes the log file, registers the Logs CR, then spawns
// the appender goroutine. Best-effort: a failure here just means that one
// pod won't have logs — it must never take down the whole daemon.
func startSimulatedLog(ctx context.Context, dynClient dynamic.Interface, logDir string, pod *corev1.Pod) {
	appName := pod.Labels["app"]
	if appName == "" {
		appName = pod.Name
	}
	path := filepath.Join(logDir, pod.Namespace+"_"+pod.Name+".log")

	f, err := os.Create(path)
	if err != nil {
		log.Printf("creating log file for %s/%s: %v", pod.Namespace, pod.Name, err)
		return
	}
	now := time.Now()
	fmt.Fprint(f, formatCRILine(now, fmt.Sprintf("[%s] starting up", appName)))
	fmt.Fprint(f, formatCRILine(now.Add(time.Millisecond), fmt.Sprintf("[%s] listening on :8080", appName)))
	f.Close()

	if err := applyLogsCR(ctx, dynClient, pod.Namespace, pod.Name, path); err != nil {
		log.Printf("applying Logs CR for %s/%s: %v", pod.Namespace, pod.Name, err)
		return
	}

	go appendLoop(ctx, path, appName)
}

func applyLogsCR(ctx context.Context, dynClient dynamic.Interface, namespace, name, logsFile string) error {
	obj := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "kwok.x-k8s.io/v1alpha1",
		"kind":       "Logs",
		"metadata": map[string]any{
			"name":      name,
			"namespace": namespace,
		},
		"spec": map[string]any{
			"logs": []any{
				map[string]any{"logsFile": logsFile},
			},
		},
	}}
	_, err := dynClient.Resource(logsGVR).Namespace(namespace).Create(ctx, obj, metav1.CreateOptions{})
	if err != nil && !isAlreadyExists(err) {
		return err
	}
	return nil
}

// appendLoop keeps a pod's log file "live" for as long as it exists.
func appendLoop(ctx context.Context, path, appName string) {
	for {
		wait := time.Duration(3+rand.Intn(5)) * time.Second
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
			f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
			if err != nil {
				return // pod's log file is gone — nothing left to append to
			}
			fmt.Fprint(f, formatCRILine(time.Now(), randomLogLine(appName)))
			f.Close()
		}
	}
}
