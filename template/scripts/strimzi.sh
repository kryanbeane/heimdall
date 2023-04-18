#!/bin/bash

NAMESPACE="heimdall"
KAFKA_YAML="./template/kafka.yaml"

echo "Checking if Strimzi Operator already exists..."
if kubectl get deployment strimzi-cluster-operator -n "$NAMESPACE" >/dev/null 2>&1; then
    echo "Strimzi Operator already exists in namespace $NAMESPACE"
else
    echo "Creating Strimzi Operator in namespace $NAMESPACE..."
    if ! kubectl create -f "https://strimzi.io/install/latest?namespace=$NAMESPACE" -n "$NAMESPACE"; then
        echo "Error creating Strimzi Operator" >&2
        exit 1
    fi
fi

echo "Checking if Kafka Cluster already exists..."
if kubectl get kafka heimdall-kafka-cluster -n "$NAMESPACE" >/dev/null 2>&1; then
    echo "Kafka Cluster already exists in namespace $NAMESPACE"
else
    echo "Creating Kafka Cluster in namespace $NAMESPACE..."
    if ! kubectl apply -f "$KAFKA_YAML" -n "$NAMESPACE"; then
        echo "Error creating Kafka Cluster" >&2
        exit 1
    fi

    echo "Waiting for Kafka Cluster to be ready..."
    kubectl wait kafka/heimdall-kafka-cluster --for=condition=Ready --timeout=300s -n "$NAMESPACE"
fi

echo "Strimzi Operator and Kafka Cluster initialized in namespace $NAMESPACE"
