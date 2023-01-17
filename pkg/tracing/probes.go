package tracing

import (
	"errors"
	"fmt"
	"log"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/rlimit"
)

type Probes struct {
	Kprobe     link.Link
	Tracepoint link.Link
	BpfObjs    bpfObjects
}

func LoadProbes() (Probes, *ebpf.Map, error) {
	if err := rlimit.RemoveMemlock(); err != nil {
		return Probes{}, nil, fmt.Errorf("error removing memory lock - %v", err)
	}

	objs := bpfObjects{}
	err := loadBpfObjects(&objs, &ebpf.CollectionOptions{})
	if err != nil {
		var ve *ebpf.VerifierError
		if errors.As(err, &ve) {
			fmt.Printf("Verifier Error: %+v\n", ve)
		}
		return Probes{}, nil, fmt.Errorf("error loading BPF objects from go-side. %v", err)
	}
	log.Printf("BPF objects loaded")

	// attach a kprobe and tracepoint
	kp, err := link.Kprobe("tcp_data_queue", objs.bpfPrograms.HandleTcpDataQueue, nil)
	if err != nil {
		return Probes{}, nil, fmt.Errorf("error attaching kprobe: %v", err)
	}
	log.Printf("Kprobe attached successfully")

	tp, err := link.Tracepoint("sock", "inet_sock_set_state", objs.bpfPrograms.HandleSockSetState, nil)
	if err != nil {
		return Probes{}, nil, fmt.Errorf("error attaching tracepoint: %v", err)
	}
	log.Printf("Tracepoint attached successfully")

	return Probes{
		Kprobe:     kp,
		Tracepoint: tp,
		BpfObjs:    objs,
	}, objs.Connections, nil
}

func (objs *Probes) UnloadProbes() error {
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
