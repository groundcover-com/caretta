FROM quay.io/cilium/ebpf-builder:1648566014 AS builder
ARG TARGETARCH
ARG TARGETPLATFORM
RUN echo "Building for $TARGETARCH"
RUN echo "Building for $TARGETPLATFORM"
WORKDIR /build
COPY . /build/
RUN make build ARCH=$TARGETARCH

FROM alpine:3.17

WORKDIR /app
COPY --from=builder build/bin/caretta ./

VOLUME /sys/kernel/debug

CMD ./caretta