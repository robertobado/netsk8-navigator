package main

import "testing"

func TestBuildDeployment_ChaosLabel(t *testing.T) {
	dep := buildDeployment("production", "billing-worker", "redis:7", 1, true)
	if dep.Spec.Template.Labels[chaosLabel] != "true" {
		t.Errorf("chaos deployment's pod template missing %s=true label, got %v", chaosLabel, dep.Spec.Template.Labels)
	}
	if *dep.Spec.Replicas != 1 {
		t.Errorf("replicas = %d, want 1", *dep.Spec.Replicas)
	}
}

func TestBuildDeployment_NoChaosLabelByDefault(t *testing.T) {
	dep := buildDeployment("production", "web-frontend", "nginx:1.27", 3, false)
	if _, ok := dep.Spec.Template.Labels[chaosLabel]; ok {
		t.Errorf("non-chaos deployment should not carry %s label", chaosLabel)
	}
	if dep.Namespace != "production" || dep.Name != "web-frontend" {
		t.Errorf("unexpected metadata: ns=%s name=%s", dep.Namespace, dep.Name)
	}
}

func TestBuildService_MatchesSelector(t *testing.T) {
	svc := buildService("production", "web-frontend", 80)
	if svc.Spec.Selector["app"] != "web-frontend" {
		t.Errorf("service selector = %v, want app=web-frontend", svc.Spec.Selector)
	}
	if svc.Spec.Ports[0].Port != 80 {
		t.Errorf("service port = %d, want 80", svc.Spec.Ports[0].Port)
	}
}

func TestBuildJob_RestartPolicyNever(t *testing.T) {
	job := buildJob("production", "db-migrate", "busybox:1.36")
	if job.Spec.Template.Spec.RestartPolicy != "Never" {
		t.Errorf("job restart policy = %s, want Never", job.Spec.Template.Spec.RestartPolicy)
	}
}

func TestBuildPVC_RequestsExpectedSize(t *testing.T) {
	pvc := buildPVC("production", "postgres-data", "20Gi")
	got := pvc.Spec.Resources.Requests.Storage()
	if got.String() != "20Gi" {
		t.Errorf("pvc storage request = %s, want 20Gi", got.String())
	}
}
