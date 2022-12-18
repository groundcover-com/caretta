FROM quay.io/cilium/ebpf-builder:1648566014
COPY --from=quay.io/cilium/cilium-bpftool /bin/bpftool ./bin