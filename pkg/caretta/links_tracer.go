package caretta

import (
	"encoding/binary"
	"errors"
	"log"
	"net"

	"github.com/groundcover-com/caretta/pkg/k8s"
	"github.com/groundcover-com/caretta/pkg/tracing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	pollsMade = promauto.NewCounter(prometheus.CounterOpts{
		Name: "caretta_polls_made",
		Help: "Counter of polls made by caretta",
	})
	failedConnectionDeletion = promauto.NewCounter(prometheus.CounterOpts{
		Name: "caretta_failed_deletions",
		Help: "Counter of failed deletion of closed connection from map",
	})
	filteredLoopbackConnections = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "caretta_current_loopback_connections",
		Help: `Number of loopback connections observed in the last iteration`,
	})
	mapSize = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "caretta_ebpf_connections_map_size",
		Help: "number of items in the connections map iterated from user space per iteration",
	})
	mapDeletions = promauto.NewCounter(prometheus.CounterOpts{
		Name: "caretta_connection_deletions",
		Help: "total number of deletions from the map done by the userspace",
	})
)

type IPResolver interface {
	ResolveIP(string) k8s.Workload
	StartWatching() error
	StopWatching()
}

type Probes interface {
	UnloadProbes() error
}

type LinksTracer struct {
	ebpfObjects Probes
	connections IEbpfMap
	resolver    IPResolver
}

// initializes a LinksTracer object
func NewTracer(resolver *k8s.K8sIPResolver) LinksTracer {
	tracer := LinksTracer{resolver: resolver}
	return tracer
}

func NewTracerWithObjs(resolver IPResolver, connections IEbpfMap, probes Probes) LinksTracer {
	return LinksTracer{
		ebpfObjects: probes,
		connections: connections,
		resolver:    resolver,
	}
}

func (tracer *LinksTracer) Start() error {
	objs, connMap, err := tracing.LoadProbes()
	if err != nil {
		return err
	}

	tracer.ebpfObjects = &objs
	tracer.connections = &EbpfMap{innerMap: connMap}
	return nil
}

func (tracer *LinksTracer) Stop() error {
	tracer.resolver.StopWatching()
	return tracer.ebpfObjects.UnloadProbes()
}

// a single polling from the eBPF maps
// iterating the traces from the kernel-space, summing each network link
func (tracer *LinksTracer) TracesPollingIteration(pastLinks map[NetworkLink]uint64) (map[NetworkLink]uint64, map[NetworkLink]uint64, []TcpConnection) {
	// outline of an iteration -
	// filter unwanted connections, sum all connections as links, add past links, and return the new map
	pollsMade.Inc()
	loopbackCounter := 0

	currentLinks := make(map[NetworkLink]uint64)
	currentTcpConnections := []TcpConnection{}
	var connectionsToDelete []ConnectionIdentifier

	var conn ConnectionIdentifier
	var throughput ConnectionThroughputStats

	entries := tracer.connections.Iterate()
	// iterate the map from the eBPF program
	itemsCounter := 0
	for entries.Next(&conn, &throughput) {
		itemsCounter += 1
		// filter unnecessary connection

		if throughput.IsActive == 0 {
			connectionsToDelete = append(connectionsToDelete, conn)
		}

		// skip loopback connections
		if conn.Tuple.SrcIp == conn.Tuple.DstIp && isAddressLoopback(conn.Tuple.DstIp) {
			loopbackCounter++
			continue
		}

		// filter unroled connections (probably indicates a bug)
		link, err := tracer.reduceConnectionToLink(conn)
		if conn.Role == UnknownConnectionRole || err != nil {
			continue
		}

		tcpConn, err := tracer.reduceConnectionToTcp(conn, throughput)
		if err != nil {
			continue
		}

		currentLinks[link] += throughput.BytesSent
		currentTcpConnections = append(currentTcpConnections, tcpConn)
	}

	mapSize.Set(float64(itemsCounter))
	filteredLoopbackConnections.Set(float64(loopbackCounter))

	// add past links
	for pastLink, pastThroughput := range pastLinks {
		currentLinks[pastLink] += pastThroughput
	}

	// delete connections marked to delete
	for _, conn := range connectionsToDelete {
		tracer.deleteAndStoreConnection(&conn, pastLinks)
	}

	return pastLinks, currentLinks, currentTcpConnections

}

func (tracer *LinksTracer) deleteAndStoreConnection(conn *ConnectionIdentifier, pastLinks map[NetworkLink]uint64) {
	// newer kernels introduce batch map operation, but it might not be available so we delete item-by-item
	var throughput ConnectionThroughputStats
	err := tracer.connections.Lookup(conn, &throughput)
	if err != nil {
		log.Printf("Error retrieving connection to delete, skipping it: %v", err)
		failedConnectionDeletion.Inc()
		return
	}
	err = tracer.connections.Delete(conn)
	if err != nil {
		log.Printf("Error deleting connection from map: %v", err)
		failedConnectionDeletion.Inc()
		return
	}
	// if deletion is successful, add it to past links
	link, err := tracer.reduceConnectionToLink(*conn)
	if err != nil {
		log.Printf("Error reducing connection to link when deleting: %v", err)
		return
	}

	pastLinks[link] += throughput.BytesSent

	mapDeletions.Inc()
}

// reduce a specific connection to a general link
func (tracer *LinksTracer) reduceConnectionToLink(connection ConnectionIdentifier) (NetworkLink, error) {
	var link NetworkLink
	link.Role = connection.Role

	srcWorkload := tracer.resolver.ResolveIP(IP(connection.Tuple.SrcIp).String())
	dstWorkload := tracer.resolver.ResolveIP(IP(connection.Tuple.DstIp).String())

	if connection.Role == ClientConnectionRole {
		// Src is Client, Dst is Server, Port is DstPort
		link.Client = srcWorkload
		link.Server = dstWorkload
		link.ServerPort = connection.Tuple.DstPort
	} else if connection.Role == ServerConnectionRole {
		// Dst is Client, Src is Server, Port is SrcPort
		link.Client = dstWorkload
		link.Server = srcWorkload
		link.ServerPort = connection.Tuple.SrcPort
	} else {
		return NetworkLink{}, errors.New("connection's role is unknown")
	}
	return link, nil
}

// reduce a specific connection to a general tcp connection
func (tracer *LinksTracer) reduceConnectionToTcp(connection ConnectionIdentifier, throughput ConnectionThroughputStats) (TcpConnection, error) {
	var tcpConn TcpConnection
	tcpConn.Role = connection.Role

	srcWorkload := tracer.resolver.ResolveIP(IP(connection.Tuple.SrcIp).String())
	dstWorkload := tracer.resolver.ResolveIP(IP(connection.Tuple.DstIp).String())

	if connection.Role == ClientConnectionRole {
		// Src is Client, Dst is Server, Port is DstPort
		tcpConn.Client = srcWorkload
		tcpConn.Server = dstWorkload
		tcpConn.ServerPort = connection.Tuple.DstPort
		tcpConn.State = TcpConnectionOpenState
	} else if connection.Role == ServerConnectionRole {
		// Dst is Client, Src is Server, Port is SrcPort
		tcpConn.Client = dstWorkload
		tcpConn.Server = srcWorkload
		tcpConn.ServerPort = connection.Tuple.SrcPort
		tcpConn.State = TcpConnectionAcceptState
	} else {
		return TcpConnection{}, errors.New("connection's role is unknown")
	}

	if throughput.IsActive == 0 {
		tcpConn.State = TcpConnectionClosedState
	}

	return tcpConn, nil
}

func isAddressLoopback(ip uint32) bool {
	ipAddr := make(net.IP, 4)
	binary.LittleEndian.PutUint32(ipAddr, ip)
	return ipAddr.IsLoopback()
}
