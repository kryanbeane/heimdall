package controllers

import (
	"context"
	"github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
	"strings"
)

type Controller struct {
	client.Client
	*runtime.Scheme
}

var _ reconcile.Reconciler = &Controller{}

// Add +kubebuilder:rbac:groups=*,resources=*,verbs=get;list;watch
func (ec Controller) Add(mgr manager.Manager, selector metav1.LabelSelector) error {
	// Create a new Controller
	c, err := controller.New("event-controller", mgr,
		controller.Options{Reconciler: &Controller{
			Client: mgr.GetClient(),
			Scheme: mgr.GetScheme(),
		}})
	if err != nil {
		logrus.Errorf("failed to create pod controller: %v", err)
		return err
	}

	dc, err := discovery.NewDiscoveryClientForConfig(ctrl.GetConfigOrDie())
	if err != nil {
		panic(err)
	}

	// Create label selector containing the specified label
	labelSelectorPredicate, err := predicate.LabelSelectorPredicate(selector)
	if err != nil {
		logrus.Errorf("error creating label selector predicate: %v", err)
		return err
	}

	// TODO: Get all Kinds like ou can with kubectl, and then loop through and watch each with label selector

	// Add a watch to objects containing that label
	err = c.Watch(
		&source.Kind{Type: &v1.Pod{}}, &handler.EnqueueRequestForObject{}, labelSelectorPredicate)
	if err != nil {
		logrus.Errorf("Error creating watch for objects: %v", err)
		return err
	}

	return nil
}

func GetResourcesDynamically(dc dynamic.Interface, ctx context.Context, gvr schema.GroupVersionResource) ([]unstructured.Unstructured, error) {
	list, err := dc.Resource(gvr).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	return list.Items, nil
}

func DiscoverArbitraryResources(dc discovery.DiscoveryClient) (ctrl.Result, error) {

	// Get a list of all groups in the cluster
	_, resourceList, err := dc.ServerGroupsAndResources()
	if err != nil {
		panic(err)
	}

	var grvs []schema.GroupVersionResource
	var group, version, resource string

	for _, apiList := range resourceList {

		if !strings.Contains(apiList.GroupVersion, "/") {
			group = ""
			version = apiList.GroupVersion
		} else {
			group = strings.Split(apiList.GroupVersion, "/")[0]
			version = strings.Split(apiList.GroupVersion, "/")[1]
		}

		for _, res := range apiList.APIResources {
			// Exclude sub-resources
			if !strings.Contains(res.Name, "/") {
				resource = res.Name
			}

			gvr := schema.GroupVersionResource{
				Resource: resource,
				Group:    group,
				Version:  version,
			}
			grvs = append(grvs, gvr)
			logrus.Infof("GRV: %v", gvr.Resource)

		}
	}

}

//+kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=pods/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=core,resources=pods/finalizers,verbs=update

func (ec Controller) Reconcile(ctx context.Context, req reconcile.Request) (ctrl.Result, error) {
	logrus.Infof("Reconciling pod %v", req.NamespacedName)
	//grvs = append(grvs)
	//// logrus.Infof("APIVersion: %s, Kind: %s, Group Version: %s", g.APIVersion, g.Kind, g.GroupVersion)
	//for _, r := range g.APIResources {
	//	logrus.Infof("APIVersion: %s, Kind: %s, Group Version: %s, Resource: %s", g.APIVersion, g.Kind, g.GroupVersion, r.Name)
	//}

	//dc, err := dynamic.NewForConfig(ctrl.GetConfigOrDie())
	//if err != nil {
	//	return ctrl.Result{}, err
	//}
	//
	//resources, err := dc.Resource(schema.GroupVersionResource{
	//	Group:    "v1",
	//	Version:  "",
	//	Resource: "",
	//}).List(ctx, metav1.ListOptions{})
	//if err != nil {
	//	return ctrl.Result{}, err
	//}

	//for _, resource := range resources.Items {
	//	logrus.Info("test", resource)
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
