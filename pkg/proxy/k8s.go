package proxy

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"strings"

	"github.com/jumpserver/koko/pkg/srvconn"
	"k8s.io/client-go/rest"

	"github.com/jumpserver-dev/sdk-go/model"
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
	token      string
	gateway    *domainGateway
}

func NewKubernetesClient(address, namespace, token string, gateway *model.Gateway) (*KubernetesClient, error) {
	kc := &KubernetesClient{}
	if err := kc.InitClient(address, namespace, token, gateway); err != nil {
		return nil, err
	}
	return kc, nil
}

func (kc *KubernetesClient) InitClient(address, namespace, token string, gateway *model.Gateway) error {
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

	kubeConf := &rest.Config{
		Host:        address,
		BearerToken: token,
	}
	kubeConf.Insecure = true

	if !srvconn.IsValidK8sUserToken(kubeConf) {
		return srvconn.ErrValidToken
	}

	kubeConfigYAML := kc.GetKubeConfig(address, namespace, token)

	tmpFile, err := os.CreateTemp("", "kubeconfig-*.yaml")
	if err != nil {
		return fmt.Errorf("error creating temp file: %w", err)
	}
	defer tmpFile.Close()

	if _, err := tmpFile.Write([]byte(kubeConfigYAML)); err != nil {
		return fmt.Errorf("error writing to temp file: %w", err)
	}
	kc.configName = tmpFile.Name()
	kc.token = token
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

func (kc *KubernetesClient) GetKubeConfig(address, namespace, token string) string {
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
    namespace: %s
  name: remote-context
current-context: remote-context
users:
- name: remote-user
  user:
    token: %s
`, address, namespace, token)
}

var (
	getNamespacesLineAll     = "kubectl get namespaces -o custom-columns=NAME:.metadata.name --no-headers"
	getPodsAllNamespacesLine = "kubectl get pods --all-namespaces -o custom-columns=NAMESPACE:.metadata.namespace,POD:.metadata.name,CONTAINER:.spec.containers[*].name --no-headers"
	getPodsInNamespaceFmt    = "kubectl get pods -n %s -o custom-columns=NAMESPACE:.metadata.namespace,POD:.metadata.name,CONTAINER:.spec.containers[*].name --no-headers"
	getCurrentNSLine         = "kubectl config view --minify -o jsonpath='{..namespace}'"
)

func (kc *KubernetesClient) GetTreeData() (string, error) {
	env := append(os.Environ(), "KUBECONFIG="+kc.configName)

	nsList, err := kc.resolveNamespaces(env)
	if err != nil {
		logger.Debugf("resolveNamespaces failed: %v", err)
		return "{}", err
	}
	if len(nsList) == 0 {
		return "{}", fmt.Errorf("no accessible namespaces detected for current credentials")
	}

	podLines, err := kc.listPodsSmart(env, nsList)
	if err != nil {
		logger.Debugf("listPodsSmart failed: %v", err)
		return "{}", err
	}

	k8sTree := NewTree()
	for _, line := range podLines {
		record := strings.Fields(line)
		if len(record) < 3 {
			continue
		}
		nsName, podName, containerNames := record[0], record[1], record[2]
		for _, containerName := range strings.Split(containerNames, ",") {
			if strings.TrimSpace(containerName) == "" {
				continue
			}
			k8sTree.InsertResource(nsName, podName, containerName)
		}
	}

	namespaces := make(map[string]*Namespace)
	for _, ns := range nsList {
		namespaces[ns] = &Namespace{Name: ns, Type: "namespace"}
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
	if k8sNode, ok := node.SubTree[resource]; ok {
		return k8sNode
	}
	newNode := NewNode()
	newNode.Type = resourceType
	newNode.Name = resource
	node.SubTree[resource] = newNode
	return newNode
}

func (node *K8sNode) searchContainers() []Container {
	containers := make([]Container, 0, len(node.SubTree))
	for _, container := range node.SubTree {
		containers = append(containers, Container{
			Type: container.Type,
			Name: container.Name,
		})
	}
	return containers
}

func (node *K8sNode) searchPods() []Pod {
	pods := make([]Pod, 0, len(node.SubTree))
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
	namespaces := make([]Namespace, 0, len(tree.Root.SubTree))
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
	cur.insert(ns, ResourceNamespace).
		insert(pod, ResourcePod).
		insert(container, ResourceContainer)
}

func (kc *KubernetesClient) resolveNamespaces(env []string) ([]string, error) {
	if curNS := strings.TrimSpace(shellOutOrEmpty(env, getCurrentNSLine)); curNS != "" {
		return []string{curNS}, nil
	}

	if out, err := runCmd(env, getNamespacesLineAll); err == nil {
		return nonEmptyLines(out), nil
	}

	if ns, err := extractNamespaceFromSAToken(kc.token); err == nil && ns != "" {
		return []string{ns}, nil
	} else {
		logger.Debugf("extractNamespaceFromSAToken failed: %v", err)
	}

	return nil, fmt.Errorf("cannot determine accessible namespaces: no list permission, no SA token namespace, no context namespace")
}

func (kc *KubernetesClient) listPodsSmart(env []string, nsList []string) ([]string, error) {
	if len(nsList) > 1 {
		if out, err := runCmd(env, getPodsAllNamespacesLine); err == nil {
			return nonEmptyLines(out), nil
		}
	}
	var all []string
	for _, ns := range nsList {
		cmd := fmt.Sprintf(getPodsInNamespaceFmt, shellEscape(ns))
		out, err := runCmd(env, cmd)
		if err != nil {
			logger.Debugf("list pods in ns %q failed: %v", ns, err)
			continue
		}
		all = append(all, nonEmptyLines(out)...)
	}
	return all, nil
}

func extractNamespaceFromSAToken(token string) (string, error) {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return "", fmt.Errorf("token is not a JWT")
	}
	payloadB64 := parts[1]

	payloadBytes, err := base64.RawURLEncoding.DecodeString(payloadB64)
	if err != nil {
		if payloadBytes2, err2 := base64.StdEncoding.DecodeString(payloadB64); err2 == nil {
			payloadBytes = payloadBytes2
		} else {
			return "", fmt.Errorf("failed to decode JWT payload: %v / %v", err, err2)
		}
	}

	var claims map[string]any
	if err := json.Unmarshal(payloadBytes, &claims); err != nil {
		return "", fmt.Errorf("failed to unmarshal JWT payload: %w", err)
	}

	if v, ok := claims["kubernetes.io/serviceaccount/namespace"]; ok {
		if ns, ok := v.(string); ok && strings.TrimSpace(ns) != "" {
			return ns, nil
		}
	}
	return "", fmt.Errorf("no serviceaccount namespace claim in token")
}

func runCmd(env []string, line string) (string, error) {
	cmd := exec.Command("bash", "-c", line)
	cmd.Env = env

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	if err != nil {
		return "", fmt.Errorf("cmd failed: %s, err: %w, stderr: %s", line, err, strings.TrimSpace(stderr.String()))
	}

	return stdout.String(), nil
}

func shellOutOrEmpty(env []string, line string) string {
	out, err := runCmd(env, line)
	if err != nil {
		return ""
	}
	return out
}

func nonEmptyLines(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	raw := strings.Split(strings.TrimSpace(s), "\n")
	dst := make([]string, 0, len(raw))
	for _, v := range raw {
		v = strings.TrimSpace(v)
		if v != "" && !strings.HasPrefix(v, "error:") {
			dst = append(dst, v)
		}
	}
	return dst
}

func shellEscape(s string) string {
	if !strings.ContainsAny(s, " \t\n'\"\\$`") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}
