package helpers

import (
	"encoding/json"
	"fmt"
	kafka "github.com/Shopify/sarama"
	"github.com/sirupsen/logrus"
)

var (
	heimdallTopic = "heimdall-topic"
)

type ResourceDetails struct {
	Name      string
	Namespace string
	Kind      string
	Group     string
	Version   string
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

	err = CreateKafkaTopic(*consumerConfig, brokerList)
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

func CreateKafkaTopic(config kafka.Config, brokerList []string) error {
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
