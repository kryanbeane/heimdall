package controllers

import (
	"bytes"
	"context"
	"github.com/heimdall-controller/heimdall/pkg/controllers/helpers"
	"github.com/heimdall-controller/heimdall/pkg/slack"
	"github.com/heimdall-controller/heimdall/pkg/slack/provider"
	gcp2 "github.com/heimdall-controller/heimdall/pkg/slack/provider/gcp"
	"github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	u "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"strings"
	"sync"
	"time"
)

type Controller struct {
	Client        client.Client
	Scheme        *runtime.Scheme
	DynamicClient dynamic.Interface
}

// Reconcile Placeholder for now as we don't need a traditional reconcile loop
func (c *Controller) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	return reconcile.Result{}, nil
}

const (
	configMapName = "heimdall-settings"
	namespace     = "heimdall"
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

	ctr, err := controller.New("controller", mgr,
		controller.Options{Reconciler: &Controller{
			Client:        mgr.GetClient(),
			Scheme:        mgr.GetScheme(),
			DynamicClient: dynamicClient,
		}})
	if err != nil {
		logrus.Errorf("failed to create controller: %v", err)
		return err
	}
	logrus.Infof("Initialized Heimdall controller: %s", ctr)

	NewMetrics()

	logrus.Infof("Custom Prometheus Metrics registered")

	// Create a Kubernetes client for low-level work
	clientset, err = kubernetes.NewForConfig(mgr.GetConfig())
	if err != nil {
		return err
	}

	//// Fetch provider id
	//_ = provider.GetProviderId(clientset)

	discoveryClient, err := discovery.NewDiscoveryClientForConfig(mgr.GetConfig())
	if err != nil {
		return err
	}

	if err := c.WatchResources(discoveryClient, dynamicClient, requiredLabel); err != nil {
		return err
	}

	return nil
}

func (c *Controller) resourceReconcile(ctx context.Context, resource *u.Unstructured) (reconcile.Result, error) {
	// THIS FUNC WILL ONLY BE TRIGGERED ON A RESOURCE CHANGE
	//TODO Changes that need to be done here once moved to standalone function
	// Refetch the resource (to get most up to date)
	// Update map with latest version
	// Reconcile label to ensure it should still be watched, if not then delete from map
	// Build notification URL
	// Reconcile notification stuff
	// Reconcile cadence
	logrus.Info("IM A TEST")
	WatchingResourcesCount.Set(float64(helpers.GetMapLength(&resources)))

	res, found := helpers.RetrieveResource(namespacedName(resource), &resources)
	if !found {
		logrus.Errorf("failed to retrieve resource from map: %v", namespacedName(resource))
		return reconcile.Result{}, nil
	}
	gvr := GVRFromUnstructured(res)

	logrus.Infof("reconciling %s with importance of %s", namespacedName(&res), res.GetLabels()[watchingLabel])

	// Update the map with the latest resource version
	helpers.UpdateMapWithResource(res, namespacedName(&res), &resources)

	// If resource no longer has the label or if label is empty (no priority set) then delete it from the map
	if value, labelExists := res.GetLabels()[watchingLabel]; !labelExists || value == "" {
		resources.Delete(namespacedName(&res))
		return reconcile.Result{}, nil
	}

	// Set priority level based on label - if invalid priority, default to low
	priority := strings.ToLower(res.GetLabels()[watchingLabel])
	if priority != "low" && priority != "medium" && priority != "high" {
		logrus.Warnf("invalid priority set: %s, for resource: %s, defaulting to low priority", priority, res.GetName())
		res.GetLabels()[watchingLabel] = "low"

		labels := res.GetLabels()
		labels[watchingLabel] = "low"
		res.SetLabels(labels)
		priority = "low"

		if _, err := c.DynamicClient.Resource(gvr).Namespace(res.GetNamespace()).Update(ctx, &res, metav1.UpdateOptions{}); err != nil {
			return reconcile.Result{}, err
		}
	}

	// Get notification URL
	url := provider.BuildNotificationURL(*clientset,
		gcp2.ResourceInformation{
			Name:      res.GetName(),
			Namespace: res.GetNamespace(),
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

	err = c.ReconcileNotificationCadence(&res, url, priority, cm, secret)
	if err != nil {
		return ctrl.Result{}, nil
	}

	return reconcile.Result{}, nil
}

func namespacedName(resource *u.Unstructured) string {
	return resource.GetNamespace() + "/" + resource.GetName()
}

func sendSlackNotification(name string, resource *u.Unstructured, url string, priority string, secret v1.Secret, configMap v1.ConfigMap) error {
	if err := slack.SendEvent(*resource, url, priority, secret, configMap); err != nil {
		return err
	}
	lastNotificationTimes.Store(name, time.Now())
	return nil
}

func (c *Controller) ReconcileNotificationCadence(resource *u.Unstructured, url string, priority string, configMap v1.ConfigMap, secret v1.Secret) error {
	cadence, err := time.ParseDuration(configMap.Data[priority+"-priority-cadence"])
	if err != nil {
		return err
	}

	lastNotificationTime, loaded := lastNotificationTimes.LoadOrStore(namespacedName(resource), time.Now())
	if loaded {
		if !lastNotificationTime.(time.Time).Add(cadence).Before(time.Now()) {
			logrus.Warnf("skipping notification for %s because the last message was sent less than %s ago", namespacedName(resource), cadence.String())
			return nil
		}
	}

	if err = sendSlackNotification(namespacedName(resource), resource, url, priority, secret, configMap); err != nil {
		return err
	}
	return nil
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

func (c *Controller) WatchResources(discoveryClient *discovery.DiscoveryClient, dynamicClient dynamic.Interface, requiredLabel string) error {
	// Create label selector containing the specified label key with no value (LabelSelectorOpExists checks that key exists)
	_, err := predicate.LabelSelectorPredicate(metav1.LabelSelector{
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
					// If not in the map; add it. if in the map; update it.
					helpers.UpdateMapWithResource(item, namespacedName(&item), &resources)

					item := item
					namespacedName := namespacedName(&item)
					go func() {
						// Set up watch for the item
						watcher, err := dynamicClient.Resource(helpers.GVRFromUnstructured(item)).Watch(context.TODO(), metav1.ListOptions{})
						if err != nil {
							return
						}

						logrus.Infof("Watching Events on Resource: %s", namespacedName)

						for {
							// Iterate over events
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
								// Update the resource in the map
								helpers.UpdateMapWithResource(*unstructuredObj, namespacedName, &resources)

								logrus.Infof("Object %s has been modified", namespacedName)

								if namespacedName != "/" {
									_, err := c.resourceReconcile(context.TODO(), &item)
									if err != nil {
										return
									}

									// Reconcile has completed so we can now delete the resource from the map
									helpers.DeleteResource(namespacedName, &resources)
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
					jqItems, err := helpers.GetResourcesByJq(di, ctx, g, v, r, requiredLabelQuery)
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
