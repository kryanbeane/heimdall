#!/bin/bash

# install prometheus
helm install prometheus prometheus-community/prometheus -n monitoring

kubectl patch service prometheus-grafana -n monitoring --type='json' -p='[{"op": "replace", "path": "/spec/type", "value": "NodePort"}]'

NODE_PORT=$(kubectl get svc -n monitoring prometheus-grafana -o jsonpath='{.spec.ports[0].nodePort}')
echo "Link to example website http://192.168.58.2:$NODE_PORT"


# apply test resource
kubectl apply -f ./nginx.yaml