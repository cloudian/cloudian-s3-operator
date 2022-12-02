#!/bin/bash

# Handle restarting container
sudo rm -f /var/run/docker.pid /var/run/docker/containerd/containerd.pid

echo "Starting dockerd..."
exec sudo dockerd
