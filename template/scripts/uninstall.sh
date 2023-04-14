#!/bin/bash

echo "Uninstalling Strimzi Operator and Kafka Cluster..."

kubectl delete -f 'https://strimzi.io/install/latest?namespace=heimdall' -n heimdall

echo "Uninstalling admission controller and webhook..."

kubectl delete -A MutatingWebhookConfiguration heimdall-webhook

kubectl delete Deployment heimdall-admission-controller -n heimdall

echo "Finalizing Heimdall's uninstall..."

kubectl delete Deployment heimdall -n heimdall

kubectl delete ns heimdall

echo "Heimdall uninstalled, we'll miss you :("