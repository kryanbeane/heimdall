#!/bin/bash

set -e

# variables
NAMESPACE="heimdall"
WEBHOOK="heimdall-webhook"
ROLE_BINDING="admission-role-binding"
ROLE="admission-role"
CONTROLLER="heimdall-admission-controller"
TLS_SECRET="heimdall-admission-controller-tls"
SERVICE="heimdall-admission-controller"
DEPLOYMENT="heimdall"

echo "Uninstalling Strimzi Operator and Kafka Cluster..."
kubectl delete -f 'https://strimzi.io/install/latest?namespace=heimdall' -n $NAMESPACE --ignore-not-found=true

echo "Uninstalling Admission Controller..."
kubectl delete -A MutatingWebhookConfiguration $WEBHOOK --ignore-not-found=true
kubectl delete RoleBinding $ROLE_BINDING -n $NAMESPACE --ignore-not-found=true
kubectl delete Role $ROLE -n $NAMESPACE --ignore-not-found=true
kubectl delete Deployment $CONTROLLER -n $NAMESPACE --ignore-not-found=true
kubectl delete Secret $TLS_SECRET -n $NAMESPACE --ignore-not-found=true
kubectl delete Service $SERVICE -n $NAMESPACE --ignore-not-found=true

echo "Finalizing Heimdall's removal..."

make undeploy IMG=kryanbeane/heimdall:latest

echo "Heimdall uninstalled, we'll miss you :("
