#!/usr/bin/env bash

# This downloads the libbpf headers we need to compile eBPF code.
# The script is based on cilium's update headers script,
# https://github.com/cilium/ebpf/blob/4420605496c54a45653a7f1d277896e71e6705e2/examples/headers/update.sh#L1

# Version of libbpf to fetch headers from
LIBBPF_VERSION=0.6.1

# Version of cilium ebpf repository to fetch vmlinux from
CILIUM_VMLINUX_VERSION=0.9.0

HEADERS_DIRECTORY="/tmp/caretta_extra/libbpf_headers"

# The headers we want
prefix=libbpf-"$LIBBPF_VERSION"
headers=(
    "$prefix"/src/bpf_endian.h
    "$prefix"/src/bpf_helper_defs.h
    "$prefix"/src/bpf_helpers.h
    "$prefix"/src/bpf_tracing.h
    "$prefix"/src/bpf_core_read.h
)

if [ ! -d "pkg" ] ; then
    echo "Run this scripts from the repository's root directory." 1>&2
    exit 1
fi

if [ ! -d "$HEADERS_DIRECTORY" ]; then
    mkdir -p "$HEADERS_DIRECTORY"
    if [ "$?" -ne 0 ]; then
        echo "Failed to create libbpf headers directory \""$HEADERS_DIRECTORY"\"." 1>&2
        exit 1
    fi
fi

# Fetch libbpf release and extract the desired headers
curl -sL --connect-timeout 10 --max-time 10 \
    "https://github.com/libbpf/libbpf/archive/refs/tags/v${LIBBPF_VERSION}.tar.gz" | \
    tar -xz --xform='s#.*/##' -C "$HEADERS_DIRECTORY" "${headers[@]}"
if [ "$?" -ne 0 ]; then
    echo "Failed to download and extract the needed libbpf headers." 1>&2
    exit 1
fi

# # Fetch compact vmlinux file from cilium's ebpf repository.
# # This is not a libbpf header per-se, but it's close enough that we put it in the same location.
# curl -s -o "$HEADERS_DIRECTORY"/vmlinux.h \
#     https://raw.githubusercontent.com/cilium/ebpf/v${CILIUM_VMLINUX_VERSION}/examples/headers/common.h
# if [ "$?" -ne 0 ]; then
#     echo "Failed to download vmlinux compact version from cilium's repository."
#     exit 1
# fi

echo "Successfully downloaded libbpf headers." 1>&2