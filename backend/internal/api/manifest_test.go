package api

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestCleanUnstructured(t *testing.T) {
	obj := &unstructured.Unstructured{Object: map[string]any{
		"metadata": map[string]any{
			"name":          "web",
			"managedFields": []any{map[string]any{"manager": "kubectl"}},
			"annotations": map[string]any{
				"kubectl.kubernetes.io/last-applied-configuration": "{...}",
				"custom.io/keep": "yes",
			},
		},
	}}
	cleanUnstructured(obj)

	if _, found, _ := unstructured.NestedFieldNoCopy(obj.Object, "metadata", "managedFields"); found {
		t.Error("managedFields should be removed")
	}
	ann, _, _ := unstructured.NestedStringMap(obj.Object, "metadata", "annotations")
	if _, has := ann["kubectl.kubernetes.io/last-applied-configuration"]; has {
		t.Error("last-applied-configuration annotation should be removed")
	}
	if ann["custom.io/keep"] != "yes" {
		t.Error("unrelated annotations should survive")
	}
}

func TestCleanUnstructured_RemovesEmptyAnnotationsMap(t *testing.T) {
	obj := &unstructured.Unstructured{Object: map[string]any{
		"metadata": map[string]any{
			"annotations": map[string]any{
				"kubectl.kubernetes.io/last-applied-configuration": "{...}",
			},
		},
	}}
	cleanUnstructured(obj)
	if _, found, _ := unstructured.NestedFieldNoCopy(obj.Object, "metadata", "annotations"); found {
		t.Error("annotations map should be removed entirely once emptied")
	}
}

func TestResolveSlug_UnknownSlug(t *testing.T) {
	s := newTestServer(t)
	if _, err := s.resolveSlug("test", "not-a-real-kind"); err == nil {
		t.Error("expected an error for an unknown manifest slug")
	}
}
