package main

import (
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// chaosLabel marks a pod template for logs.go's breakPod: once the pod
// reaches Running, its status is patched (once) into a stable, realistic
// CrashLoopBackOff look. This is deliberately NOT kwok's own
// "pod-container-running-failed" chaos stage label — that stage sets the
// whole pod to a terminal Failed phase, which makes the Deployment's
// ReplicaSet spawn endless replacement pods (see breakPod's doc comment).
const chaosLabel = "netsk8-navigator.dev/demo-chaos"

// usageCPUAnnotation/usageMemoryAnnotation feed ../kwok/metrics.yaml's
// ClusterResourceUsage (a copy of kwok's usage-from-annotation.yaml): kwok's
// simulated kubelet reports each pod's /metrics/resource usage as whatever
// these annotations say (falling back to a flat 1m/1Mi when absent), which
// is what a real metrics-server scrapes to answer metrics.k8s.io.
const (
	usageCPUAnnotation    = "kwok.x-k8s.io/usage-cpu"
	usageMemoryAnnotation = "kwok.x-k8s.io/usage-memory"
)

func podTemplate(name, image string, chaos bool, cpu, memory string) corev1.PodTemplateSpec {
	labels := map[string]string{"app": name}
	if chaos {
		labels[chaosLabel] = "true"
	}
	annotations := map[string]string{}
	if cpu != "" {
		annotations[usageCPUAnnotation] = cpu
	}
	if memory != "" {
		annotations[usageMemoryAnnotation] = memory
	}
	return corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{Labels: labels, Annotations: annotations},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name:  name,
				Image: image,
				Ports: []corev1.ContainerPort{{ContainerPort: 8080}},
			}},
		},
	}
}

func buildDeployment(ns, name, image string, replicas int32, chaos bool, cpu, memory string) *appsv1.Deployment {
	tmpl := podTemplate(name, image, chaos, cpu, memory)
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, Labels: map[string]string{"app": name}},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32p(replicas),
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": name}},
			Template: tmpl,
		},
	}
}

func buildStatefulSet(ns, name, image string, replicas int32, cpu, memory string) *appsv1.StatefulSet {
	tmpl := podTemplate(name, image, false, cpu, memory)
	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, Labels: map[string]string{"app": name}},
		Spec: appsv1.StatefulSetSpec{
			ServiceName: name,
			Replicas:    int32p(replicas),
			Selector:    &metav1.LabelSelector{MatchLabels: map[string]string{"app": name}},
			Template:    tmpl,
		},
	}
}

func buildDaemonSet(ns, name, image string, cpu, memory string) *appsv1.DaemonSet {
	tmpl := podTemplate(name, image, false, cpu, memory)
	return &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, Labels: map[string]string{"app": name}},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": name}},
			Template: tmpl,
		},
	}
}

func buildJob(ns, name, image string, cpu, memory string) *batchv1.Job {
	tmpl := podTemplate(name, image, false, cpu, memory)
	tmpl.Spec.RestartPolicy = corev1.RestartPolicyNever
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, Labels: map[string]string{"app": name}},
		Spec:       batchv1.JobSpec{Template: tmpl},
	}
}

func buildCronJob(ns, name, image, schedule string, cpu, memory string) *batchv1.CronJob {
	tmpl := podTemplate(name, image, false, cpu, memory)
	tmpl.Spec.RestartPolicy = corev1.RestartPolicyNever
	return &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, Labels: map[string]string{"app": name}},
		Spec: batchv1.CronJobSpec{
			Schedule: schedule,
			JobTemplate: batchv1.JobTemplateSpec{
				Spec: batchv1.JobSpec{Template: tmpl},
			},
		},
	}
}

func buildService(ns, name string, port int32) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, Labels: map[string]string{"app": name}},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": name},
			Ports:    []corev1.ServicePort{{Port: port, TargetPort: intstr.FromInt32(8080)}},
		},
	}
}

func buildConfigMap(ns, name string, data map[string]string) *corev1.ConfigMap {
	return &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns}, Data: data}
}

func buildSecret(ns, name string, data map[string]string) *corev1.Secret {
	strData := make(map[string]string, len(data))
	for k, v := range data {
		strData[k] = v
	}
	return &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns}, StringData: strData, Type: corev1.SecretTypeOpaque}
}

func buildPVC(ns, name, size string) *corev1.PersistentVolumeClaim {
	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceStorage: mustParseQuantity(size)},
			},
		},
	}
}
