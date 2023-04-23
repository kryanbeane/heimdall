#!/bin/bash

NAMESPACE="heimdall"
KAFKA_YAML="./template/kafka.yaml"

echo -e "\n\033[1mChecking if Strimzi Operator already exists...\033[0m"
if kubectl get deployment strimzi-cluster-operator -n "$NAMESPACE" >/dev/null 2>&1; then
    echo -e "\n\033[1mStrimzi Operator already exists in namespace $NAMESPACE \033[0m"
else
    echo -e "\n\033[1mCreating Strimzi Operator in namespace $NAMESPACE...\033[0m"
    if ! kubectl create -f "https://strimzi.io/install/latest?namespace=$NAMESPACE" -n "$NAMESPACE"; then
        echo -e "\n\033[1mError creating Strimzi Operator\033[0m" >&2
        exit 1
    fi
fi

echo -e "\n\033[1mChecking if Kafka Cluster already exists...\033[0m"
if kubectl get kafka heimdall-kafka-cluster -n "$NAMESPACE" >/dev/null 2>&1; then
    echo -e "\n\033[1mKafka Cluster already exists in namespace $NAMESPACE \033[0m"
else
    echo -e "\n\033[1mCreating Kafka Cluster in namespace $NAMESPACE...\033[0m"
    if ! kubectl apply -f "$KAFKA_YAML" -n "$NAMESPACE"; then
        echo -e "\n\033[1mError creating Kafka Cluster\033[0m" >&2
        exit 1
    fi

    echo -e "\n\033[1mWaiting for Kafka Cluster to be ready...\033[0m"
    kubectl wait kafka/heimdall-kafka-cluster --for=condition=Ready --timeout=300s -n "$NAMESPACE"
fi

echo -e "\n\033[1mStrimzi Operator and Kafka Cluster initialized in namespace $NAMESPACE\033[0m"
