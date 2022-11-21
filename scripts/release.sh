#!/bin/bash -e

cd "$(dirname "$0")/.." || exit

push=false
if [ "$1" == "--push" ]; then
  push=true
  shift
fi

tag=${1:-quay.io/cloudian/cloudian-s3-operator:1.0.1}

docker build -t "$tag" .

# If we're running a kind cluster, load the image
# into the cluster so we don't have to download it
if [ -n "$(kubectl config get-contexts | grep "*" | grep "kind-kind")" ]; then
     kind load docker-image "$tag"
fi

# If we're running a microk9s cluster, set a tag and push to the k8s registry
if [ -n "$(kubectl config get-contexts | grep "*" | grep "microk8s")" ]; then
  mk8s_tag="localhost:32000/$(basename "$tag")"
  docker image tag "$tag" "$mk8s_tag"
  # tolerate errors so we don't if the microk8s registry isn't enabled
  set +e
  docker push "$mk8s_tag"
  set -e
fi

if $push; then
  docker push "$tag"
fi
