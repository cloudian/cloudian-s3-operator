#!/bin/bash

keep=false
if [ "$1" == "--keep" ]; then
  keep=true
  shift
fi

field=${1:-green}field
if [ "$field" != "greenfield" ] && [ "$field" != "brownfield" ]; then
  echo "usage: down-prod.sh [green|brown]"
  exit 1
fi

cd "$(dirname "$0")/.." || exit

kubectl delete -f "examples/$field/photo.yaml"
kubectl delete -f "examples/$field/storageclass.yaml"

if $keep; then
  exit 0
fi

kubectl delete -f examples/owner-secret.yaml
kubectl delete -f examples/cloudian-s3-provisioner.yaml
kubectl delete -f crds/apiextensions-v1/objectbucket.io_objectbuckets.yaml
kubectl delete -f crds/apiextensions-v1/objectbucket.io_objectbucketclaims.yaml
