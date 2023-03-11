package provider

import (
	"context"
	gcp2 "github.com/heimdall-controller/heimdall/pkg/slack/provider/gcp"
	"github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"strings"
)

var (
	gcp          = "gce"
	aws          = "aws"
	azure        = "azure"
	digitaloceon = "digitaloceon"
	ibm          = "ibm"
	vmware       = "vmware"
	openshift    = "openshift"
)

var resourceInfo gcp2.ResourceInformation

func getProviderID(clientset *kubernetes.Clientset) string {
	nodes, err := clientset.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return ""
	}

	resourceInfo.NodeName = nodes.Items[0].Name
	return nodes.Items[0].Spec.ProviderID
}

func BuildNotificationURL(client kubernetes.Clientset, resourceInformation gcp2.ResourceInformation) string {
	resourceInfo = resourceInformation // Set the resource information (node name will be blank)
	provider := getProviderID(&client) // Get the provider ID (also sets the node name)
	providerID := strings.Split(provider, ":")[0]

	var link string
	switch {
	case providerID == gcp:
		link = gcp2.BuildGCPLink(provider, resourceInfo)
	case provider == aws:
		logrus.Infof("Provider is AWS")
	case provider == azure:
		logrus.Infof("Provider is Azure")
	case provider == openshift:
		logrus.Infof("Provider is OpenShift")
	default:
		link = "http://127.0.0.1:40307/api/v1/namespaces/" + resourceInfo.Namespace + "/services/http:kubernetes-dashboard:/proxy/#/deployment/" + resourceInfo.Namespace + "/" + resourceInfo.Name + "?namespace=" + resourceInfo.Namespace
	}

	return link
}
