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
kubectl delete -f 'https://strimzi.io/install/latest?namespace=heimdall' -n $NAMESPACE

echo "Uninstalling Admission Controller..."
kubectl delete -A MutatingWebhookConfiguration $WEBHOOK
kubectl delete RoleBinding $ROLE_BINDING -n $NAMESPACE
kubectl delete Role $ROLE -n $NAMESPACE
kubectl delete Deployment $CONTROLLER -n $NAMESPACE
kubectl delete Secret $TLS_SECRET -n $NAMESPACE
kubectl delete Service $SERVICE -n $NAMESPACE

echo "Finalizing Heimdall's removal..."

kubectl delete Deployment $DEPLOYMENT -n $NAMESPACE
kubectl delete ns $NAMESPACE

echo "Heimdall uninstalled, we'll miss you :("