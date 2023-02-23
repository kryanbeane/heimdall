package controllers

import (
	"context"
	"github.com/gertd/go-pluralize"
	"github.com/heimdall-controller/heimdall/pkg/slack"
	"github.com/itchyny/gojq"
	"github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	u "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
	"strings"
	"sync"
	"time"
)

type Controller struct {
	Client        client.Client
	Scheme        *runtime.Scheme
	DynamicClient dynamic.Interface
}

const (
	configMapName = "heimdall-settings"
	namespace     = "heimdall-controller"
	watchingLabel = "app.heimdall.io/watching"
	secretName    = "heimdall-secret"
)

var configMap = v1.ConfigMap{
	ObjectMeta: metav1.ObjectMeta{
		Name:      configMapName,
		Namespace: namespace,
	},
	Data: map[string]string{
		"slack-channel":           "your-channel",
		"low-priority-cadence":    "600",
		"medium-priority-cadence": "300",
		"high-priority-cadence":   "60",
	},
}

var secret = v1.Secret{
	ObjectMeta: metav1.ObjectMeta{
		Name:      secretName,
		Namespace: namespace,
	},
	Data: map[string][]byte{
		"slack-token": []byte("your-token"),
	},
}

var resources = sync.Map{}
var lastNotificationTimes = sync.Map{}

// InitializeController Add +kubebuilder:rbac:groups=*,resources=*,verbs=get;list;watch
func (c *Controller) InitializeController(mgr manager.Manager, requiredLabel string) error {
	dynamicClient, err := dynamic.NewForConfig(mgr.GetConfig())
	if err != nil {
		return err
	}

	ctrlr, err := controller.New("controller", mgr,
		controller.Options{Reconciler: &Controller{
			Client:        mgr.GetClient(),
			Scheme:        mgr.GetScheme(),
			DynamicClient: dynamicClient,
		}})
	if err != nil {
		logrus.Errorf("failed to create controller: %v", err)
		return err
	}

	discoveryClient, err := discovery.NewDiscoveryClientForConfig(mgr.GetConfig())
	if err != nil {
		return err
	}

	if err := c.WatchResources(ctrlr, discoveryClient, dynamicClient, requiredLabel); err != nil {
		return err
	}

	return nil
}

func (c *Controller) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {

	//TODO Remember that this reconcile is triggered on every event happening to the object
	// Eventually Heimdall will need to track when the last notification was sent but for now
	// We can just send them on every event

	resource, ok := c.RetrieveResourceFromMap(request.NamespacedName.String())
	if !ok {
		logrus.Errorf("failed to retrieve resource from map: %v", request.NamespacedName.String())
		return reconcile.Result{}, nil
	}

	logrus.Infof("reconciling %s with importance of %s", request.NamespacedName, resource.GetLabels()[watchingLabel])

	// If resource no longer has the label or if label is empty (no priority set) then delete it from the map
	if value, labelExists := resource.GetLabels()[watchingLabel]; !labelExists || value == "" {
		resources.Delete(request.NamespacedName.String())
		return reconcile.Result{}, nil
	}

	// Set priority level based on label - if invalid priority, default to low
	priority := strings.ToLower(resource.GetLabels()[watchingLabel])
	if priority != "low" && priority != "medium" && priority != "high" {
		logrus.Errorf("invalid priority set: %s, for resource: %s, defaulting to low priority", priority, resource.GetName())
		resource.GetLabels()[watchingLabel] = "low"

		gvr := GVRFromUnstructured(resource)

		if obj, err := c.DynamicClient.Resource(gvr).Namespace(resource.GetNamespace()).Get(ctx, resource.GetName(), metav1.GetOptions{}); err == nil {
			labels := obj.GetLabels()
			labels[watchingLabel] = "low"
			obj.SetLabels(labels)
			logrus.Infof("updating label for resource: %s", resource.GetName())

			if _, err := c.DynamicClient.Resource(gvr).Namespace(obj.GetNamespace()).Update(ctx, obj, metav1.UpdateOptions{}); err != nil {
				return reconcile.Result{}, err
			}
			resource = *obj
		} else {
			return reconcile.Result{}, err
		}

		logrus.Infof("successfully updated resource: %s", resource.GetName())
	}

	configMap, err := c.ReconcileConfigMap(ctx)
	if err != nil {
		return reconcile.Result{}, err
	}

	secret, err = c.ReconcileSecret(ctx)
	if err != nil {
		return reconcile.Result{}, err
	}

	slack.SendEvent(resource, secret, configMap)

	return reconcile.Result{}, nil
}

func (c *Controller) RetrieveResourceFromMap(key string, m *sync.Map) (u.Unstructured, bool) {
	if res, ok := m.Load(key); !ok {
		return u.Unstructured{}, false
	} else {
		return res.(u.Unstructured), true
	}
}

func GVRFromUnstructured(o u.Unstructured) schema.GroupVersionResource {
	resource := strings.ToLower(pluralize.NewClient().Plural(o.GetObjectKind().GroupVersionKind().Kind))
	return schema.GroupVersionResource{Group: o.GetObjectKind().GroupVersionKind().Group, Version: o.GetObjectKind().GroupVersionKind().Version, Resource: resource}
}

func (c *Controller) ReconcileConfigMap(ctx context.Context) (v1.ConfigMap, error) {
	var cm v1.ConfigMap

	if err := c.Client.Get(ctx, client.ObjectKeyFromObject(&configMap), &cm); err != nil {
		if errors.IsNotFound(err) {
			if err := c.Client.Create(ctx, &configMap); err != nil {
				return v1.ConfigMap{}, err
			}
			// Successful creation
			return configMap, nil
		}
		return v1.ConfigMap{}, err
	}

	// Successfully retrieved configmap
	return cm, nil
}

func (c *Controller) ReconcileSecret(ctx context.Context) (v1.Secret, error) {
	var s v1.Secret

	if err := c.Client.Get(ctx, client.ObjectKeyFromObject(&secret), &s); err != nil {
		if errors.IsNotFound(err) {
			if err := c.Client.Create(ctx, &secret); err != nil {
				return v1.Secret{}, err
			}
			// Successful creation
			return secret, nil
		}
		return v1.Secret{}, err
	}

	// Successfully retrieved configmap
	return s, nil
}

func (c *Controller) WatchResources(controller controller.Controller, discoveryClient *discovery.DiscoveryClient, dynamicClient dynamic.Interface, requiredLabel string) error {

	// Create label selector containing the specified label key with no value (LabelSelectorOpExists checks that key exists)
	pred, err := predicate.LabelSelectorPredicate(metav1.LabelSelector{
		MatchExpressions: []metav1.LabelSelectorRequirement{
			{
				Key:      watchingLabel,
				Operator: metav1.LabelSelectorOpExists,
			},
		},
	})
	if err != nil {
		return err
	}

	go func() {
		ticker := time.NewTicker(time.Second * 5)

		for {
			select {
			case <-ticker.C:

				unstructuredItems, err := DiscoverClusterGRVs(context.TODO(), discoveryClient, dynamicClient, requiredLabel)
				if err != nil {
					logrus.Errorf("error discovering cluster resources: %v", err)
					return
				}

				for _, item := range unstructuredItems {
					item := item
					go func() {
						// Add the unstructured item to the resources map
						if _, ok := c.RetrieveResourceFromMap(item.GetNamespace()+item.GetName(), &resources); !ok {
							resources.Store(item.GetNamespace()+"/"+item.GetName(), item)
						}
						// Add the last notification time to the notification map
						if _, ok := c.RetrieveResourceFromMap(item.GetNamespace()+item.GetName(), &lastNotificationTimes); !ok {
							lastNotificationTimes.Store(item.GetNamespace()+"/"+item.GetName(), time.Now().Add(time.Duration(-1000)*time.Hour))
						}

						err = controller.Watch(
							&source.Kind{Type: &item},
							&handler.EnqueueRequestForObject{},
							pred)
						if err != nil {
							return
						}
					}()
				}

				//for _, item := range unstructuredItems {
				//	i := item
				//	go func() {
				//		watcher, err := dynamicClient.Resource(GroupVersionResourceFromUnstructured(&i)).Watch(context.TODO(), metav1.ListOptions{LabelSelector: "app.heimdall.io/watching=priority-level"})
				//		if err != nil {
				//			return
				//		}
				//
				//		logrus.Infof("Watching Events on Resource: %s of type: %s", i.GetName(), i.GetObjectKind().GroupVersionKind().Kind)
				//
				//		for {
				//			event, ok := <-watcher.ResultChan()
				//			if !ok {
				//				return
				//			}
				//
				//			if event.Type == watch.Modified {
				//				unstructuredObj, ok := event.Object.(*u.Unstructured)
				//				if !ok {
				//					logrus.Error("error converting object to *unstructured.Unstructured")
				//					continue
				//				}
				//
				//				if unstructuredObj.GetName() == i.GetName() {
				//					logrus.Infof("Resource %s has been modified", i.GetName())
				//					// TODO: Process the event
				//				}
				//			}
				//		}
				//	}()
				//}
			}
		}
	}()
	return nil
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

func DiscoverClusterGRVs(ctx context.Context, dc *discovery.DiscoveryClient, di dynamic.Interface, requiredLabelQuery string) ([]u.Unstructured, error) {
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

	items, err := GetUnstructuredResourceList(dynamic, ctx, group, version, resource)
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
