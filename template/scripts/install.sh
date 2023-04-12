#!/bin/bash

echo "Cloning and installing Heimdall's Admission Controller"

git clone "https://github.com/heimdall-controller/heimdall-admission.git"
./heimdall-admission/deploy.sh
rm -rf heimdall-admission

kubectl create ns heimdall

./template/scripts/gen-secret.sh

