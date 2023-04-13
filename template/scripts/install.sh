#!/bin/bash

echo "Cloning and installing Heimdall's Admission Controller"

git clone "https://github.com/heimdall-controller/heimdall-admission.git"
./heimdall-admission/deploy.sh
echo "Removing the cloned repo"
rm -rf heimdall-admission
pwd

kubectl create ns heimdall

./gen-secret.sh
