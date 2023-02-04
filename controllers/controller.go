package controllers

import (
	"context"
	"fmt"
	"github.com/itchyny/gojq"
	"github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	u "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
	"strings"
)

type Controller struct {
	client.Client
	*runtime.Scheme
}

var _ reconcile.Reconciler = &Controller{}
var watches = make(map[schema.GroupVersionResource]watch.Interface)

// Add +kubebuilder:rbac:groups=*,resources=*,verbs=get;list;watch
func (ec Controller) Add(mgr manager.Manager, requiredLabel string) error {
	// Create a new Controller
	c, err := controller.New("heimdall", mgr,
		controller.Options{Reconciler: &Controller{
			Client: mgr.GetClient(),
			Scheme: mgr.GetScheme(),
		}})
	if err != nil {
		logrus.Errorf("failed to create heimdall controller: %v", err)
		return err
	}

	dynClient, err := discovery.NewDiscoveryClientForConfig(mgr.GetConfig())
	if err != nil {
		panic(err)
	}

	dynInterface, err := dynamic.NewForConfig(mgr.GetConfig())

	unstructuredItems, err := DiscoverGroupResourceVersions(context.TODO(), dynClient, dynInterface, requiredLabel)
	if err != nil {
		logrus.Errorf("error discovering group resource versions: %v", err)
		return err
	}

	for _, item := range unstructuredItems {
		logrus.Infof("adding watch for %s", item.Object["metadata"].(map[string]interface{})["labels"].(map[string]interface{}))
		if item.Object["metadata"].(map[string]interface{})["labels"].(map[string]interface{})["app.heimdall.io/watching"] == "priority-level" {

			test, err := GetGoType(item.GetObjectKind().GroupVersionKind(), mgr.GetScheme())
			if err != nil {
				return err
			}

			err = c.Watch(
				&source.Kind{Type: test}, &handler.EnqueueRequestForObject{})
			if err != nil {
				logrus.Errorf("error creating watch for objects: %v", err)
				return err
			}

			gvr := GroupVersionResourceFromUnstructured(&item)

			if _, ok := watches[gvr]; ok {
				//already have a watch set up for this resource
				continue
			}

			wi, err := dynInterface.Resource(gvr).Watch(context.TODO(), metav1.ListOptions{})
			if err != nil {
				logrus.Errorf("error watching resource: %v", err)
			}

			go func() {
				for {
					select {
					case event, ok := <-wi.ResultChan():
						if !ok {
							delete(watches, gvr)
							return
						}
						rawObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(event.Object)
						if err != nil {
							logrus.Errorf("error converting to unstructured: %v", err)
						}

						logrus.Infof("got object event: %v", rawObj["metadata"].(map[string]interface{})["name"])
					}
				}
			}()
			// add our watch to the map, so we don't add any new ones
			watches[gvr] = wi
			continue
		}
	}

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
		logrus.Errorf("error listing resources dynamically: %v", err)
		return nil, err
	}

	return list.Items, nil
}

func GroupVersionResourceFromUnstructured(o *u.Unstructured) schema.GroupVersionResource {
	resource := strings.ToLower(o.GetObjectKind().GroupVersionKind().Kind + "s")
	return schema.GroupVersionResource{Group: o.GetObjectKind().GroupVersionKind().Group, Version: o.GetObjectKind().GroupVersionKind().Version, Resource: resource}
}

//func (ec Controller) GetGoType(obj *u.Unstructured) (runtime.Object, error) {
//	gvk := obj.GetObjectKind().GroupVersionKind()
//	gv := schema.GroupVersion{Group: gvk.Group, Version: gvk.Version}
//	object, err := ec.Scheme.New(gv.WithKind(gvk.Kind))
//	if err != nil {
//		return nil, err
//	}
//	return object, nil
//}

func GetGoType(groupVersion schema.GroupVersionKind, scheme *runtime.Scheme) (client.Object, error) {
	// Create a new instance of the desired type
	obj, err := scheme.New(groupVersion)
	if obj == nil || err != nil {
		return nil, fmt.Errorf("failed to create a new instance of type %s", groupVersion.String())
	}

	// Cast the object to a client.Object
	clientObj, ok := obj.(client.Object)
	if !ok {
		return nil, fmt.Errorf("failed to cast object to client.Object")
	}

	return clientObj, nil
}

func DiscoverGroupResourceVersions(ctx context.Context, dc *discovery.DiscoveryClient, di dynamic.Interface, requiredLabelQuery string) ([]u.Unstructured, error) {
	// Get a list of all groups in the cluster
	_, resourceList, err := dc.ServerGroupsAndResources()
	if err != nil {
		panic(err)
	}

	var group, version, resource string
	var items []u.Unstructured

	for _, apiList := range resourceList {
		if !strings.Contains(apiList.GroupVersion, "/") {
			group = ""
			version = apiList.GroupVersion
		} else {
			group = strings.Split(apiList.GroupVersion, "/")[0]
			version = strings.Split(apiList.GroupVersion, "/")[1]
		}

		resources, err := dc.ServerResourcesForGroupVersion(apiList.GroupVersion)
		if err != nil {
			logrus.Errorf("error getting resources for group version: %v", err)
			continue
		}

		for _, res := range resources.APIResources {
			// Exclude sub-resources
			if !strings.Contains(res.Name, "/") {
				resource = res.Name
			}

			jqItems, err := GetResourcesByJq(di, ctx, group, version, resource, requiredLabelQuery)
			if err != nil {
				continue
			}
			items = append(items, jqItems...)
		}
		return items, nil
	}

	return items, nil
}

//+kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=pods/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=core,resources=pods/finalizers,verbs=update

func (ec Controller) Reconcile(ctx context.Context, req reconcile.Request) (ctrl.Result, error) {
	logrus.Infof("Reconciling %v", req.NamespacedName)

	return ctrl.Result{}, nil
}

func GetResourcesByJq(dynamic dynamic.Interface, ctx context.Context, group string, version string, resource string, jq string) ([]u.Unstructured, error) {
	resources := make([]u.Unstructured, 0)

	query, err := gojq.Parse(jq)
	if err != nil {
		logrus.Errorf("error parsing jq query: %v", err)
		return nil, err
	}

	items, err := GetResourcesDynamically(dynamic, ctx, group, version, resource)
	if err != nil {
		logrus.Errorf("error getting resources dynamically with jq: %v", err)
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
