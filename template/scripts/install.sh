#!/bin/bash

echo "Cloning and installing Heimdall's Admission Controller"

kubectl create ns heimdall

git clone "https://github.com/heimdall-controller/heimdall-admission.git"
./heimdall-admission/deploy.sh
echo "Removing the cloned repo"
rm -rf heimdall-admission
pwd

kubectl create ns heimdall

echo "Installing Heimdall"

kubectl apply -f ./template/heimdall.yaml

./template/scripts/gen-secret.sh
