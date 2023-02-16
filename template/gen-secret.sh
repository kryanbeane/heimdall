#!/bin/bash

# Check if an input string was provided
if [[ $# -eq 0 ]]; then
    echo "Error: an input slack token is required."
    exit 1
fi

# Base64-encode the input string and store it in a variable
encoded_token=$(echo -n "$1" | base64)

# Create the YAML file with the encoded string
cat << EOF > ./template/secret.yaml
apiVersion: v1
kind: Secret
metadata:
  name: heimdall-secret
  namespace: heimdall-controller
type: Opaque
data:
  slack-token: $encoded_token
EOF

kubectl apply -f ./template/secret.yaml