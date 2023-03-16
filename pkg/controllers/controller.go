package controllers

import (
	"bytes"
	"context"
	"github.com/gertd/go-pluralize"
	"github.com/heimdall-controller/heimdall/pkg/controllers/jq"
	"github.com/heimdall-controller/heimdall/pkg/controllers/map"
	"github.com/heimdall-controller/heimdall/pkg/slack"
	"github.com/heimdall-controller/heimdall/pkg/slack/provider"
	gcp2 "github.com/heimdall-controller/heimdall/pkg/slack/provider/gcp"
	"github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	u "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
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
		"slack-channel":           "default-heimdall-channel",
		"low-priority-cadence":    "600s",
		"medium-priority-cadence": "300s",
		"high-priority-cadence":   "60s",
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

var clientset *kubernetes.Clientset

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

	// Create a Kubernetes client for low-level work
	clientset, err = kubernetes.NewForConfig(mgr.GetConfig())
	if err != nil {
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

	watchingResourcesCount.Set(float64(_map.GetMapLength(&resources)))

	resourceRef, ok := _map.RetrieveResourceFromMap(request.NamespacedName.String(), &resources)
	if !ok {
		logrus.Errorf("failed to retrieve resource from map: %v", request.NamespacedName.String())
		return reconcile.Result{}, nil
	}

	// Update the map with the latest resource version
	_map.UpdateMapWithResource(resourceRef, &resources)

	gvr := GVRFromUnstructured(resourceRef)
	resource, err := c.DynamicClient.Resource(gvr).Namespace(resourceRef.GetNamespace()).Get(ctx, resourceRef.GetName(), metav1.GetOptions{})
	if err != nil {
		return reconcile.Result{}, err
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
		logrus.Warnf("invalid priority set: %s, for resource: %s, defaulting to low priority", priority, resource.GetName())
		resource.GetLabels()[watchingLabel] = "low"

		labels := resource.GetLabels()
		labels[watchingLabel] = "low"
		resource.SetLabels(labels)
		priority = "low"

		if _, err := c.DynamicClient.Resource(gvr).Namespace(resource.GetNamespace()).Update(ctx, resource, metav1.UpdateOptions{}); err != nil {
			return reconcile.Result{}, err
		}
	}

	// Get notification URL
	url := provider.BuildNotificationURL(*clientset,
		gcp2.ResourceInformation{
			Name:      resource.GetName(),
			Namespace: resource.GetNamespace(),
			NodeName:  "",
		},
	)
	logrus.Infof("notification url: %s", url)

	cm, err := c.ReconcileConfigMap(ctx)
	if err != nil {
		return reconcile.Result{}, err
	}
	if cm.Data == nil {
		logrus.Warn("config map not configured, please configure Heimdall config map")
		return reconcile.Result{}, nil
	}

	secret, err = c.ReconcileSecret(ctx)
	if err != nil {
		return reconcile.Result{}, err
	}
	if secret.Data == nil {
		logrus.Warn("secret not configured, please configure Heimdall secret")
		return reconcile.Result{}, nil
	}

	err = c.ReconcileNotificationCadence(request, resource, url, priority, cm, secret)
	if err != nil {
		return ctrl.Result{}, nil
	}

	return reconcile.Result{}, nil
}

func sendSlackNotification(name string, resource *u.Unstructured, url string, priority string, secret v1.Secret, configMap v1.ConfigMap) error {
	if err := slack.SendEvent(*resource, url, priority, secret, configMap); err != nil {
		return err
	}
	lastNotificationTimes.Store(name, time.Now())
	return nil
}

func (c *Controller) ReconcileNotificationCadence(request reconcile.Request, resource *u.Unstructured, url string, priority string, configMap v1.ConfigMap, secret v1.Secret) error {
	cadence, err := time.ParseDuration(configMap.Data[priority+"-priority-cadence"])
	if err != nil {
		return err
	}

	lastNotificationTime, loaded := lastNotificationTimes.LoadOrStore(request.NamespacedName.String(), time.Now())
	if loaded {
		if !lastNotificationTime.(time.Time).Add(cadence).Before(time.Now()) {
			logrus.Warnf("skipping notification for %s because the last message was sent less than %s ago", request.NamespacedName.String(), cadence.String())
			return nil
		}
	}

	if err = sendSlackNotification(request.NamespacedName.String(), resource, url, priority, secret, configMap); err != nil {
		return err
	}
	return nil
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

	channelValue := cm.Data["slack-channel"]
	if channelValue == "default-heimdall-channel" || channelValue == "" {
		return v1.ConfigMap{Data: nil}, nil
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

	slackValue := s.Data["slack-token"]
	if slackValue == nil || bytes.Equal(slackValue, []byte("your-token")) {
		return v1.Secret{Data: nil}, nil
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
		ticker := time.NewTicker(time.Second * 1)

		for {
			select {
			case <-ticker.C:

				unstructuredItems, err := jq.DiscoverClusterGRVs(context.TODO(), discoveryClient, dynamicClient, requiredLabel)
				if err != nil {
					logrus.Errorf("error discovering cluster resources: %v", err)
					return
				}

				for _, item := range unstructuredItems {
					item := item
					go func() {
						if item.GetName() != "" && item.GetNamespace() != "" {
							// Add the unstructured item to the resources map
							if _, ok := _map.RetrieveResourceFromMap(item.GetNamespace()+item.GetName(), &resources); !ok {
								resources.Store(item.GetNamespace()+"/"+item.GetName(), item)
							}

							err = controller.Watch(
								&source.Kind{Type: &item},
								&handler.EnqueueRequestForObject{},
								pred)
							if err != nil {
								return
							}
						}
					}()
				}
			}
		}
	}()
	return nil
}
