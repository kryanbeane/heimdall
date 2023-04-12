package helpers

import (
	"context"
	u "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"strings"
)

type discoverClusterGRVsFunc func(di dynamic.Interface, ctx context.Context, g, v, r, query string, namespace string, name string) ([]u.Unstructured, error)

func discoverClusterGRVs(dc *discovery.DiscoveryClient, di dynamic.Interface, query string, namespace string, name string, discoverFunc discoverClusterGRVsFunc) ([]u.Unstructured, error) {
	var g, v, r string
	var items []u.Unstructured

	// Get all server groups found in the cluster
	apiGroupList, err := dc.ServerGroups()
	if err != nil {
		panic(err)
	}

	// Loop through all groups found, so apps, events.k8s.io, apiregistration.k8s.io, etc... (and custom groups - like heimdall.k8s.io)
	for _, apiGroup := range apiGroupList.Groups {

		// Loop through all versions found in each group, so v1, v1beta1, etc...
		for _, version := range apiGroup.Versions {

			// Get a list of all server resources for each group version found in the cluster
			groupVersion, err := dc.ServerResourcesForGroupVersion(version.GroupVersion)
			if err != nil {
				return nil, err
			}

			// Loop through all resources found in each group version
			for _, resource := range groupVersion.APIResources {
				g = apiGroup.Name
				v = version.Version

				if !strings.Contains(resource.Name, "/") {
					r = resource.Name

					jqItems, err := discoverFunc(di, context.Background(), g, v, r, query, namespace, name)
					if err != nil {
						continue
					}

					items = append(items, jqItems...)
				}
			}
		}
	}

	return items, nil
}

func DiscoverClusterGRVsByLabel(dc *discovery.DiscoveryClient, di dynamic.Interface, requiredLabelQuery string) ([]u.Unstructured, error) {
	discoverFunc := func(di dynamic.Interface, ctx context.Context, g, v, r, query, namespace, name string) ([]u.Unstructured, error) {
		return GetResourcesByJq(di, ctx, g, v, r, query, namespace, name)
	}

	return discoverClusterGRVs(dc, di, requiredLabelQuery, "", "", discoverFunc)
}

func DiscoverClusterGRVsByNamespacedName(dc *discovery.DiscoveryClient, di dynamic.Interface, namespace string, name string) ([]u.Unstructured, error) {
	discoverFunc := func(di dynamic.Interface, ctx context.Context, g, v, r, query, namespace, name string) ([]u.Unstructured, error) {
		return GetResourcesByJq(di, ctx, g, v, r, query, namespace, name)
	}

	return discoverClusterGRVs(dc, di, "", namespace, name, discoverFunc)
}
