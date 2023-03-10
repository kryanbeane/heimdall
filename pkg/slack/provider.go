package slack

import (
	"context"
	"github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"strings"
)

func GetProviderId(clientset *kubernetes.Clientset) error {

	nodes, err := clientset.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return err
	}

	provider := ExtractProviderFromNode(&nodes.Items[0])
	logrus.Infof("Provider ID: %s", provider)

	return nil
}

func ExtractProviderFromNode(node *v1.Node) string {
	// could be any one of: gce, aws, azure, digitaloceon, ibm, vmware, openshift, rancher, heptio, platform9, anthos
	provider := strings.Split(node.Spec.ProviderID, ":")[0]
	if len(provider) == 0 {
		return "local"
	}
	return provider
}
