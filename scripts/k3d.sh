#!/bin/bash -e

K3D_CLUSTER=s3provisioner
ROOT=$(dirname "$(realpath "$0")")/../

# If we're restarting the dev container the kube node may already be running
if [ "$1" == "--restore" ] && k3d cluster list $K3D_CLUSTER > /dev/null 2>&1; then
  k3d kubeconfig get $K3D_CLUSTER > ~/.kube/config
  exit 0
fi

# Purge and reinstall kind kubernetes cluster
if k3d cluster list $K3D_CLUSTER > /dev/null 2>&1; then
    echo "Destroying existing cluster"
    k3d cluster delete $K3D_CLUSTER
fi

K3D_FIX_DNS=1 k3d cluster create -p "30007:30007@server:0" $K3D_CLUSTER

# Create a launch.json so we can run the provisioner in the vscode debugger
echo "Creating launch.json"

mkdir -p "$ROOT/.vscode"
cat << EOF > "$ROOT/.vscode/launch.json"
{
    // Use IntelliSense to learn about possible attributes.
    // Hover to view descriptions of existing attributes.
    // For more information, visit: https://go.microsoft.com/fwlink/?linkid=830387
    "version": "0.2.0",
    "configurations": [
        {
            "name": "aws s3 provisioner",
            "type": "go",
            "request": "launch",
            "mode": "auto",
            "program": "\${workspaceFolder}/cmd",
            "env": {},
            "args": ["-kubeconfig", "/home/builder/.kube/config", "-alsologtostderr", "-v=2"]
        }
    ]
}
EOF

# Load the photo gallery image into the cluster to avoid downloading if we have it cached
echo "Pre loading photo gallery app"
image="quay.io/cloudian/photo-gallery:v1.0.0"

docker pull "$image"
k3d image import -c "$K3D_CLUSTER" "$image"
