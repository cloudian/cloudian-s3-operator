#!/bin/bash

cd "$(dirname "$0")/.." || exit

keep=false
if [ "$1" == "--keep" ]; then
  keep=true
  shift
fi


FIELD=${1:-green}field
if [ "$FIELD" != "greenfield" ] && [ "$FIELD" != "brownfield" ]; then
  echo "usage: down.sh [--keep] [green|brown]"
  exit 1
fi

# Clean up app
kubectl delete -f "examples/$FIELD/photo.yaml"
kubectl delete -f "examples/$FIELD/storageclass.yaml"

if $keep; then
  exit 0
fi

# Kill provisioner
ps auxf | grep -alsologtostderr | grep -v grep | grep -v docker | grep -v down.sh | awk '{print $2}' | xargs --no-run-if-empty kill

# Undo object setup
kubectl delete -f examples/owner-secret.yaml
kubectl delete -f examples/cloudian-s3-provisioner-dev.yaml
kubectl delete -f crds/apiextensions-v1/objectbucket.io_objectbuckets.yaml
kubectl delete -f crds/apiextensions-v1/objectbucket.io_objectbucketclaims.yaml
