#!/bin/bash

# Prompt the user for the Slack token
while true; do
  echo -e "\n\033[1mEnter the Slack token:\033[0m"
  read input_string
  # Trim the input string and base64-encode it
  trimmed_string=$(echo -n "$input_string" | tr -d '[:space:]')
  export encoded_string=$(echo -n $trimmed_string | base64 -w 0)

  # Check if the input starts with "xoxb"
  if [[ $input_string == xoxb* ]]; then
    break
  else
    echo -e "\n\033[1mInvalid input. Please enter a Slack token that begins with 'xoxb'.\033[0m"
  fi
done

# Prompt the user for the low-priority notification cadence time
while true; do
  echo -e "\n\033[1mEnter notification cadence for LOW priority (use format 30s):\033[0m"
  read low_cadence

  # Check if the input matches the format of "<number>s"
  if [[ $low_cadence =~ ^[0-9]+s$ ]]; then
    break
  else
    echo -e "\n\033[1mInvalid input. Please enter the notification cadence in the format of '<number>s'.\033[0m"
  fi
done
export low=$low_cadence

# Prompt the user for the medium-priority notification cadence time
while true; do
  echo -e "\n\033[1mEnter notification cadence for MEDIUM priority (use format 20s):\033[0m"
  read medium_cadence

  # Check if the input matches the format of "<number>s"
  if [[ $medium_cadence =~ ^[0-9]+s$ ]]; then
    break
  else
    echo -e "\n\033[1mInvalid input. Please enter the notification cadence in the format of '<number>s'.\033[0m"
  fi
done
export medium=$medium_cadence

# Prompt the user for the high-priority notification cadence time
while true; do
  echo -e "\n\033[1mEnter notification cadence for HIGH priority (use format 10s):\033[0m"
  read high_cadence

  # Check if the input matches the format of "<number>s"
  if [[ $high_cadence =~ ^[0-9]+s$ ]]; then
    break
  else
    echo -e "\n\033[1mInvalid input. Please enter the notification cadence in the format of '<number>s'.\033[0m"
  fi
done
export high=$high_cadence

echo -e "\n\033[1mEnter your Slack channel name:\033[0m"
read slack_channel

if [ -z "$slack_channel" ]; then
  slack_channel="default-heimdall-channel"
fi

export slack=$slack_channel

echo -e "\n\033[1mCreating Config Maps and Secret...\033[0m"

# Create the YAML file with the encoded string
cat <<EOF > ../secret.yaml
apiVersion: v1
kind: Secret
metadata:
  name: heimdall-secret
  namespace: heimdall
data:
  slack-token: $encoded_string
type: Opaque
EOF

cat  <<EOF > ../config-maps.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: heimdall-settings
  namespace: heimdall
data:
  slack-channel: "$slack"
  low-priority-cadence: "$low"
  medium-priority-cadence: "$medium"
  high-priority-cadence: "$high"
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: heimdall-resources
  namespace: heimdall
EOF

kubectl apply -f ../secret.yaml

kubectl apply -f ../config-maps.yaml
