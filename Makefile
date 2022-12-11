BIN_DIR:=bin
BINARY_PATH:=${BIN_DIR}/caretta
DOCKER_BIN:=docker
BPF2GO_BINARY := ${BIN_DIR}/bpf2go
BPF2GO_VERSION := 0.9.0
REPODIR := $(shell dirname $(realpath $(firstword $(MAKEFILE_LIST))))
UIDGID := $(shell stat -c '%u:%g' ${REPODIR})
PROJECT_DIRNAME := $(shell basename ${REPODIR})
CILIUM_EBPF_DIRECTORY := /tmp/cilium-ebpf
BUILD_SCRIPTS_DIRECTORY=scripts/build
BPF_CLANG := clang-12
INCLUDE_C_FLAGS := -I/tmp/caretta_extra/libbpf_headers -I/tmp/${PROJECT_DIRNAME}/
BPF_CFLAGS := -O2 -g -Wall -Werror -fdebug-prefix-map=/ebpf=. ${INCLUDE_C_FLAGS}
IMAGE=caretta-builder
VERSION=1

.PHONY: build
build: caretta

.PHONY: run
run: build
	sudo ./bin/caretta

.PHONY: download_libbpf_headers
download_libbpf_headers: 
	${REPODIR}/${BUILD_SCRIPTS_DIRECTORY}/download_libbpf_headers.sh
	bpftool btf dump file /sys/kernel/btf/vmlinux format c > /tmp/caretta_extra/libbpf_headers/vmlinux.h

.PHONY: generate_ebpf
generate_ebpf: ${BPF2GO_BINARY}_${BPF2GO_VERSION} \
				download_libbpf_headers
	go mod vendor
	(cd ${REPODIR}/pkg/tracing && \
		GOPACKAGE=tracing ${REPODIR}/${BPF2GO_BINARY}_${BPF2GO_VERSION} \
		-cc "${BPF_CLANG}" -cflags "${BPF_CFLAGS}"  \
		-target native bpf \
		ebpf/caretta.bpf.c --)

${BPF2GO_BINARY}_${BPF2GO_VERSION}:
	git clone -q --branch v${BPF2GO_VERSION} https://github.com/cilium/ebpf \
		${CILIUM_EBPF_DIRECTORY} 2>/dev/null
	cd ${CILIUM_EBPF_DIRECTORY} && \
		go build -o ${REPODIR}/${BPF2GO_BINARY}_${BPF2GO_VERSION} ./cmd/bpf2go

.PHONY: binary_directory
binary_directory:
	mkdir -p ${BIN_DIR}

.PHONY: caretta
caretta: binary_directory generate_ebpf_in_docker cmd/caretta/caretta.go
	go build -o ${BINARY_PATH} cmd/caretta/caretta.go

.PHONY: build_builder_docker
build_builder_docker:
	docker build --tag ${IMAGE}:${VERSION} scripts/build

.PHONY: generate_ebpf_in_docker
generate_ebpf_in_docker: build_builder_docker
	${DOCKER_BIN} run \
		-v ${REPODIR}:/tmp/caretta \
		-w /tmp/${PROJECT_DIRNAME} \
		--env HOME="/tmp/" \
		"${IMAGE}:${VERSION}" \
		${MAKE} generate_ebpf