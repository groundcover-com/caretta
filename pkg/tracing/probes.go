package tracing

import (
	"errors"
	"fmt"
	"log"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
)

type TracerEbpfObjects struct {
	Kprobe     *link.Link
	Tracepoint *link.Link
	BpfObjs    *bpfObjects
}

func LoadProbes() (TracerEbpfObjects, error) {
	objs := bpfObjects{}
	err := loadBpfObjects(&objs, &ebpf.CollectionOptions{})
	if err != nil {
		var ve *ebpf.VerifierError
		if errors.As(err, &ve) {
			fmt.Printf("Verifier Error: %+v\n", ve)
		}
		return TracerEbpfObjects{}, errors.New(fmt.Sprintf("Error loading BPF objects from go-side. %v", err))
	}
	log.Printf("BPF objects loaded")

	// attach a kprobe and tracepoint
	kp, err := link.Kprobe("tcp_data_queue", objs.bpfPrograms.HandleTcpDataQueue, nil)
	if err != nil {
		return TracerEbpfObjects{}, errors.New(fmt.Sprintf("Error attaching kprobe: %v", err))
	}
	log.Printf("Kprobe attached successfully")

	tp, err := link.Tracepoint("sock", "inet_sock_set_state", objs.bpfPrograms.HandleSockSetState, nil)
	if err != nil {
		return TracerEbpfObjects{}, errors.New(fmt.Sprintf("Error attaching tracepoint: %v", err))
	}
	log.Printf("Tracepoint attached successfully")

	return TracerEbpfObjects{
		Kprobe:     &kp,
		Tracepoint: &tp,
		BpfObjs:    &objs,
	}, nil
}
