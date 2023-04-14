#!/bin/bash

kubectl create -f 'https://strimzi.io/install/latest?namespace=heimdall' -n heimdall

kubectl apply -f ./template/kafka.yaml -n heimdall

kubectl wait heimdall/heimdall-cluster --for=condition=Ready --timeout=300s -n kafka

