#!/bin/bash

set -e

# This script packages a the function in pushes it in to the **Local** OCI registry.
#  crossplane xpkg push only suopport remote repos, and that is not suitable for local development/testsing
#
#
# Build container image, output to a Tar file
ARCH=$(uname -m)
docker build . --output "type=docker,dest=runtime-${ARCH}.tar" --platform linux/${ARCH}
# Add crossplane metadata and re-pack as  .xpkg file
crossplane xpkg build --package-file=${ARCH}.xpkg --package-root=package/ --embed-runtime-image-tarball=runtime-${ARCH}.tar
# Loca to **Local** Docker registry
IMAGE_SHA=$(docker load -i ${ARCH}.xpkg | awk -F\: '{print $NF}')
docker tag $IMAGE_SHA function-nodepools
docker tag $IMAGE_SHA local.local/oded-b/function-nodepools

rm -v runtime-${ARCH}.tar
rm -v ${ARCH}.xpkg
