#!/bin/bash -e

if [ "$1" == "--help" ] || [ "$1" == "-h" ]; then
  echo "Usage: $(basename "$1") [--rebuild] | [--destroy [--volumes]] | [command to run in dev container]"
  echo "If no parameters are provided, a bash session is started in the dev container"
  exit 0
fi

# Don't run this inside the docker env container it's supposed to start!
if [ -e /.dockerenv ]; then
  echo "Don't run this script inside a container!"
  exit 1
fi

COMPOSE_FILE="$(dirname "$(realpath "$0")")/../.devcontainer/docker-compose.yml"
# shellcheck source=/dev/null
source "$(dirname "$COMPOSE_FILE")/../.env"
export COMPOSE_PROJECT_NAME
export COMPOSE_FILE
export BUILD_USER=$(id -u)
export BUILD_GROUP=$(id -g)
DEV_CONTAINER="${COMPOSE_PROJECT_NAME}_devenv_1"
DEV_EXEC="docker exec -it --user builder --workdir /home/builder/cloudian-s3-operator $DEV_CONTAINER"

post_create_step() {
  docker cp ~/.gitconfig "$DEV_CONTAINER:/home/builder/.gitconfig"
  $DEV_EXEC ./scripts/k8s.sh --restore
}

if [ "$1" == "--rebuild" ]; then
  docker-compose up -d --build --force-recreate
  post_create_step
  exit 0
fi

if [ "$1" == "--destroy" ]; then
  # shellcheck disable=SC2086
  docker-compose down $2
  exit 0
fi

# Check if dev env is currently running

if [ -z "$(docker ps -q -f name="$DEV_CONTAINER")" ]; then
  # Starting dev environment
  docker-compose up -d --build
  post_create_step
fi

if [ $# -eq 0 ]; then
  # shellcheck disable=SC2086
  exec $DEV_EXEC bash
else
  # shellcheck disable=SC2086
  exec $DEV_EXEC "$@"
fi
