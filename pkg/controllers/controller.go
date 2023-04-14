package controllers

import (
	"bytes"
	"context"
	"fmt"
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
	"net"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Controller struct {
	Client client.Client
	Scheme *runtime.Scheme
}

// Reconcile Placeholder for now as we don't need a traditional reconcile loop
func (c *Controller) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	return reconcile.Result{}, nil
}

const (
	configMapName = "heimdall-settings"
	namespace     = "heimdall"
	priorityLabel = "app.heimdall.io/priority"
	ownerLabel    = "app.heimdall.io/owner"
	secretName    = "heimdall-secret"
)

var settingsMap = v1.ConfigMap{
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

var resourceMap = v1.ConfigMap{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "heimdall-resource-map",
		Namespace: namespace,
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
var dc *discovery.DiscoveryClient
var dynamicClient dynamic.Interface
var resources = sync.Map{}
var lastNotificationTimes = sync.Map{}

// InitializeController Add +kubebuilder:rbac:groups=*,resources=*,verbs=get;list;watch
func (c *Controller) InitializeController(mgr manager.Manager, requiredLabel string) error {

	ctr, err := controller.New("controller", mgr,
		controller.Options{Reconciler: &Controller{
			Client: mgr.GetClient(),
			Scheme: mgr.GetScheme(),
		}})
	if err != nil {
		logrus.Errorf("failed to create controller: %v", err)
		return err
	}
	logrus.Infof("Initialized Heimdall controller: %s", ctr)

	NewMetrics()

	logrus.Infof("Prometheus Metrics registered")

	// Create a Kubernetes client for low-level work
	clientset, err = kubernetes.NewForConfig(mgr.GetConfig())
	if err != nil {
		return err
	}

	dc, err = discovery.NewDiscoveryClientForConfig(mgr.GetConfig())
	if err != nil {
		return err
	}

	dynamicClient, err = dynamic.NewForConfig(mgr.GetConfig())
	if err != nil {
		return err
	}

	if err := c.WatchResources(dc, dynamicClient, requiredLabel); err != nil {
		return err
	}

	return nil
}

func (c *Controller) resourceReconcile(ctx context.Context, resource *u.Unstructured) (reconcile.Result, error) {
	WatchingResourcesCount.Set(float64(helpers.GetMapLength(&resources)))

	res, found := helpers.RetrieveResource(namespacedName(resource), &resources)
	if !found {
		logrus.Errorf("failed to retrieve resource from map: %v", namespacedName(resource))
		return reconcile.Result{}, nil
	}

	logrus.Infof("Reconciling labels for %s", namespacedName(resource))
	priority, err := c.ReconcileLabels(ctx, res)
	if err != nil {
		return reconcile.Result{}, err
	}

	// We know priority label is configured so we can log that reconcile is occurring
	logrus.Infof("reconciling %s with importance of %s", namespacedName(&res), res.GetLabels()[priorityLabel])

	// Update the map with the latest resource version
	helpers.UpdateMapWithResource(res, namespacedName(&res), &resources)

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

func parseNamespacedName(namespacedName string) (string, string, error) {
	parts := strings.Split(namespacedName, ".")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid namespaced name: %s", namespacedName)
	}
	return parts[0], parts[1], nil
}

func sendSlackNotification(name string, resource *u.Unstructured, url string, priority string, secret v1.Secret, configMap v1.ConfigMap) error {
	if err := slack.SendEvent(*resource, url, priority, secret, configMap); err != nil {
		return err
	}
	lastNotificationTimes.Store(name, time.Now())
	return nil
}

func (c *Controller) ReconcileLabels(ctx context.Context, resource u.Unstructured) (string, error) {
	gvr := helpers.GVRFromUnstructured(resource)

	// If resource no longer has the label or if label is empty (no priority set) then delete it from the map
	if value, labelExists := resource.GetLabels()[priorityLabel]; !labelExists || value == "" {
		resources.Delete(namespacedName(&resource))
		return "", nil
	}

	// Set priority level based on label - if invalid priority, default to low
	priority := strings.ToLower(resource.GetLabels()[priorityLabel])
	if priority != "low" && priority != "medium" && priority != "high" {
		logrus.Warnf("invalid priority set: %s, for resource: %s, defaulting to low priority", priority, resource.GetName())
		resource.GetLabels()[priorityLabel] = "low"

		labels := resource.GetLabels()
		labels[priorityLabel] = "low"
		resource.SetLabels(labels)
		priority = "low"

		if _, err := dynamicClient.Resource(gvr).Namespace(resource.GetNamespace()).Update(ctx, &resource, metav1.UpdateOptions{}); err != nil {
			return "", err
		}
		return priority, nil
	}

	owner := resource.GetLabels()[ownerLabel]
	// check if owner value is in format of ip address
	if net.ParseIP(owner) != nil {
		return priority, nil
	}
	ownerNamespace, ownerName, err := parseNamespacedName(owner)
	if err != nil {
		logrus.Errorf("failed to parse namespaced name: %v", err)
		return "", err
	}

	if owner != "" {
		// Get owner resource by name and namespace
		ownerResources, err := helpers.DiscoverClusterGRVsByNamespacedName(dc, dynamicClient, ownerNamespace, ownerName)
		if err != nil {
			logrus.Errorf("failed to discover owner resource: %v", err)
			return "", err
		}
		if len(ownerResources) == 0 {
			logrus.Errorf("owner resource not found")
			return "", fmt.Errorf("owner resource not found")
		}
		ownerResource := ownerResources[0]

		// Extract IP from owner
		ownerIP := ownerResource.Object["status"].(map[string]interface{})["podIP"].(string)
		logrus.Infof("owner ip: %s", ownerIP)

		// Set the IP to be the label's value
		labels := resource.GetLabels()
		labels[ownerLabel] = ownerIP
		resource.SetLabels(labels)

		// Update resource with new label
		if _, err := dynamicClient.Resource(gvr).Namespace(resource.GetNamespace()).Update(ctx, &resource, metav1.UpdateOptions{}); err != nil {
			logrus.Errorf("failed to update resource: %v", err)
			return "", err
		}
	}

	return priority, nil
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

	if err := c.Client.Get(ctx, client.ObjectKeyFromObject(&settingsMap), &cm); err != nil {
		if errors.IsNotFound(err) {
			if err := c.Client.Create(ctx, &settingsMap); err != nil {
				return v1.ConfigMap{}, err
			}
			// Successful creation
			return settingsMap, nil
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
	ctx := context.TODO()
	// Get config maps from cluster - they should exist due to install script but just incase
	if err := c.Client.Get(ctx, client.ObjectKeyFromObject(&settingsMap), &settingsMap); err != nil {
		if errors.IsNotFound(err) {
			if err := c.Client.Create(context.TODO(), &settingsMap); err != nil {
				return err
			}
		} else {
			return err
		}
	}

	if err := c.Client.Get(ctx, client.ObjectKeyFromObject(&resourceMap), &resourceMap); err != nil {
		if errors.IsNotFound(err) {
			if err := c.Client.Create(ctx, &resourceMap); err != nil {
				return err
			}
		} else {
			return err
		}
	}

	// Allow configurable cadence for resource fetching
	fetchCadence := strings.Replace(settingsMap.Data["fetch-cadence"], "s", "", -1)
	cadenceTime, _ := strconv.Atoi(fetchCadence)

	go func() {
		ticker := time.NewTicker(time.Second * time.Duration(cadenceTime))

		for {
			select {
			case <-ticker.C:
				unstructuredItems, err := helpers.DiscoverClusterGRVsByLabel(discoveryClient, dynamicClient, requiredLabel)
				if err != nil {
					logrus.Errorf("error discovering cluster resources: %v", err)
					return
				}
				for _, item := range unstructuredItems {
					//TODO we no lnger need to watch resources since changes are being blocked
					// insetad, we want to have a config map containing all of the resources being monitored

					//TODO If the resource doesn't already exist in the config map then add it and trigger a reconcile
					// this is because we want to trigger the controller's functionality to reconcile stuff
					// we can ensure no message is sent here by setting the last notification time to now
					// this initalizes a new resource in the maps

					//TODO if it already exists, no need to trigger reconcile just keep polling for changes in kafka

					c.updateMapAndWatchResource(dynamicClient, item)
				}
			}
		}
	}()
	return nil
}

func (c *Controller) updateMapAndWatchResource(dynamicClient dynamic.Interface, item u.Unstructured) {
	// If not in the map; add it. if in the map; update it.
	helpers.UpdateMapWithResource(item, namespacedName(&item), &resources)

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
				c.handleModifiedEvent(event, item, namespacedName)
			}
		}
	}()
}

func (c *Controller) handleModifiedEvent(event watch.Event, item u.Unstructured, namespacedName string) {
	unstructuredObj, ok := event.Object.(*u.Unstructured)
	if !ok {
		logrus.Error("error converting object to *unstructured.Unstructured")
		return
	}
	// Update the resource in the map
	helpers.UpdateMapWithResource(*unstructuredObj, namespacedName, &resources)

	logrus.Infof("Object %s has been modified", namespacedName)

	if namespacedName != "/" {
		c.triggerResourceReconcile(item, namespacedName)
	}
}

func (c *Controller) triggerResourceReconcile(item u.Unstructured, namespacedName string) {
	logrus.Warnf("Triggering Reconcile for %s", namespacedName)
	_, err := c.resourceReconcile(context.TODO(), &item)
	if err != nil {
		return
	}

	// Reconcile has completed so we can now delete the resource from the map indicating that it does not need a reconcile
	helpers.DeleteResource(namespacedName, &resources)
}
