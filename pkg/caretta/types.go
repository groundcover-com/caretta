package caretta

import (
	"encoding/binary"
	"net"

	caretta_k8s "github.com/groundcover-com/caretta/pkg/k8s"
)

const (
	UnknownConnectionRole    = iota
	ClientConnectionRole     = iota
	ServerConnectionRole     = iota
	TcpConnectionOpenState   = iota
	TcpConnectionAcceptState = iota
	TcpConnectionClosedState = iota
)

type IP uint32

func (ip IP) String() string {
	netIp := make(net.IP, 4)
	binary.LittleEndian.PutUint32(netIp, uint32(ip))
	return netIp.String()
}

// "final" type of link, like an edge on the graph
type NetworkLink struct {
	Client     caretta_k8s.Workload
	Server     caretta_k8s.Workload
	ServerPort uint16
	Role       uint32
}

type TcpConnection struct {
	Client     caretta_k8s.Workload
	Server     caretta_k8s.Workload
	ServerPort uint16
	Role       uint32
	State      uint32
}

type ConnectionTuple struct {
	SrcIp   uint32
	DstIp   uint32
	SrcPort uint16
	DstPort uint16
}

type ConnectionIdentifier struct {
	Id    uint32
	Pid   uint32
	Tuple ConnectionTuple
	Role  uint32
}

type ConnectionThroughputStats struct {
	BytesSent     uint64
	BytesReceived uint64
	IsActive      uint64
}
