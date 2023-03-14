package openshift

import (
	"github.com/heimdall-controller/heimdall/pkg/slack/provider/gcp"
	"strings"
)

func BuildOpenshiftLink(clusterURL string, resInfo gcp.ResourceInformation) string {
	clusterName := extractOpenshiftInfo(clusterURL)
	return "https://console-openshift-console.apps." + clusterName + "/k8s/ns/" + resInfo.Namespace + "/deployment/" + resInfo.Name + "/overview"
}

func extractOpenshiftInfo(clusterURL string) string {
	// Split the URL into parts
	parts := strings.Split(clusterURL, "/")

	// Extract the provider id, project id, and region
	clusterName := parts[2]

	return clusterName
}
