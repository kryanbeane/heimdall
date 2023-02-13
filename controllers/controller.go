package controllers

import (
	"context"
	pluralize "github.com/gertd/go-pluralize"
	"github.com/itchyny/gojq"
	"github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	u "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"strings"
	"time"
)

type Controller struct {
	client.Client
	*runtime.Scheme
}

// Add +kubebuilder:rbac:groups=*,resources=*,verbs=get;list;watch
func (ec Controller) Add(mgr manager.Manager, requiredLabel string) error {
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(mgr.GetConfig())
	if err != nil {
		return err
	}

	dynamicClient, err := dynamic.NewForConfig(mgr.GetConfig())
	if err != nil {
		return err
	}

	if err := WatchResources(discoveryClient, dynamicClient, requiredLabel); err != nil {
		return err
	}

	return nil
}

func WatchResources(discoveryClient *discovery.DiscoveryClient, dynamicClient dynamic.Interface, requiredLabel string) error {
	go func() {
		ticker := time.NewTicker(time.Second * 100)

		for {
			select {
			case <-ticker.C:
				unstructuredItems, err := DiscoverClusterResources(context.TODO(), discoveryClient, dynamicClient, requiredLabel)
				if err != nil {
					logrus.Errorf("error retrieving all resources: %v", err)
					return
				}

				for _, item := range unstructuredItems {
					i := item
					go func() {
						watcher, err := dynamicClient.Resource(GroupVersionResourceFromUnstructured(&i)).Watch(context.TODO(), metav1.ListOptions{LabelSelector: "app.heimdall.io/watching=priority-level"})
						if err != nil {
							return
						}

						logrus.Infof("Watching Events on Resource: %s of type: %s", i.GetName(), i.GetObjectKind().GroupVersionKind().Kind)

						for {
							event, ok := <-watcher.ResultChan()
							if !ok {
								return
							}

							if event.Type == watch.Modified {
								unstructuredObj, ok := event.Object.(*u.Unstructured)
								if !ok {
									logrus.Error("error converting object to *unstructured.Unstructured")
									continue
								}

								if unstructuredObj.GetName() == i.GetName() {
									logrus.Infof("Resource %s has been modified", i.GetName())
									// TODO: Process the event
								}
							}
						}
					}()
				}
			}
		}
	}()
	return nil
}

func GetResourcesDynamically(dynamic dynamic.Interface, ctx context.Context, group string, version string, resource string) ([]u.Unstructured, error) {
	resourceId := schema.GroupVersionResource{
		Group:    group,
		Version:  version,
		Resource: resource,
	}

	list, err := dynamic.Resource(resourceId).Namespace("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	return list.Items, nil
}

func GroupVersionResourceFromUnstructured(o *u.Unstructured) schema.GroupVersionResource {
	resource := strings.ToLower(pluralize.NewClient().Plural(o.GetObjectKind().GroupVersionKind().Kind))
	return schema.GroupVersionResource{Group: o.GetObjectKind().GroupVersionKind().Group, Version: o.GetObjectKind().GroupVersionKind().Version, Resource: resource}
}

func DiscoverClusterResources(ctx context.Context, dc *discovery.DiscoveryClient, di dynamic.Interface, requiredLabelQuery string) ([]u.Unstructured, error) {
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

					// Get a list of all objects in the group/version/resource in json with the label
					jqItems, err := GetResourcesByJq(di, ctx, g, v, r, requiredLabelQuery)
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

func GetResourcesByJq(dynamic dynamic.Interface, ctx context.Context, group string, version string, resource string, labelQuery string) ([]u.Unstructured, error) {
	resources := make([]u.Unstructured, 0)

	query, err := gojq.Parse(labelQuery)
	if err != nil {
		logrus.Errorf("error parsing jq query: %v", err)
		return nil, err
	}

	items, err := GetResourcesDynamically(dynamic, ctx, group, version, resource)
	if err != nil {
		return nil, err
	}

	for _, item := range items {

		// Convert object to raw JSON
		var rawJson interface{}
		err = runtime.DefaultUnstructuredConverter.FromUnstructured(item.Object, &rawJson)
		if err != nil {
			logrus.Errorf("error converting object to raw JSON: %v", err)
			return nil, err
		}

		// Evaluate jq against JSON
		iter := query.Run(rawJson)
		for {
			result, ok := iter.Next()
			if !ok {
				break
			}
			if err, ok := result.(error); ok {
				if err != nil {
					logrus.Errorf("error evaluating jq: %v", err)
					return nil, err
				}
			} else {
				boolResult, ok := result.(bool)
				if !ok {
					logrus.Errorf("error converting jq result to bool: %v", err)
				} else if boolResult {
					resources = append(resources, item)
				}
			}
		}
	}
	return resources, nil
}
