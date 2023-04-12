package helpers

import (
	"context"
	"github.com/gertd/go-pluralize"
	"github.com/itchyny/gojq"
	"github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	u "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"strings"
)

func GetResourcesByJq(dynamic dynamic.Interface, ctx context.Context, group string, version string, resource string, labelQuery string, namepace string, name string) ([]u.Unstructured, error) {
	resources := make([]u.Unstructured, 0)
	var query *gojq.Query
	var err error
	if labelQuery != "" {
		query, err = gojq.Parse(labelQuery)
		if err != nil {
			logrus.Errorf("error parsing jq query: %v", err)
			return nil, err
		}
	}

	items, err := GetUnstructuredResourceList(dynamic, ctx, group, version, resource)
	if err != nil {
		return nil, err
	}

	for _, item := range items {

		// handle label search
		if name == "" && namepace == "" {
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

		// handle name & ns search
		if item.GetNamespace() == namepace && item.GetName() == name {
			resources = append(resources, item)
		}
	}
	return resources, nil
}

func GetUnstructuredResourceList(dynamic dynamic.Interface, ctx context.Context, group string, version string, resource string) ([]u.Unstructured, error) {
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

func GVRFromUnstructured(o u.Unstructured) schema.GroupVersionResource {
	resource := strings.ToLower(pluralize.NewClient().Plural(o.GetObjectKind().GroupVersionKind().Kind))
	return schema.GroupVersionResource{Group: o.GetObjectKind().GroupVersionKind().Group, Version: o.GetObjectKind().GroupVersionKind().Version, Resource: resource}
}
