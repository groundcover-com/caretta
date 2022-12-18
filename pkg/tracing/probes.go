package tracing

import (
	"errors"
	"fmt"
	"log"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
)

type TracerEbpfObjects struct {
	Kprobe     link.Link
	Tracepoint link.Link
	BpfObjs    bpfObjects
}

func LoadProbes() (TracerEbpfObjects, error) {
	objs := bpfObjects{}
	err := loadBpfObjects(&objs, &ebpf.CollectionOptions{})
	if err != nil {
		var ve *ebpf.VerifierError
		if errors.As(err, &ve) {
			fmt.Printf("Verifier Error: %+v\n", ve)
		}
		return TracerEbpfObjects{}, fmt.Errorf("error loading BPF objects from go-side. %v", err)
	}
	log.Printf("BPF objects loaded")

	// attach a kprobe and tracepoint
	kp, err := link.Kprobe("tcp_data_queue", objs.bpfPrograms.HandleTcpDataQueue, nil)
	if err != nil {
		return TracerEbpfObjects{}, fmt.Errorf("error attaching kprobe: %v", err)
	}
	log.Printf("Kprobe attached successfully")

	tp, err := link.Tracepoint("sock", "inet_sock_set_state", objs.bpfPrograms.HandleSockSetState, nil)
	if err != nil {
		return TracerEbpfObjects{}, fmt.Errorf("error attaching tracepoint: %v", err)
	}
	log.Printf("Tracepoint attached successfully")

	return TracerEbpfObjects{
		Kprobe:     kp,
		Tracepoint: tp,
		BpfObjs:    objs,
	}, nil
}

func (objs *TracerEbpfObjects) UnloadProbes() error {
	// if any close operation fails, will continue to try closing the rest of the struct,
	// and return the first error
	var resultErr error
	resultErr = nil

	err := objs.Kprobe.Close()
	if err != nil {
		resultErr = err
	}
	err = objs.Tracepoint.Close()
	if err != nil && resultErr == nil {
		resultErr = err
	}
	err = objs.BpfObjs.Close()
	if err != nil && resultErr == nil {
		resultErr = err
	}

	return resultErr
}
