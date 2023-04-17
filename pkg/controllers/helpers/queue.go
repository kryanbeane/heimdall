package helpers

import (
	"context"
	"encoding/json"
	"fmt"
	kafka "github.com/Shopify/sarama"
	"github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"strings"
)

var (
	heimdallTopic    = "heimdall-topic"
	kafkaClusterName = "heimdall-kafka-cluster"
	namespace        = "heimdall"
)

type ResourceDetails struct {
	Name      string
	Namespace string
	Kind      string
	Group     string
	Version   string
}

func InitializeKafkaConsumption() error {
	// Get Kafka broker list
	brokerList, err := getBrokerList(namespace, kafkaClusterName)
	if err != nil {
		logrus.Errorf("failed to get broker list: %v", err)
		return err
	}

	logrus.Infof("retrieved Kafka broker address %s", brokerList)
	err = ConsumeKafkaMessages(brokerList, heimdallTopic)
	if err != nil {
		return err
	}

	return nil
}

func ConsumeKafkaMessages(brokerList []string, topic string) error {
	consumerConfig := kafka.NewConfig()
	consumerConfig.Consumer.Return.Errors = true

	// Connect to Kafka broker
	consumer, err := kafka.NewConsumer(brokerList, consumerConfig)
	if err != nil {
		return err
	}
	defer consumer.Close()

	err = createKafkaTopic(*consumerConfig, brokerList)
	if err != nil {
		return err
	}

	// Subscribe to Kafka topic
	partition, err := consumer.ConsumePartition(topic, 0, kafka.OffsetOldest)
	if err != nil {
		return err
	}

	// Consume Kafka messages
	for {
		select {
		case msg := <-partition.Messages():
			resourceDetails, err := deconstructMessage(string(msg.Value))
			if err != nil {
				return err
			}

			//TODO Deal with new resource details - queue it for reconcile
			logrus.Infof("Received message: %v", resourceDetails)

			fmt.Printf("Received message: %v\n", string(msg.Value))
		case err := <-partition.Errors():
			fmt.Printf("Error while consuming message: %v\n", err)
		}
	}
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
		return nil, err
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
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
