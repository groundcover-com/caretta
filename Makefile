.PHONY: build
build: gen caretta

.PHONY: run
run: build
	sudo ./out/caretta
	

.PHONY: gen
gen: sum bpf_bpfel_x86.go

.PHONY: sum
sum: go.sum

caretta: cmd/caretta/caretta.go bpf_bpfel_x86.go
	CGO_ENABLED=0 go build -o ./out/caretta cmd/caretta/caretta.go



bpf_bpfel%.go: pkg/tracing/ebpf/caretta.bpf.c
	go generate pkg/tracing/probes.go

go.sum:
	go mod download github.com/cilium/ebpf
	go get github.com/cilium/ebpf/internal/unix

