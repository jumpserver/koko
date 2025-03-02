package proxy

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"strings"
	"testing"
	"time"
)

const podLineMocked = `
default                     frontend                          nginx
default                     backend                           nodejs,mongodb
monitoring                  prometheus-server                 prometheus,config-reloader
logging                     fluentd-aggregator                fluentd,log-exporter
app-production              order-service                     java-app,redis-sidecar
app-production              payment-service                   go-app,postgresql
app-staging                 order-service                     java-app-staging,redis-staging
app-staging                 payment-service                   go-app-staging,postgres-staging
kube-system                 kube-proxy                        kube-proxy
kube-system                 coredns                           coredns
vault                       vault-server                      vault
ai-pipeline                 tensorflow-job                   trainer,metrics-collector,log-uploader
bigdata                     spark-driver                     spark-main,zeppelin,livy,history-server
ci-cd                       jenkins-controller               jenkins,jnlp-agent,git-sync,ssh-tunnel
long-namespace-name-123     very-long-pod-name-567890        minimal-container
special-chars               @weird_pod!                      $pecial-container,{test}-box
stress-test                 high-mem-pod                     memtester,swap-manager
stress-test                 high-cpu-pod                     cpuburner,throttle-monitor
namespace1                  pod1                             container1
namespace1                  pod2                             container1,container2
namespace2                  pod3                             containerA
namespace2                  pod4                             containerB,containerC
`

func TestK8sResourceTree_SearchNamespaces(t *testing.T) {
	k8sTree := NewTree()
	lines := strings.Split(strings.TrimSpace(podLineMocked), "\n")
	for _, line := range lines {
		elements := strings.Fields(line)
		ns, pod, containers := elements[0], elements[1], elements[2]
		for _, container := range strings.Split(containers, ",") {
			k8sTree.InsertResource(ns, pod, container)
		}
	}
	namespaces := k8sTree.SearchNamespaces()
	fmt.Println(namespaces)
}

func TestK8sResourceTree_ResultTest(t *testing.T) {
	mockedData := GenPodLines(30000)
	res1 := v1(mockedData)
	res2 := v2(mockedData)
	if !nsIsEqual(res1, res2) {
		panic("not equal")
	}
	fmt.Println("ok")
}

func TestK8sTreeGen_SpeedTest(t *testing.T) {
	mockedData := GenPodLines(30000) // 模拟企业级生产环境小集群架构下的k8s规模

	var duration1, duration2 int64
	st1 := time.Now()
	for i := 0; i <= 100; i++ {
		v1(mockedData)
	}
	duration1 = time.Now().Sub(st1).Milliseconds()

	st2 := time.Now()
	for i := 0; i <= 100; i++ {
		v2(mockedData)
	}
	duration2 = time.Now().Sub(st2).Milliseconds()

	fmt.Printf("v1: %d\n", duration1/100)
	fmt.Printf("v2: %d\n", duration2/100)
	fmt.Printf("improvement: %2f\n", (float64(duration1-duration2))/float64(duration1))
}

func containersIsEqual(c1, c2 []Container) (ans bool) {
	defer func() {
		if !ans {
			fmt.Println("container error")
		}
	}()
	c1Map := make(map[string]Container)
	for _, c := range c1 {
		c1Map[c.Name] = c
	}
	for _, c := range c2 {
		if cc, ok := c1Map[c.Name]; !ok {
			return
		} else if cc != c {
			return
		}
		delete(c1Map, c.Name)
	}
	ans = len(c1Map) == 0
	return
}

func podsIsEqual(p1, p2 []Pod) (ans bool) {
	defer func() {
		if !ans {
			fmt.Println("pod error")
		}
	}()
	p1Map := make(map[string]Pod)
	for _, p := range p1 {
		p1Map[p.Name] = p
	}
	for _, p := range p2 {
		if pp, ok := p1Map[p.Name]; !ok {
			return
		} else if p.Name != pp.Name || !containersIsEqual(p.Containers, pp.Containers) {
			return
		}

		delete(p1Map, p.Name)
	}

	ans = len(p1Map) == 0
	return
}

func nsIsEqual(n1, n2 map[string]*Namespace) (ans bool) {
	defer func() {
		if !ans {
			fmt.Println("ns error")
		}
	}()
	n1Map := n1
	for _, n := range n2 {
		if nn, ok := n1Map[n.Name]; !ok {
			return
		} else if n.Name != nn.Name || !podsIsEqual(n.Pods, nn.Pods) {
			return
		}

		delete(n1Map, n.Name)
	}
	ans = len(n1Map) == 0
	return
}

func v2(podLines []string) map[string]*Namespace {
	namespaces := make(map[string]*Namespace)
	k8sTree := NewTree()
	for _, line := range podLines {
		elements := strings.Fields(line)
		ns, pod, containers := elements[0], elements[1], elements[2]
		for _, container := range strings.Split(containers, ",") {
			k8sTree.InsertResource(ns, pod, container)
		}
	}
	for _, namespace := range k8sTree.SearchNamespaces() {
		namespaceCopy := namespace
		namespaces[namespace.Name] = &namespaceCopy
	}
	return namespaces
}

func v1(podLines []string) map[string]*Namespace {
	namespaces := make(map[string]*Namespace)
	for _, line := range podLines {
		record := strings.Fields(line)
		if len(record) < 3 {
			continue
		}

		nsName, podName, containerName := record[0], record[1], record[2]

		ns, exists := namespaces[nsName]
		if !exists {
			ns = &Namespace{Name: nsName, Type: "namespace"}
			namespaces[nsName] = ns
		}

		var pod *Pod
		for i := range ns.Pods {
			if ns.Pods[i].Name == podName {
				pod = &ns.Pods[i]
				break
			}
		}

		if pod == nil {
			pod = &Pod{Name: podName, Type: "pod"}
			ns.Pods = append(ns.Pods, *pod)
		}

		for i := range ns.Pods {
			if ns.Pods[i].Name == podName {
				containers := make([]Container, 0)
				for _, v := range strings.Split(containerName, ",") {
					containers = append(containers, Container{Name: v, Type: "container"})
				}
				ns.Pods[i].Containers = append(ns.Pods[i].Containers, containers...)
				break
			}
		}
	}
	return namespaces
}

// 模拟大规模的生产环境的k8s集群数据
//
//nolint:gosec
func GenPodLines(scale int) []string {
	lines := make([]string, scale)

	// 预生成命名空间列表（约100个不同的ns）
	nsList := make([]string, 100)
	for i := range nsList {
		nsList[i] = genNsName()
	}

	// 真实集群的典型分布模式
	for i := 0; i < scale; i++ {
		var ns string
		switch {
		case i < scale/20: // 5% 系统组件
			ns = choice([]string{"kube-system", "kube-public", "istio-system"})
		case i < scale/100*15: // 15% 监控日志
			ns = choice([]string{"monitoring", "logging", "security"})
		case i < scale/2: // 50% 业务应用
			ns = nsList[CryptoRandInt(30)+20] // 使用前50个业务ns
		default: // 30% 其他
			ns = nsList[CryptoRandInt(len(nsList))]
		}

		lines[i] = fmt.Sprintf("%s\t%s\t%s",
			ns,
			genPodName(ns),
			genContainers(ns),
		)
	}
	return lines
}

// 生成命名空间名称
//
//nolint:gosec
func genNsName() string {
	prefix := choice([]string{
		"app", "team", "project", "service", "system",
		"infra", "test", "staging", "prod", "backend",
	})
	suffix := fmt.Sprintf("%03d", CryptoRandInt(200))
	return fmt.Sprintf("%s-%s-%s", prefix, choice([]string{"web", "api", "data", "mobile"}), suffix)
}

// 生成Pod名称（基于命名空间特征）
//
//nolint:gosec
func genPodName(ns string) string {
	parts := strings.Split(ns, "-")
	appType := ""
	if len(parts) > 1 {
		appType = parts[1]
	} else {
		appType = parts[0]
	}

	templates := map[string][]string{
		"web":    {"frontend", "ui", "portal"},
		"api":    {"gateway", "service", "controller"},
		"data":   {"database", "redis", "kafka"},
		"mobile": {"android", "ios", "sync"},
	}

	return fmt.Sprintf("%s-%s-%s-%s",
		appType,
		choice(templates[appType]),
		choice([]string{"prod", "dev", "canary"}),
		randomString(4),
	)
}

// 生成容器列表（带sidecar模式）
//
//nolint:gosec
func genContainers(ns string) string {
	baseContainers := []string{
		"nginx", "nodejs", "java-app", "python",
		"postgres", "redis", "kafka", "elasticsearch",
	}

	sidecars := []string{
		"prometheus-exporter", "istio-proxy",
		"log-collector", "vault-agent",
	}

	var containers []string

	// 主容器
	main := baseContainers[CryptoRandInt(len(baseContainers))]
	containers = append(containers, main)

	// 30%的Pod带sidecar
	if CryptoRandInt(100) < 30 {
		containers = append(containers, sidecars[CryptoRandInt(len(sidecars))])
	}

	// 系统命名空间特殊处理
	if strings.Contains(ns, "kube-system") {
		return strings.Join([]string{"kube-proxy", "metrics-server"}, ",")
	}

	return strings.Join(containers, ",")
}

// 辅助函数：随机选择
//
//nolint:gosec // 测试数据生成无需密码学安全随机
func choice(options []string) string {
	if len(options) == 0 {
		return "none"
	}
	return options[CryptoRandInt(len(options))]
}

// 生成随机字符串
//
//nolint:gosec
func randomString(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[CryptoRandInt(len(letters))]
	}
	return string(b)
}

func CryptoRandInt(max int) int {
	bigRange := big.NewInt(int64(max))

	randomNum, err := rand.Int(rand.Reader, bigRange)
	if err != nil {
		return 0
	}

	// 调整到目标范围并返回
	return int(randomNum.Int64())
}
