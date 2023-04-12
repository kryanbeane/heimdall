#!/bin/bash

# Prompt the user for an input string
read -p "Enter the Slack token: " input_string

# Trim the input string and base64-encode it
trimmed_string=$(echo -n "$input_string" | tr -d '[:space:]')
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
