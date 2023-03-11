package gcp

import (
	"strings"
)

type ResourceInformation struct {
	Name      string
	Namespace string
	NodeName  string
}

func BuildGCPLink(clusterURL string, resInfo ResourceInformation) string {
	projectID, region := extractGCPInfo(clusterURL)
	clusterName := extractClusterNameFromNodeName(resInfo.NodeName)
	return "https://console.cloud.google.com/kubernetes/deployment/" + region + "/" + clusterName + "/" + resInfo.Namespace + "/" + resInfo.Name + "/overview?project=" + projectID
}

func extractGCPInfo(clusterURL string) (string, string) {
	// Split the URL into parts
	parts := strings.Split(clusterURL, "/")

	// Extract the provider id, project id, and region
	projectID := parts[2]
	region := strings.Split(parts[3], "-")[0] + "-" + strings.Split(parts[3], "-")[1]

	return projectID, region
}

func extractClusterNameFromNodeName(nodeName string) string {
	parts := strings.Split(nodeName, "-")
	numParts := len(parts)

	if numParts <= 6 {
		return parts[1]
	} else {
		return strings.Join(parts[1:numParts-4], "-")
	}
}
