package proxy

import (
	"context"
	"fmt"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/model"
	"github.com/jumpserver/koko/pkg/logger"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"net"
	"net/url"
)

type KubernetesClient struct {
	Client  *kubernetes.Clientset
	gateway *domainGateway
}

func NewKubernetesClient(address, token string, gateway *model.Gateway) (*KubernetesClient, error) {
	kc := &KubernetesClient{}
	err := kc.InitClient(address, token, gateway)
	if err != nil {
		logger.Error(err)
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

	config := &rest.Config{
		Host:        address,
		BearerToken: token,
		TLSClientConfig: rest.TLSClientConfig{
			Insecure: true,
		},
	}

	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("K8sCon new config err: %s", err)
	}

	kc.Client = client
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

func (kc *KubernetesClient) GetNamespaces() ([]string, error) {
	namespaces, err := kc.Client.CoreV1().Namespaces().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	var namespaceNames []string
	for _, ns := range namespaces.Items {
		namespaceNames = append(namespaceNames, ns.Name)
	}
	return namespaceNames, nil
}

func (kc *KubernetesClient) GetPodsInNamespace(namespace string) ([]string, error) {
	pods, err := kc.Client.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	var podNames []string
	for _, pod := range pods.Items {
		podNames = append(podNames, pod.Name)
	}
	return podNames, nil
}

func (kc *KubernetesClient) GetContainersInPod(namespace, podName string) ([]string, error) {
	pod, err := kc.Client.CoreV1().Pods(namespace).Get(context.TODO(), podName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	var containerNames []string
	for _, container := range pod.Spec.Containers {
		containerNames = append(containerNames, container.Name)
	}
	return containerNames, nil
}

func (kc *KubernetesClient) Close() {
	if kc.gateway != nil {
		kc.gateway.Stop()
	}
}
