package helpers

import (
	"context"
	"encoding/json"
	"fmt"
	kafka "github.com/Shopify/sarama"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"strings"
	"time"
)

var (
	kafkaClusterName = "heimdall-kafka-cluster"
	namespace        = "heimdall"
	heimdallTopic    = "heimdall-topic"
)

type ResourceDetails struct {
	MessageID uuid.UUID
	Name      string
	Namespace string
	Kind      string
	Group     string
	Version   string
}

func InitializeKafkaConsumer() ([]string, error) {
	// Get Kafka broker list
	brokerList, err := getBrokerList(namespace, kafkaClusterName)
	if err != nil {
		logrus.Errorf("failed to get broker list: %v", err)
		return nil, err
	}

	return brokerList, nil
}

func ConsumeKafkaMessages(brokerList []string, topic string) error {
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
			logrus.Errorf("Error from consumer: %v", err)
			time.Sleep(5 * time.Second)
		}
	}
}

type ConsumerHandler struct {
	processedMessages map[string]bool
}

func (h *ConsumerHandler) Setup(_ kafka.ConsumerGroupSession) error {
	return nil
}

func (h *ConsumerHandler) Cleanup(_ kafka.ConsumerGroupSession) error {
	return nil
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

		logrus.Infof("Received resource %s/%s from Kafka", resourceDetails.Namespace, resourceDetails.Name)

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

func createKafkaTopic(config kafka.Config, brokerList []string) error {
	admin, err := kafka.NewClusterAdmin(brokerList, &config)
	if err != nil {
		return err
	}
	defer func() { _ = admin.Close() }()

	// Check if topic already exists
	topicMetadata, err := admin.DescribeTopics([]string{heimdallTopic})
	if err == nil && len(topicMetadata) == 1 {
		// Topic already exists
		return nil
	}

	// Create topic
	topicDetails := kafka.TopicDetail{
		NumPartitions:     2,
		ReplicationFactor: 1,
	}
	err = admin.CreateTopic(heimdallTopic, &topicDetails, false)
	if err != nil {
		return err
	}

	return nil
}

func getBrokerList(namespace string, kafkaClusterName string) ([]string, error) {
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
