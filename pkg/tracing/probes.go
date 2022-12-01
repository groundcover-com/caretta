package tracing

import (
	"errors"
	"fmt"
	"log"

	"github.com/cilium/ebpf"
)

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -target amd64 -cc clang bpf ./ebpf/caretta.bpf.c --  -I./ebpf/headers  -I/usr/src/linux-headers-5.4.0-1045-aws/include/ -I/usr/src/linux-headers-5.4.0-1045-aws/include/uapi  -I.

func loadProbes() {
	objs := bpfObjects{}
	err := loadBpfObjects(&objs, &ebpf.CollectionOptions{})
	if err != nil {
		var ve *ebpf.VerifierError
		if errors.As(err, &ve) {
			fmt.Printf("Verifier Error: %+v\n", ve)
		}
		log.Fatalf("Error loading BPF objects from go-side. %v", err)
	}
	log.Printf("BPF objects loaded")

	// attach a kprobe and tracepoint

}
