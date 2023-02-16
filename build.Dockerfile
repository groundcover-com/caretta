FROM quay.io/cilium/ebpf-builder:1648566014
ARG TARGETARCH
ARG TARGETPLATFORM
RUN echo "Building for $TARGETARCH"
RUN echo "Building for $TARGETPLATFORM"
WORKDIR /build
COPY . /build/
RUN make ARCH=$TARGETARCH