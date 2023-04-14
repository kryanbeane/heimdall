#!/bin/bash

kubectl delete -f 'https://strimzi.io/install/latest?namespace=heimdall' -n heimdall

kubectl delete -A MutatingWebhookConfiguration heimdall-webhook

kubectl delete Deployment heimdall -n heimdall

kubectl delete Deployment heimdall-admission-controller -n heimdall

kubectl delete ns heimdall