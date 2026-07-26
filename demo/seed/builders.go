package main

import (
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// chaosLabel marks a pod template so ../kwok/stages.yaml's
// pod-container-running-failed stage fails it shortly after it starts.
const chaosLabel = "pod-container-running-failed.stage.kwok.x-k8s.io"

func podTemplate(name, image string, chaos bool) corev1.PodTemplateSpec {
	labels := map[string]string{"app": name}
	if chaos {
		labels[chaosLabel] = "true"
	}
	return corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{Labels: labels},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name:  name,
				Image: image,
				Ports: []corev1.ContainerPort{{ContainerPort: 8080}},
			}},
		},
	}
}

func buildDeployment(ns, name, image string, replicas int32, chaos bool) *appsv1.Deployment {
	tmpl := podTemplate(name, image, chaos)
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, Labels: map[string]string{"app": name}},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32p(replicas),
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": name}},
			Template: tmpl,
		},
	}
}

func buildStatefulSet(ns, name, image string, replicas int32) *appsv1.StatefulSet {
	tmpl := podTemplate(name, image, false)
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

func buildDaemonSet(ns, name, image string) *appsv1.DaemonSet {
	tmpl := podTemplate(name, image, false)
	return &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, Labels: map[string]string{"app": name}},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": name}},
			Template: tmpl,
		},
	}
}

func buildJob(ns, name, image string) *batchv1.Job {
	tmpl := podTemplate(name, image, false)
	tmpl.Spec.RestartPolicy = corev1.RestartPolicyNever
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, Labels: map[string]string{"app": name}},
		Spec:       batchv1.JobSpec{Template: tmpl},
	}
}

func buildCronJob(ns, name, image, schedule string) *batchv1.CronJob {
	tmpl := podTemplate(name, image, false)
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
