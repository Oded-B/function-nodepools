#!/bin/bash

set -e

# This script packages a the function in pushes it in to the **Local** OCI registry.
#  crossplane xpkg push only suopport remote repos, and that is not suitable for local development/testsing
#
#
# Build container image, output to a Tar file
# Detect architecture: use RUNNER_ARCH env var if set, otherwise use uname
if [ -n "$RUNNER_ARCH" ]; then
  if [ "$RUNNER_ARCH" = "ARM64" ]; then
    ARCH="arm64"
  else
    ARCH="amd64"
  fi
else
  ARCH=$(uname -m)
fi
docker build . --output "type=docker,dest=runtime-${ARCH}.tar" --platform linux/${ARCH}
# Add crossplane metadata and re-pack as  .xpkg file
crossplane xpkg build --package-file=${ARCH}.xpkg --package-root=package/ --embed-runtime-image-tarball=runtime-${ARCH}.tar
# Loca to **Local** Docker registry
IMAGE_SHA=$(docker load -i ${ARCH}.xpkg | awk -F\: '{print $NF}')
docker tag $IMAGE_SHA function-nodepools
docker tag $IMAGE_SHA local.local/oded-b/function-nodepools

rm -v runtime-${ARCH}.tar
rm -v ${ARCH}.xpkg
