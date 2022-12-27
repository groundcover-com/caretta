package caretta

import (
	"encoding/binary"
	"errors"
	"log"
	"net"

	"github.com/cilium/ebpf"
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
	unRoledConnections = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "caretta_current_unroled_connections",
		Help: `Number of connection which coldn't be distinguished to
		 role (client/server) in the last iteration`,
	})
	filteredLoopbackConnections = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "caretta_current_loopback_connections",
		Help: `Number of loopback connections observed in the last iteration`,
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
	connections *ebpf.Map
	resolver    IPResolver
}

// initializes a LinksTracer object
func NewTracer(resolver *k8s.K8sIPResolver) LinksTracer {
	tracer := LinksTracer{resolver: resolver}
	return tracer
}

func NewTracerWithObjs(resolver IPResolver, connections *ebpf.Map, probes Probes) LinksTracer {
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
	tracer.connections = connMap
	return nil
}

func (tracer *LinksTracer) Stop() error {
	tracer.resolver.StopWatching()
	return tracer.ebpfObjects.UnloadProbes()
}

// a single polling from the eBPF maps
// iterating the traces from the kernel-space, summing each network link
func (tracer *LinksTracer) TracesPollingIteration(pastLinks map[NetworkLink]uint64) (map[NetworkLink]uint64, map[NetworkLink]uint64) {
	// outline of an iteration -
	// filter unwanted connections, sum all connections as links, add past links, and return the new map
	pollsMade.Inc()
	unroledCounter := 0
	loopbackCounter := 0

	currentLinks := make(map[NetworkLink]uint64)
	var connectionsToDelete []ConnectionIdentifier

	var conn ConnectionIdentifier
	var throughput ConnectionThroughputStats

	entries := tracer.connections.Iterate()
	// iterate the map from the eBPF program
	for entries.Next(&conn, &throughput) {
		// filter unnecessary connection

		// skip loopback connections
		if conn.Tuple.SrcIp == conn.Tuple.DstIp && isAddressLoopback(conn.Tuple.DstIp) {
			loopbackCounter++
			continue
		}

		// filter unroled connections (probably indicates a bug)
		link, err := tracer.reduceConnectionToLink(conn)
		if conn.Role == UnknownConnectionRole || err != nil {
			unroledCounter++
			continue
		}

		currentLinks[link] += throughput.BytesSent

		if throughput.IsActive == 0 {
			connectionsToDelete = append(connectionsToDelete, conn)
		}
	}

	unRoledConnections.Set(float64(unroledCounter))
	filteredLoopbackConnections.Set(float64(loopbackCounter))

	// add past links
	for pastLink, pastThroughput := range pastLinks {
		currentLinks[pastLink] += pastThroughput
	}

	// delete connections marked to delete
	for _, conn := range connectionsToDelete {
		tracer.deleteAndStoreConnection(&conn, pastLinks)
	}

	return pastLinks, currentLinks

}

func (tracer *LinksTracer) deleteAndStoreConnection(conn *ConnectionIdentifier, pastLinks map[NetworkLink]uint64) {
	// newer kernels introduce batch map operation, but it might not be available so we delete item-by-item
	var throughput ConnectionThroughputStats
	err := tracer.connections.Lookup(conn, &throughput)
	if err != nil {
		log.Printf("Error retreiving connecion to delete, skipping it: %v", err)
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

func isAddressLoopback(ip uint32) bool {
	ipAddr := make(net.IP, 4)
	binary.LittleEndian.PutUint32(ipAddr, ip)
	return ipAddr.IsLoopback()
}
