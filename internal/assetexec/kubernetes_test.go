package assetexec

import (
	"context"
	"strings"
	"testing"
)

func TestKubernetesCommandValidationAndCanonicalization(t *testing.T) {
	for _, command := range []string{
		"kubectl get pods -n app -o json",
		`kubectl "g"et pods --namespace=app`,
		"kubectl get pods -o 'jsonpath={.items[*].metadata.name}'",
		"kubectl describe deployment/app --show-events=false",
		"kubectl logs pod -c app --previous --tail=20 --since=1h",
		"kubectl logs -l app=web --all-containers",
	} {
		canonical, err := PrepareKubernetesCommand(command)
		if err != nil {
			t.Fatalf("valid Kubernetes command rejected: %s: %v", command, err)
		}
		if again, err := PrepareKubernetesCommand(canonical); err != nil || again != canonical {
			t.Fatalf("approved argv is not stable: %s: %v", canonical, err)
		}
		if strings.Contains(command, `"g"et`) && !strings.HasPrefix(canonical, "kubectl get ") {
			t.Fatal("Core would review an obscured command verb")
		}
		if strings.HasPrefix(command, "kubectl logs") && !strings.Contains(canonical, "--limit-bytes=262144") {
			t.Fatal("logs were not bounded before approval")
		}
	}
	for _, command := range []string{
		"kubectl apply -f /tmp/manifest", "kubectl delete pod app", "kubectl exec pod -- id",
		"kubectl config view --raw", "kubectl plugin list", "kubectl proxy", "kubectl port-forward pod 80",
		"kubectl get pods --server=https://other", "kubectl get pods -s https://other",
		"kubectl get pods --token=other", "kubectl get pods --as=admin", "kubectl get pods --as-group=admin",
		"kubectl get pods --context=other", "kubectl get pods --user=other", "kubectl get pods --cluster=other",
		"kubectl get pods --kubeconfig=/tmp/config", "kubectl get pods --kuberc=/tmp/config",
		"kubectl get pods --cache-dir=/tmp/cache", "kubectl get pods --v=9", "kubectl get pods --request-timeout=0",
		"kubectl get --raw=/api/v1/secrets", "kubectl get -f /tmp/file", "kubectl get -k /tmp/dir",
		"kubectl get pods -o go-template-file=/tmp/file", "kubectl get pods -o jsonpath-file=/tmp/file",
		"kubectl get pods -o custom-columns-file=/tmp/file", "kubectl get pods --watch",
		"kubectl get pods --chunk-size=0", "kubectl get pods -- -s https://other",
		"kubectl get pods; id", "kubectl get pods | cat", "kubectl get $(id)", "kubectl get pods > /tmp/file",
		"kubectl get pods -n ../other", "kubectl get pods -n app -A", "kubectl logs pod -f",
		"kubectl logs pod --tail=-1", "kubectl logs pod --limit-bytes=0", "kubectl logs pod --limit-bytes=1048577",
		"kubectl logs pod --since=-1s", "kubectl logs pod --since-time=invalid", "kubectl logs -l ''",
	} {
		if _, err := PrepareKubernetesCommand(command); err == nil {
			t.Fatalf("unsafe Kubernetes command accepted: %s", command)
		}
	}
}

func TestKubernetesRejectsUnsupportedRequestsBeforeDispatch(t *testing.T) {
	conn := &Connection{protocol: "k8s"}
	for _, request := range []Request{
		{Command: "kubectl get pods", Cwd: "/tmp"},
		{Command: "kubectl get pods", Env: map[string]string{"KUBECONFIG": "/tmp/config"}},
		{Command: "kubectl get pods --token=other"},
		{Command: "kubectl exec pod -- id"},
	} {
		result, err := conn.Execute(context.Background(), request, func(string, []byte) error { return nil })
		if err == nil || result.Dispatched {
			t.Fatalf("unsupported request reached execution: %+v %v", result, err)
		}
	}
}
