package controllers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	kafka "github.com/Shopify/sarama"
	"github.com/google/uuid"
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
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"log"
	"net"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"strings"
	"time"
)

type Controller struct {
	Client client.Client
	Scheme *runtime.Scheme
}

var ctr *Controller

type ResourceDetails struct {
	MessageID uuid.UUID
	Name      string
	Namespace string
	Kind      string
	Group     string
	Version   string
	Resource  string
}

// Reconcile Placeholder for now as we don't need a traditional reconcile loop
func (c *Controller) Reconcile(_ context.Context, _ reconcile.Request) (reconcile.Result, error) {
	return reconcile.Result{}, nil
}

const (
	configMapName    = "heimdall-settings"
	resourceMapName  = "heimdall-resources"
	namespace        = "heimdall"
	priorityLabel    = "app.heimdall.io/priority"
	ownerLabel       = "app.heimdall.io/owner"
	secretName       = "heimdall-secret"
	heimdallTopic    = "heimdall-topic"
	kafkaClusterName = "heimdall-kafka-cluster"
)

var settingsMap = v1.ConfigMap{
	ObjectMeta: metav1.ObjectMeta{
		Name:      configMapName,
		Namespace: namespace,
	},
	Data: map[string]string{
		"slack-channel":           "default-heimdall-channel",
		"low-priority-cadence":    "30s",
		"medium-priority-cadence": "20s",
		"high-priority-cadence":   "10s",
	},
}

var resourceMap = v1.ConfigMap{
	ObjectMeta: metav1.ObjectMeta{
		Name:      resourceMapName,
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

	logrus.Infof("Initialized Heimdall controller %s", ctr)

	NewMetrics()

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

func (c *Controller) ResourceReconcile(ctx context.Context, resource *u.Unstructured) (reconcile.Result, error) {
	//WatchingResourcesCount.Set(float64(helpers.GetMapLength(&resources)))
	// We know priority label is configured so we can log that reconcile is occurring
	logrus.Infof("Reconciling %s with importance of %s", namespacedName(resource), resource.GetLabels()[priorityLabel])

	logrus.Infof("Reconciling labels for %s", namespacedName(resource))
	priority, err := ctr.reconcileLabels(ctx, *resource)
	if err != nil {
		return reconcile.Result{}, err
	}

	// Get notification URL
	url := provider.BuildNotificationURL(*clientset,
		gcp2.ResourceInformation{
			Name:      resource.GetName(),
			Namespace: resource.GetNamespace(),
			NodeName:  "",
			Kind:      resource.GetKind(),
		},
	)
	logrus.Infof("Notification URL created: %s", url)

	logrus.Infof("Reconciling Settings Config Map: %s", configMapName)
	settings, err := c.reconcileSettingsConfigMap(ctx)
	if err != nil {
		return reconcile.Result{}, err
	}
	if settings.Data == nil {
		logrus.Warn("settings config map not configured, please configure Heimdall config map")
		return reconcile.Result{}, nil
	}

	logrus.Infof("Reconciling Resources Config Map: %s", resourceMapName)
	resourceMap, err = c.reconcileResourcesMap(ctx)
	if err != nil {
		return reconcile.Result{}, err
	}
	if settings.Data == nil {
		logrus.Warn("resource config map is empty")
		return reconcile.Result{}, nil
	}

	logrus.Infof("Reconciling Secret: %s", secretName)
	secret, err = c.reconcileSecret(ctx)
	if err != nil {
		return reconcile.Result{}, err
	}
	if secret.Data == nil {
		logrus.Warn("secret not configured, please configure Heimdall secret")
		return reconcile.Result{}, nil
	}

	logrus.Infof("Reconciling Notification Cadence for %s", namespacedName(resource))
	err = c.ReconcileNotificationCadence(resource, url, priority, settings, secret)
	if err != nil {
		return ctrl.Result{}, nil
	}

	return reconcile.Result{}, nil
}

func namespacedName(resource *u.Unstructured) string {
	return resource.GetNamespace() + "/" + resource.GetName()
}

func configMapSafeNamespacedName(resource *u.Unstructured) string {
	return resource.GetNamespace() + "." + resource.GetName()
}
func parseNamespacedName(namespacedName string) (string, string, error) {
	parts := strings.Split(namespacedName, ".")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid namespaced name: %s", namespacedName)
	}
	return parts[0], parts[1], nil
}

func sendSlackNotification(resource *u.Unstructured, url string, priority string, secret v1.Secret, configMap v1.ConfigMap) error {
	if err := slack.SendEvent(*resource, url, priority, secret, configMap); err != nil {
		logrus.Errorf("failed to send slack notification: %v", err)
		return err
	}
	return nil
}

func (c *Controller) reconcileLabels(ctx context.Context, resource u.Unstructured) (string, error) {
	gvr := helpers.GVRFromUnstructured(resource)

	// If resource no longer has the label or if label is empty (no priority set) then delete it from the map
	if value, labelExists := resource.GetLabels()[priorityLabel]; !labelExists || value == "" {
		err := ctr.removeOldResourceFromMap(configMapSafeNamespacedName(&resource), resourceMap)
		if err != nil {
			return "", err
		}
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

func (c *Controller) ReconcileNotificationCadence(resource *u.Unstructured, url string, priority string, settings v1.ConfigMap, secret v1.Secret) error {
	cadence, err := time.ParseDuration(settings.Data[priority+"-priority-cadence"])
	if err != nil {
		return err
	}

	lastNotificationTimeString := resourceMap.Data[configMapSafeNamespacedName(resource)]

	lastNotificationTime, err := time.Parse(time.RFC3339, lastNotificationTimeString)
	if err != nil {
		logrus.Errorf("failed to parse last notification time: %v", err)
		return err
	}

	if !lastNotificationTime.Add(cadence).Before(time.Now()) {
		logrus.Warnf("skipping notification for %s because the last message was sent less than %s ago", namespacedName(resource), cadence.String())
		return nil
	}

	logrus.Infof("New Notification permitted, sending Slack message to Channel %s", settings.Data["slack-channel"])

	if err = sendSlackNotification(resource, url, priority, secret, settings); err != nil {
		return err
	}
	return nil
}

func (c *Controller) reconcileSettingsConfigMap(ctx context.Context) (v1.ConfigMap, error) {
	if err := ctr.Client.Get(ctx, client.ObjectKeyFromObject(&settingsMap), &settingsMap); err != nil {
		if errors.IsNotFound(err) {
			if err := ctr.Client.Create(ctx, &settingsMap); err != nil {

				return v1.ConfigMap{}, err
			}
			// Successful creation
			return settingsMap, nil
		}
		return v1.ConfigMap{}, err
	}

	channelValue := settingsMap.Data["slack-channel"]
	if channelValue == "default-heimdall-channel" || channelValue == "" {
		settingsMap.Data = nil
		return settingsMap, nil
	}

	// Successfully retrieved configmap
	return settingsMap, nil
}

func (c *Controller) reconcileResourcesMap(ctx context.Context) (v1.ConfigMap, error) {
	var cm v1.ConfigMap

	if err := c.Client.Get(ctx, client.ObjectKeyFromObject(&resourceMap), &cm); err != nil {
		if errors.IsNotFound(err) {
			if err := c.Client.Create(ctx, &resourceMap); err != nil {
				return v1.ConfigMap{}, err
			}
			// Successful creation
			return resourceMap, nil
		}
	}

	// Successfully retrieved or created configmap
	return cm, nil
}

func (c *Controller) reconcileSecret(ctx context.Context) (v1.Secret, error) {
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

var _ *discovery.DiscoveryClient
var _ dynamic.Interface

func (c *Controller) WatchResources(discoveryClient *discovery.DiscoveryClient, dynamicClient dynamic.Interface, requiredLabel string) error {
	_ = discoveryClient
	_ = dynamicClient
	grafanaReconciled := false
	go func() {
		ticker := time.NewTicker(time.Second * 10)

		for {
			select {
			case <-ticker.C:
				unstructuredItems, err := helpers.DiscoverClusterGRVsByLabel(discoveryClient, dynamicClient, requiredLabel)
				if err != nil {
					logrus.Errorf("Error discovering cluster Resources: %v, will try again", err)
					continue
				}

				if !grafanaReconciled {

					// get prometheus-grafana service
					grafana := &v1.Service{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "prometheus-grafana",
							Namespace: "monitoring",
						},
					}
					err := c.Client.Get(context.TODO(), client.ObjectKeyFromObject(grafana), grafana)
					if err != nil {
						logrus.Errorf("Error getting Grafana service: %v", err)
						return
					}

					// get ip address from node
					node, err := clientset.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
					if err != nil {
						logrus.Errorf("Error getting node list: %v", err)
						return
					}

					address := node.Items[0].Status.Addresses[0].Address

					grafanaURL := fmt.Sprintf("http://%s:%d", address, grafana.Spec.Ports[0].NodePort)
					logrus.Infof("Grafana URL: %s", grafanaURL)

					// get secret for grafana admin password
					grafanaSecret := &v1.Secret{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "prometheus-grafana",
							Namespace: "monitoring",
						},
					}

					err = c.Client.Get(context.TODO(), client.ObjectKeyFromObject(grafanaSecret), grafanaSecret)
					if err != nil {
						logrus.Errorf("Error getting Grafana secret: %v", err)
						return
					}
					grafanaUser := string(grafanaSecret.Data["admin-user"])
					grafanaPassword := string(grafanaSecret.Data["admin-password"])

					orgID, err := helpers.CreateGrafanaOrg(grafanaURL, grafanaUser, grafanaPassword)
					if err != nil {
						logrus.Errorf("Error creating Grafana org: %v", err)
						return
					}

					err = helpers.AddAdminUserToOrg(grafanaURL, grafanaUser, grafanaPassword, orgID)
					if err != nil {
						logrus.Errorf("Error adding admin user to org: %v", err)
						return
					}

					apiKey, err := helpers.CreateGrafanaAPIToken(grafanaURL, grafanaUser, grafanaPassword)
					if err != nil {
						logrus.Errorf("Error creating Grafana API token: %v", err)
						return
					}

					folderName := "Heimdall"
					folderID, err := helpers.GetOrCreateGrafanaFolder(grafanaURL, grafanaUser, grafanaPassword, folderName)
					if err != nil {
						log.Fatalf("Failed to get or create Grafana folder: %v", err)
					}

					err = helpers.CreateGrafanaDashboard(grafanaURL, apiKey, folderID)
					if err != nil {
						logrus.Errorf("Error creating Grafana dashboard: %v", err)
						return
					}

					grafanaReconciled = true
				}

				ctx := context.TODO()
				//Get config maps from cluster - they should exist due to install script but just in-case
				settingsMap, err = c.reconcileSettingsConfigMap(ctx)
				if err != nil {
					logrus.Errorf("Error reconciling settings Config Map: %v", err)
					continue
				}
				if settingsMap.Data == nil {
					logrus.Warnf("Settings config map not configured, re-run install script to configure Slack")
				}

				resourceMap, err = c.reconcileResourcesMap(ctx)
				if err != nil {
					logrus.Errorf("Error reconciling resource Config Map: %v", err)
					continue
				}

				if err = c.reconcileResourceStatus(unstructuredItems); err != nil {
					logrus.Errorf("Error reconciling resource status: %v", err)
					continue
				}

				for _, item := range unstructuredItems {
					if _, err = c.reconcileLabels(ctx, item); err != nil {
						logrus.Errorf("Error reconciling resource label: %v", err)
						continue
					}
				}
			}
		}
	}()

	brokerList, err := c.initializeKafkaConsumer()
	if err != nil {
		logrus.Errorf("Error fetching Kafka brokers: %v", err)
		return err
	}

	// Set ctr reference so that we don't have a nil controller value during reconcile
	ctr = c

	// create topic and start consuming in go routine
	go func() {
		err := c.consumeKafkaMessages(brokerList, heimdallTopic)
		if err != nil {
			logrus.Errorf("Error consuming Kafka messages: %v", err)
			return
		}
	}()

	return nil
}

func (c *Controller) reconcileResourceStatus(items []u.Unstructured) error {
	// Get the latest version of the ConfigMap
	if err := c.Client.Get(context.TODO(), client.ObjectKeyFromObject(&resourceMap), &resourceMap); err != nil {
		return err
	}
	if resourceMap.Data == nil {
		resourceMap.Data = make(map[string]string)
	}

	// Check and add new resources to the config map
	for _, item := range items {
		itemName := configMapSafeNamespacedName(&item)

		if _, ok := resourceMap.Data[itemName]; !ok {
			logrus.Infof("Resource %s not found in map, adding it", itemName)

			// First change the label to be the ip address
			_, err := c.reconcileLabels(context.TODO(), item)
			if err != nil {
				return err
			}

			// them add it to the map
			if err := c.addNewResourceToMap(item, resourceMap); err != nil {
				return err
			}
		}
	}

	// Check and remove outdated resources from the config map
	for key := range resourceMap.Data {
		var found bool
		for _, item := range items {
			if configMapSafeNamespacedName(&item) == key {
				found = true
				break
			}
		}

		if !found {

			err := c.removeOldResourceFromMap(key, resourceMap)
			if err != nil {
				return err
			}
		}
	}

	// update reference to the config map with newest version
	if err := c.Client.Get(context.TODO(), client.ObjectKeyFromObject(&resourceMap), &resourceMap); err != nil {
		return err
	}

	return nil
}

func (c *Controller) removeOldResourceFromMap(key string, cm v1.ConfigMap) error {
	delete(cm.Data, key)
	logrus.Infof("Removing old resource from map: %s", key)
	if err := c.Client.Update(context.TODO(), &cm); err != nil {
		logrus.Infof("Error updating configmap: %v", err)
		return err
	}
	resourceMap = cm
	return nil
}

func (c *Controller) addNewResourceToMap(item u.Unstructured, cm v1.ConfigMap) error {
	if cm.Data == nil {
		logrus.Infof("ConfigMap Data is nil. Initializing map")
		cm.Data = make(map[string]string)
	}

	cm.Data[configMapSafeNamespacedName(&item)] = time.Now().Format(time.RFC3339)
	logrus.Infof("Adding new resource to map: %s with time of %s", configMapSafeNamespacedName(&item), time.Now().Format(time.RFC3339))
	if err := c.Client.Update(context.TODO(), &cm); err != nil {
		logrus.Infof("Error updating configmap: %v", err)
		return err
	}
	resourceMap = cm
	return nil
}

func (c *Controller) updateMapAndWatchResource(dynamicClient dynamic.Interface, item u.Unstructured) {
	// If not in the map; add it. if in the map; update it.
	//helpers.UpdateMapWithResource(item, namespacedName(&item), &resources)

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
	_, ok := event.Object.(*u.Unstructured)
	if !ok {
		logrus.Error("error converting object to *unstructured.Unstructured")
		return
	}
	// Update the resource in the map
	//helpers.UpdateMapWithResource(*unstructuredObj, namespacedName, &resources)

	logrus.Infof("Object %s has been modified", namespacedName)

	if namespacedName != "/" {
		c.triggerResourceReconcile(item)
	}
}

func (c *Controller) triggerResourceReconcile(item u.Unstructured) {
	_, err := c.ResourceReconcile(context.TODO(), &item)
	if err != nil {
		return
	}

	// Reconcile has completed so we can now delete the resource from the map indicating that it does not need a reconcile
	//helpers.DeleteResource(namespacedName, &resources)
}

func (c *Controller) initializeKafkaConsumer() ([]string, error) {
	// Get Kafka broker list
	brokerList, err := c.getBrokerList(namespace, kafkaClusterName)
	if err != nil {
		logrus.Errorf("failed to get broker list: %v", err)
		return nil, err
	}

	return brokerList, nil
}

func (c *Controller) consumeKafkaMessages(brokerList []string, topic string) error {
	config := kafka.NewConfig()
	config.Consumer.Return.Errors = true
	config.Consumer.Offsets.AutoCommit.Enable = true
	config.Consumer.Offsets.AutoCommit.Interval = 8 * time.Second

	consumer, err := kafka.NewConsumerGroup(brokerList, "kafka-consumer-group", config)
	if err != nil {
		return err
	}
	defer consumer.Close()

	// Track processed messages
	processedMessages := make(map[string]bool)

	handler := ConsumerHandler{
		processedMessages: processedMessages,
	}

	// Start consuming from Kafka topic
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	topics := []string{topic}

	logrus.Infof("Starting Kafka consumer for topic %s", topic)
	// loop indefinitely to keep consuming
	for {
		if err := consumer.Consume(ctx, topics, &handler); err != nil {
			logrus.Errorf("Error from consumer: %v, retrying", err)
			time.Sleep(5 * time.Second)
		}
	}
}
func (c *Controller) triggerReconcile(resourceDetails ResourceDetails) error {

	gvr := schema.GroupVersionResource{
		Group:    resourceDetails.Group,
		Version:  resourceDetails.Version,
		Resource: resourceDetails.Resource,
	}

	resource, err := dynamicClient.Resource(gvr).Namespace(resourceDetails.Namespace).Get(context.TODO(), resourceDetails.Name, metav1.GetOptions{})
	if err != nil {
		logrus.Errorf("Error getting Resource: %s", err)
		return err
	}

	_, err = c.ResourceReconcile(context.TODO(), resource)
	if err != nil {
		logrus.Errorf("Error reconciling Resource: %s", err)
		return err
	}
	return nil
}

type ConsumerHandler struct {
	processedMessages map[string]bool
}

func (h *ConsumerHandler) ConsumeClaim(sess kafka.ConsumerGroupSession, claim kafka.ConsumerGroupClaim) error {
	for msg := range claim.Messages() {
		resourceDetails, err := deconstructMessage(string(msg.Value))
		if err != nil {
			return err
		}

		// Check if message has already been processed
		if h.processedMessages[resourceDetails.MessageID.String()] {
			logrus.Infof("Skipping duplicate message: %v", resourceDetails)
			continue
		}
		logrus.Infof("—————————————————————————————————————————————————————————————————————————————")
		logrus.Infof("Received resource %s/%s from Kafka, queueing it for Reconcile", resourceDetails.Namespace, resourceDetails.Name)

		err = ctr.triggerReconcile(*resourceDetails)
		if err != nil {
			return err
		}

		// Mark message as processed
		h.processedMessages[resourceDetails.MessageID.String()] = true

		// Mark message as consumed
		sess.MarkMessage(msg, "")
	}

	return nil
}

func deconstructMessage(msg string) (*ResourceDetails, error) {
	var resourceDetails ResourceDetails
	err := json.Unmarshal([]byte(msg), &resourceDetails)
	if err != nil {
		return nil, err
	}

	return &resourceDetails, nil
}

func (c *Controller) getBrokerList(namespace string, kafkaClusterName string) ([]string, error) {
	// Create Kubernetes clientset
	config, err := rest.InClusterConfig()
	if err != nil {
		logrus.Errorf("Failed to create in-cluster config: %v", err)
		return nil, err
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		logrus.Errorf("Failed to create clientset: %v", err)
		return nil, err
	}

	// Get list of Kafka broker services
	svcList, err := clientset.CoreV1().Services(namespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: fmt.Sprintf("strimzi.io/cluster=%s,strimzi.io/kind=Kafka", kafkaClusterName),
	})
	if err != nil {
		return nil, err
	}

	// Create list of broker addresses in format "broker-address:broker-port"
	brokerList := make([]string, len(svcList.Items))
	for i, svc := range svcList.Items {
		if svc.Spec.ClusterIP != "None" && strings.Contains(svc.Name, "bootstrap") {
			brokerAddress := fmt.Sprintf("%s:%d", svc.Spec.ClusterIP, 9092)
			brokerAddress = strings.Replace(brokerAddress, " ", "", -1)
			brokerList[i] = brokerAddress
		}
	}

	return brokerList, nil
}

// Setup Necessary for Consumer Groups
func (h *ConsumerHandler) Setup(_ kafka.ConsumerGroupSession) error {
	return nil
}

// Cleanup Necessary for Consumer Groups
func (h *ConsumerHandler) Cleanup(_ kafka.ConsumerGroupSession) error {
	return nil
}
