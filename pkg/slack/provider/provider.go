package provider

import (
	"context"
	aws2 "github.com/heimdall-controller/heimdall/pkg/slack/provider/aws"
	azure2 "github.com/heimdall-controller/heimdall/pkg/slack/provider/azure"
	gcp2 "github.com/heimdall-controller/heimdall/pkg/slack/provider/gcp"
	openshift2 "github.com/heimdall-controller/heimdall/pkg/slack/provider/openshift"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"strings"
)

var (
	gcp       = "gce"
	aws       = "aws"
	azure     = "azure"
	openshift = "openshift"
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

func BuildNotificationURL(client kubernetes.Clientset, resourceInfo gcp2.ResourceInformation) string {
	provider := getProviderID(&client) // Get the provider ID (also sets the node name)
	providerID := strings.Split(provider, ":")[0]

	var link string
	switch {
	case providerID == gcp:
		link = gcp2.BuildGCPLink(provider, resourceInfo)
	case provider == aws:
		link = aws2.BuildAWSLink(provider, resourceInfo)
	case provider == azure:
		azure2.BuildAzureLink(provider, resourceInfo)
	case provider == openshift:
		openshift2.BuildOpenshiftLink(provider, resourceInfo)
	default:
		link = "http://127.0.0.1:40004/api/v1/namespaces/kubernetes-dashboard/services/http:kubernetes-dashboard:/proxy/#/" + strings.ToLower(resourceInfo.Kind) + "/" + resourceInfo.Namespace + "/" + resourceInfo.Name + "?namespace=" + resourceInfo.Namespace
	}

	return link
}
