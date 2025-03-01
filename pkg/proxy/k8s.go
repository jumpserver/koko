package proxy

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"strings"

	"github.com/jumpserver/koko/pkg/jms-sdk-go/model"
	"github.com/jumpserver/koko/pkg/logger"
)

const (
	ResourceNamespace = "namespace"
	ResourcePod       = "pod"
	ResourceContainer = "container"
)

type Container struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type Pod struct {
	Name       string      `json:"name"`
	Type       string      `json:"type"`
	Containers []Container `json:"containers"`
}

type Namespace struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Pods []Pod  `json:"pods"`
}

type KubernetesClient struct {
	configName string
	gateway    *domainGateway
}

func NewKubernetesClient(address, token string, gateway *model.Gateway) (*KubernetesClient, error) {
	kc := &KubernetesClient{}
	if err := kc.InitClient(address, token, gateway); err != nil {
		return nil, err
	}
	return kc, nil
}

func (kc *KubernetesClient) InitClient(address, token string, gateway *model.Gateway) error {
	var proxyAddr *net.TCPAddr
	if gateway != nil {
		dGateway, err := newK8sGateWayServer(address, gateway)
		if err != nil {
			return fmt.Errorf("start domain gateway failed: %v", err)
		}
		if err = dGateway.Start(); err != nil {
			return fmt.Errorf("start domain gateway failed: %v", err)
		}
		kc.gateway = dGateway
		proxyAddr = dGateway.GetListenAddr()
	}

	if proxyAddr != nil {
		originUrl, err := url.Parse(address)
		if err != nil {
			return err
		}
		address = ReplaceURLHostAndPort(originUrl, "127.0.0.1", proxyAddr.Port)
	}

	kubeConfig := kc.GetKubeConfig(address, token)

	tmpFile, err := os.CreateTemp("", "kubeconfig-*.yaml")
	if err != nil {
		return fmt.Errorf("error creating temp file: %w", err)
	}
	defer tmpFile.Close()

	if _, err := tmpFile.Write([]byte(kubeConfig)); err != nil {
		return fmt.Errorf("error writing to temp file: %w", err)
	}
	kc.configName = tmpFile.Name()
	return nil
}

func newK8sGateWayServer(address string, gateway *model.Gateway) (*domainGateway, error) {
	dstHost, dstPort, err := ParseUrlHostAndPort(address)
	if err != nil {
		return nil, err
	}
	dGateway := &domainGateway{
		dstIP:           dstHost,
		dstPort:         dstPort,
		selectedGateway: gateway,
	}
	return dGateway, nil
}

func (kc *KubernetesClient) GetKubeConfig(address, token string) string {
	return fmt.Sprintf(`
apiVersion: v1
kind: Config
clusters:
- cluster:
    server: %s
    insecure-skip-tls-verify: true
  name: remote-cluster
contexts:
- context:
    cluster: remote-cluster
    user: remote-user
  name: remote-context
current-context: remote-context
users:
- name: remote-user
  user:
    token: %s
`, address, token)
}

var (
	getNamespacesLine    = "kubectl get namespaces -o custom-columns=NAME:.metadata.name --no-headers"
	getPodContainersLine = "kubectl get pods --all-namespaces -o custom-columns=NAMESPACE:.metadata.namespace,POD:.metadata.name,CONTAINER:.spec.containers[*].name --no-headers"
)

func (kc *KubernetesClient) GetTreeData() (string, error) {
	env := append(os.Environ(), "KUBECONFIG="+kc.configName)

	namespacesCmd := exec.Command("bash", "-c", getNamespacesLine)
	namespacesCmd.Env = env
	namespacesOutput, err := namespacesCmd.CombinedOutput()
	if err != nil {
		logger.Debugf("Error executing kubectl get namespaces: %v", err)
		return "{}", err
	}

	namespaceLines := strings.Split(strings.TrimSpace(string(namespacesOutput)), "\n")

	namespaces := make(map[string]*Namespace)
	for _, nsName := range namespaceLines {
		if strings.HasPrefix(nsName, "error:") {
			nsName = strings.TrimPrefix(nsName, "error: ")
			return "{}", fmt.Errorf("%s", nsName)
		}

		if nsName = strings.TrimSpace(nsName); nsName != "" {
			namespaces[nsName] = &Namespace{Name: nsName, Type: "namespace"}
		}
	}

	podsCmd := exec.Command("bash", "-c", getPodContainersLine)
	podsCmd.Env = env
	podsOutput, err := podsCmd.CombinedOutput()
	if err != nil {
		logger.Debugf("Error executing kubectl get pods: %v", err)
		return "{}", err
	}

	podLines := strings.Split(strings.TrimSpace(string(podsOutput)), "\n")

	k8sTree := NewTree()
	for _, line := range podLines {
		record := strings.Fields(line)
		if len(record) < 3 {
			continue
		}

		nsName, podName, containerNames := record[0], record[1], record[2]
		for _, containerName := range strings.Split(containerNames, ",") {
			k8sTree.InsertResource(nsName, podName, containerName)
		}
	}

	for _, ns := range k8sTree.SearchNamespaces() {
		namespaces[ns.Name] = &ns
	}

	jsonData, err := json.Marshal(namespaces)
	if err != nil {
		logger.Errorf("Error marshalling JSON: %v", err)
		return "{}", err
	}

	return string(jsonData), nil
}

func (kc *KubernetesClient) Close() {
	var err error
	if removeErr := os.Remove(kc.configName); removeErr != nil {
		err = errors.Join(err, fmt.Errorf("failed to remove kubeconfig file: %w", removeErr))
	}
	if kc.gateway != nil {
		kc.gateway.Stop()
	}
	if err != nil {
		logger.Error(err)
	}
}

type K8sResourceTree struct {
	Root *K8sNode
}

type K8sNode struct {
	Name    string
	Type    string
	SubTree map[string]*K8sNode
}

func NewTree() *K8sResourceTree {
	return &K8sResourceTree{
		Root: NewNode(),
	}
}

func NewNode() *K8sNode {
	return &K8sNode{
		SubTree: make(map[string]*K8sNode),
	}
}

func (node *K8sNode) insert(resource, resourceType string) *K8sNode {
	if node.SubTree == nil {
		node.SubTree = make(map[string]*K8sNode)
	}
	newNode := NewNode()
	newNode.Type = resourceType
	newNode.Name = resource
	node.SubTree[resource] = newNode
	return newNode
}

func (node *K8sNode) searchContainers() []Container {
	containers := make([]Container, 0)
	for _, container := range node.SubTree {
		containers = append(containers, Container{
			Type: container.Type,
			Name: container.Name,
		})
	}
	return containers
}

func (node *K8sNode) searchPods() []Pod {
	pods := make([]Pod, 0)
	for _, pod := range node.SubTree {
		pods = append(pods, Pod{
			Type:       pod.Type,
			Name:       pod.Name,
			Containers: pod.searchContainers(),
		})
	}
	return pods
}

func (tree *K8sResourceTree) SearchNamespaces() []Namespace {
	namespaces := make([]Namespace, 0)
	for _, namespace := range tree.Root.SubTree {
		namespaces = append(namespaces, Namespace{
			Type: namespace.Type,
			Name: namespace.Name,
			Pods: namespace.searchPods(),
		})
	}
	return namespaces
}

func (tree *K8sResourceTree) InsertResource(ns, pod, container string) {
	cur := tree.Root
	cur = cur.insert(ns, ResourceNamespace)
	cur.insert(pod, ResourcePod)
	cur.insert(container, ResourceContainer)
}
