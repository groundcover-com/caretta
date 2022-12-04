package tracing

import (
	"encoding/binary"
	"net"
)

const (
	CONNECTION_ROLE_UNKNOWN = iota
	CONNECTION_ROLE_CLIENT  = iota
	CONNECTION_ROLE_SERVER  = iota
)

type IP uint32

func (ip IP) String() string {
	netIp := make(net.IP, 4)
	binary.LittleEndian.PutUint32(netIp, uint32(ip))
	return netIp.String()
}

// "final" type of link, like an edge on the graph
type NetworkLink struct {
	ClientHost string
	ServerHost string
	ServerPort uint16
	Role       uint32
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