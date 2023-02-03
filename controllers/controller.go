package controllers

import (
	"context"
	"github.com/itchyny/gojq"
	"github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	u "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"strings"
)

type Controller struct {
	client.Client
	*runtime.Scheme
}

var _ reconcile.Reconciler = &Controller{}

// Add +kubebuilder:rbac:groups=*,resources=*,verbs=get;list;watch
func (ec Controller) Add(mgr manager.Manager, requiredLabel string) error {
	// Create a new Controller
	_, err := controller.New("heimdall", mgr,
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
		return err
	}

	for _, item := range unstructuredItems {

	}
	//// Create label selector containing the specified label
	//labelSelectorPredicate, err := predicate.LabelSelectorPredicate(selector)
	//if err != nil {
	//	logrus.Errorf("error creating label selector predicate: %v", err)
	//	return err
	//}

	//// Add a watch to objects containing that label
	//err = c.Watch(
	//	&source.Kind{Type: &v1.Pod{}}, &handler.EnqueueRequestForObject{})
	//if err != nil {
	//	logrus.Errorf("error creating watch for objects: %v", err)
	//	return err
	//}

	return nil
}

func GetResourcesDynamically(dynamic dynamic.Interface, ctx context.Context, group string, version string, resource string) ([]u.Unstructured, error) {
	resourceId := schema.GroupVersionResource{
		Group:    group,
		Version:  version,
		Resource: resource,
	}
	list, err := dynamic.Resource(resourceId).List(ctx, metav1.ListOptions{})

	if err != nil {
		logrus.Errorf("error listing resources dynamically: %v", err)
		return nil, err
	}

	return list.Items, nil
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
			return nil, err
		}

		for _, res := range resources.APIResources {
			// Exclude sub-resources
			if !strings.Contains(res.Name, "/") {
				resource = res.Name
			}

			items, err = GetResourcesByJq(di, ctx, group, version, resource, requiredLabelQuery)
			if err != nil {
				logrus.Errorf("error getting resources by jq: %v", err)
			}
		}
	}
	return items, nil
}

//+kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=pods/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=core,resources=pods/finalizers,verbs=update

func (ec Controller) Reconcile(ctx context.Context, req reconcile.Request) (ctrl.Result, error) {
	logrus.Infof("Reconciling pod %v", req.NamespacedName)

	//dc, err := discovery.NewDiscoveryClientForConfig(ctrl.GetConfigOrDie())
	//if err != nil {
	//	panic(err)
	//}

	//grvs, err := DiscoverArbitraryResources(dc)
	//if err != nil {
	//	return ctrl.Result{}, err
	//}

	//pod := &corev1.Pod{}
	//err := ec.Client.Get(ctx, req.NamespacedName, pod)
	//if err != nil {
	//	if errors.IsNotFound(err) {
	//		return ctrl.Result{}, nil
	//	}
	//	return ctrl.Result{}, err
	//}

	//// Get secret
	//slackSecret := &corev1.Secret{}
	//
	//err = ec.Client.Get(ctx, client.ObjectKey{
	//	Name:      "slack-credentials",
	//	Namespace: "default",
	//}, slackSecret)
	//
	//if err != nil {
	//	if errors.IsNotFound(err) {
	//		err := ec.Client.Create(ctx, slackSecret)
	//		if err != nil {
	//			return ctrl.Result{}, err
	//		}
	//		logrus.Infof("empty slack secret created: %s", slackSecret.Name)
	//	}
	//	logrus.Error(err, "failed to get slack credentials")
	//	return reconcile.Result{}, err
	//}
	//
	//logrus.Infof("found slack secret: %s", slackSecret.Name)
	//
	//slack.SendEvent(pod, slackSecret)

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
