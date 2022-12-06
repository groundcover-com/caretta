FROM ubuntu:20.04

ARG DEBIAN_FRONTEND=noninteractive
RUN apt-get update && \
    apt-get install -y build-essential git cmake \
                       zlib1g-dev libevent-dev \
                       libelf-dev llvm \
                       clang libc6-dev-i386 lldb lld \
                       curl

RUN mkdir /src && \
    git init
WORKDIR /src

# Link asm/byteorder.h into eBPF
RUN ln -s /usr/include/x86_64-linux-gnu/asm/ /usr/include/asm

# Build libbpf as a static lib
RUN git clone https://github.com/libbpf/libbpf-bootstrap.git && \
    cd libbpf-bootstrap && \
    git submodule update --init --recursive

RUN cd libbpf-bootstrap/libbpf/src && \
    make BUILD_STATIC_ONLY=y && \
    make install BUILD_STATIC_ONLY=y LIBDIR=/usr/lib/x86_64-linux-gnu/

# Clones the linux kernel repo and use the latest linux kernel source BPF headers 
RUN git clone --depth 1 git://git.kernel.org/pub/scm/linux/kernel/git/stable/linux.git && \
    cp linux/include/uapi/linux/bpf* /usr/include/linux/
RUN apt-get -y --no-install-recommends install make git clang-format clang-7 llvm-7 clang-9 llvm-9 llvm-12

RUN apt-get update && apt-get install -y linux-tools-common linux-tools-aws linux-tools-5.11.0-27-generic linux-tools-5.11.0-1021-aws 

RUN apt-get install -y software-properties-common && add-apt-repository ppa:longsleep/golang-backports
RUN apt-get update && apt-get install -y musl-dev golang-1.19-go clang-12

# Configure Go
ENV GOROOT /usr/lib/go
ENV GOPATH /go
ENV PATH /go/bin:$PATH

RUN mkdir -p ${GOPATH}/src ${GOPATH}/bin
RUN mkdir -p /tmp/caretta_extra/libbpf_headers

RUN bpftool btf dump file /sys/kernel/btf/vmlinux format c > /tmp/caretta_extra/libbpf_headers/vmlinux.h


WORKDIR $GOPATH

