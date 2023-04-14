#!/bin/bash

NAMESPACE="heimdall"
KAFKA_YAML="./template/kafka.yaml"

echo "Creating Strimzi Operator and Kafka Cluster in namespace $NAMESPACE..."
if ! kubectl create -f "https://strimzi.io/install/latest?namespace=$NAMESPACE" -n "$NAMESPACE" 2>&1 | grep -q "already exists"; then
    echo "Error creating Strimzi Operator" >&2
    exit 1
fi

if ! kubectl apply -f "$KAFKA_YAML" -n "$NAMESPACE"; then
    echo "Error creating Kafka Cluster" >&2
    exit 1
fi

echo "Waiting for Kafka Cluster to be ready..."
if ! kubectl wait kafka/heimdall-kafka-cluster --for=condition=Ready --timeout=300s -n "$NAMESPACE"; then
    echo "Error waiting for Kafka Cluster to be ready" >&2
    exit 1
fi

echo "Strimzi Operator and Kafka Cluster initialized in namespace $NAMESPACE"