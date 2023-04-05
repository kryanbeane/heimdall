#!/bin/bash

# Check if an input string was provided
if [[ $# -eq 0 ]]; then
    echo "Error: an input string is required."
    exit 1
fi

# Trim the input string and base64-encode it
trimmed_string=$(echo -n "$1" | tr -d '[:space:]')
encoded_string=$(echo -n $trimmed_string | base64 -w 0)

# Create the YAML file with the encoded string
cat <<EOF > ./template/secret.yaml
apiVersion: v1
kind: Secret
metadata:
  name: heimdall-secret
  namespace: heimdall
data:
  slack-token: $encoded_string
type: Opaque
EOF

kubectl apply -f ./template/secret.yaml