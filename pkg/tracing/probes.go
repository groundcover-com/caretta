package tracing

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -target amd64 -cc clang bpf ./ebpf/caretta.bpf.c --  -I./ebpf/headers  -I/usr/src/linux-headers-5.4.0-1045-aws/include/ -I/usr/src/linux-headers-5.4.0-1045-aws/include/uapi  -I.
