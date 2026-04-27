#!/usr/bin/bash

# Helper script to install Go dev tools.
# This is run _inside_ the manylinux container(s)
# used in cibuildwheel to build the wheels.

set -euo pipefail

ARCH=$(uname -m)
case "${ARCH}" in
    aarch64)
        ARCH="arm64"
        GO_DIST_SHA256='38ac33b4cfa41e8a32132de7a87c6db49277ab5c0de1412512484db1ed77637e'
        ;;
    x86_64)
        ARCH="amd64"
        GO_DIST_SHA256='6842c516ca66c89d648a7f1dbe28e28c47b61b59f8f06633eb2ceb1188e9251d'
        ;;
    *)
        >&2 echo "Architecture ${ARCH} not supported by this script"
        exit 1
        ;;
esac

TDIR="$(mktemp -d)"
>&2 echo "Working dir: ${TDIR}"
trap "rm -rf ${TDIR}" EXIT

>&2 echo "Downloading Go 1.24.8 distribution file."
curl -fL -o "${TDIR}/go1.24.8.linux-${ARCH}.tar.gz" "https://go.dev/dl/go1.24.8.linux-${ARCH}.tar.gz"

>&2 echo "Checking distribution file integrity"
printf '%s %s/go1.24.8.linux-%s.tar.gz\n' "${GO_DIST_SHA256}" "${TDIR}" "${ARCH}" | sha256sum -c

>&2 echo "Unpacking to /usr/local/go"
rm -rf /usr/local/go && tar -C /usr/local -xzf "${TDIR}/go1.24.8.linux-${ARCH}.tar.gz"

>&2 echo "Installed Go version:"
/usr/local/go/bin/go version
