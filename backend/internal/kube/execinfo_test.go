package kube

import (
	"testing"

	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

func TestExecInfoFor(t *testing.T) {
	m := &Manager{rawConfig: clientcmdapi.Config{
		Contexts: map[string]*clientcmdapi.Context{
			"via-env":  {AuthInfo: "aws-env"},
			"via-args": {AuthInfo: "aws-args"},
			"no-exec":  {AuthInfo: "static"},
		},
		AuthInfos: map[string]*clientcmdapi.AuthInfo{
			"aws-env": {Exec: &clientcmdapi.ExecConfig{
				Command: "/usr/local/bin/aws",
				Args:    []string{"eks", "get-token", "--cluster-name", "stage"},
				Env:     []clientcmdapi.ExecEnvVar{{Name: "AWS_PROFILE", Value: "studio-stage"}},
			}},
			"aws-args": {Exec: &clientcmdapi.ExecConfig{
				Command: "/usr/local/bin/aws",
				Args:    []string{"eks", "get-token", "--profile", "studio-prod", "--cluster-name", "prod"},
			}},
			"static": {Token: "static-token"},
		},
	}}

	if cmd, profile, ok := m.ExecInfoFor("via-env"); !ok || cmd != "aws" || profile != "studio-stage" {
		t.Errorf("via-env: got (%q, %q, %v), want (aws, studio-stage, true)", cmd, profile, ok)
	}
	if cmd, profile, ok := m.ExecInfoFor("via-args"); !ok || cmd != "aws" || profile != "studio-prod" {
		t.Errorf("via-args: got (%q, %q, %v), want (aws, studio-prod, true)", cmd, profile, ok)
	}
	if _, _, ok := m.ExecInfoFor("no-exec"); ok {
		t.Error("no-exec: expected ok=false for an AuthInfo with no Exec block")
	}
	if _, _, ok := m.ExecInfoFor("unknown-context"); ok {
		t.Error("unknown-context: expected ok=false")
	}
}
