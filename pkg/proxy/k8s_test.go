package proxy

import (
	"fmt"
	"math/rand"
	"reflect"
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
	mockedData := GenPodLines()
	res1 := v1(mockedData)
	res2 := v2(mockedData)
	if !reflect.DeepEqual(res1, res2) {
		panic("not equal")
	}
	fmt.Println("ok")
}

func TestK8sTreeGen_SpeedTest(t *testing.T) {
	mockedData := GenPodLines()
	st1 := time.Now()
	v1(mockedData)
	fmt.Printf("duration v1: %dms", time.Now().Sub(st1).Milliseconds())
	st2 := time.Now()
	v2(mockedData)
	fmt.Printf("duration v2: %dms", time.Now().Sub(st2).Milliseconds())
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
		namespaces[namespace.Name] = &namespace
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
func GenPodLines() []string {
	rand.Seed(time.Now().UnixNano())
	lines := make([]string, 10000)

	// 预生成命名空间列表（约100个不同的ns）
	nsList := make([]string, 100)
	for i := range nsList {
		nsList[i] = genNsName()
	}

	// 真实集群的典型分布模式
	for i := 0; i < 10000; i++ {
		var ns string
		switch {
		case i < 500: // 5% 系统组件
			ns = choice([]string{"kube-system", "kube-public", "istio-system"})
		case i < 2000: // 15% 监控日志
			ns = choice([]string{"monitoring", "logging", "security"})
		case i < 7000: // 50% 业务应用
			ns = nsList[rand.Intn(30)+20] // 使用前50个业务ns
		default: // 30% 其他
			ns = nsList[rand.Intn(len(nsList))]
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
func genNsName() string {
	prefix := choice([]string{
		"app", "team", "project", "service", "system",
		"infra", "test", "staging", "prod", "backend",
	})
	suffix := fmt.Sprintf("%03d", rand.Intn(200))
	return fmt.Sprintf("%s-%s-%s", prefix, choice([]string{"web", "api", "data", "mobile"}), suffix)
}

// 生成Pod名称（基于命名空间特征）
func genPodName(ns string) string {
	parts := strings.Split(ns, "-")
	appType := parts[1]

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
	main := baseContainers[rand.Intn(len(baseContainers))]
	containers = append(containers, main)

	// 30%的Pod带sidecar
	if rand.Float32() < 0.3 {
		containers = append(containers, sidecars[rand.Intn(len(sidecars))])
	}

	// 系统命名空间特殊处理
	if strings.Contains(ns, "kube-system") {
		return strings.Join([]string{"kube-proxy", "metrics-server"}, ",")
	}

	return strings.Join(containers, ",")
}

// 辅助函数：随机选择
func choice(options []string) string {
	return options[rand.Intn(len(options))]
}

// 生成随机字符串
func randomString(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}
