#!/bin/bash -x

set -o errexit
set -o nounset
set -o pipefail

SCRIPT_ROOT=$(dirname "${BASH_SOURCE}")
BUILD_DIR="${SCRIPT_ROOT}/build"

docker run --rm --privileged multiarch/qemu-user-static:register

if ! [[ -x "${SCRIPT_ROOT}/qemu-arm-static" ]]; then
    curl -sL https://github.com/multiarch/qemu-user-static/releases/download/v2.12.0-1/x86_64_qemu-arm-static.tar.gz | tar xz -C "${SCRIPT_ROOT}"
fi

for docker_arch in amd64 arm32v7; do
  case ${docker_arch} in
    amd64   ) qemu_arch="x86_64" debian_arch="amd64" ;;
    arm32v7 ) qemu_arch="arm" debian_arch="armhf" ;;
    arm64v8 ) qemu_arch="aarch64" ;;
  esac
  mkdir -p "${BUILD_DIR}/${docker_arch}"
  docker_file="${BUILD_DIR}/${docker_arch}/Dockerfile"
  cp "${SCRIPT_ROOT}/Dockerfile.cross" "${docker_file}"
  sed -i "s|__BASEIMAGE_ARCH__|${docker_arch}|g" "${docker_file}"
  sed -i "s|__QEMU_ARCH__|${qemu_arch}|g" "${docker_file}"
  sed -i "s|__DEBIAN_ARCH__|${debian_arch}|g" "${docker_file}"
  if [ ${docker_arch} == 'amd64' ]; then
    sed -i "/__CROSS_/d" "${docker_file}"
  else
    sed -i "s/__CROSS_//g" "${docker_file}"
  fi
  docker build -f "${docker_file}" .
done
