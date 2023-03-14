package azure

import (
	"github.com/heimdall-controller/heimdall/pkg/slack/provider/gcp"
	"strings"
)

// https://console-openshift-console.apps.CLUSTER_URL/k8s/ns/NAMESPACE/deployment/DEPLOYMENT_NAME
// cluster_url, namespace, deployment name

func BuildAzureLink(clusterURL string, resInfo gcp.ResourceInformation) string {
	clusterName := extractAzureInfo(clusterURL)
	return "https://console-openshift-console.apps." + clusterName + "/k8s/ns/" + resInfo.Namespace + "/deployment/" + resInfo.Name + "/overview"
}

func extractAzureInfo(clusterURL string) string {
	// Split the URL into parts
	parts := strings.Split(clusterURL, "/")

	// Extract the provider id, project id, and region
	clusterName := parts[2]

	return clusterName
}
