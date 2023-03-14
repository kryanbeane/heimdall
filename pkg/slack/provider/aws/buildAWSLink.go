package aws

import (
	"github.com/heimdall-controller/heimdall/pkg/slack/provider/gcp"
	"strings"
)

// https://console.aws.amazon.com/eks/home#/clusters/CLUSTER_NAME/deployments/test-deployment
// clustername, deployment name, namespace

func BuildAWSLink(clusterURL string, resInfo gcp.ResourceInformation) string {
	clusterName := extractAWSInfo(clusterURL)
	return "https://console.aws.amazon.com/eks/home#/clusters/" + clusterName + "/deployments/" + resInfo.Namespace + "/" + resInfo.Name + "/overview"
}

func extractAWSInfo(clusterURL string) string {
	// Split the URL into parts
	parts := strings.Split(clusterURL, "/")

	// Extract the provider id, project id, and region
	clusterName := parts[5]

	return clusterName
}
