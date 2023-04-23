#!/bin/bash

# In case of exiting, run install script to clean up
cleanup() {
  echo -e "\n\033[1mInstall cancelled, cleaning up...\033[0m"
  sleep 2
  ./template/scripts/uninstall.sh
  exit 0
}

trap cleanup SIGINT

echo -e "\n\033[1mCloning and installing Heimdall's Admission Controller\033[0m"

kubectl create ns heimdall

./template/scripts/strimzi.sh

git clone "https://github.com/heimdall-controller/heimdall-admission.git"
./heimdall-admission/deploy.sh
echo -e "\n\033[1mRemoving the cloned repo\033[0m"
rm -rf heimdall-admission

echo -e "\n\033[1mConfig Map and Secret creation\033[0m"

./template/scripts/gen-configs.sh

echo -e "\n\033[1mInstalling Heimdall\033[0m"

docker pull kryanbeane/heimdall:dev
make deploy IMG=kryanbeane/heimdall:dev

