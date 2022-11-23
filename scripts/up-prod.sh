#!/bin/bash -e

field=${1:-green}field
if [ "$field" != "greenfield" ] && [ "$field" != "brownfield" ]; then
  echo "usage: up-prod.sh [green|brown]"
  exit 1
fi

cd "$(dirname "$0")/.."

kubectl apply -f crds/apiextensions-v1/objectbucket.io_objectbuckets.yaml
kubectl apply -f crds/apiextensions-v1/objectbucket.io_objectbucketclaims.yaml
kubectl apply -f examples/owner-secret.yaml
kubectl apply -f "examples/$field/storageclass.yaml"
kubectl apply -f "examples/$field/photo.yaml"
kubectl get pods -w
