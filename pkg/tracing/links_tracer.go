package tracing

import (
	"log"

	"github.com/cilium/ebpf"
	"github.com/groundcover-com/caretta/pkg/k8s"
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

type BpfObjects *bpfObjects

// a single polling from the eBPF maps
// iterating the traces from the kernel-space, summing each network link
func TracesPollingIteration(objs *bpfObjects, pastLinks map[NetworkLink]uint64, resolver k8s.IPResolver) (map[NetworkLink]uint64, map[NetworkLink]uint64) {
	// outline of an iteration -
	// filter unwanted connections, sum all connections as links, add past links, and return the new map
	pollsMade.Inc()
	unroledCounter := 0
	loopbackCounter := 0

	var connectionsToDelete []ConnectionIdentifier
	currentLinks := make(map[NetworkLink]uint64)

	var (
		conn       ConnectionIdentifier
		throughput ConnectionThroughputStats
	)

	entries := objs.bpfMaps.Connections.Iterate()
	// iterate the map from the eBPF program
	for entries.Next(&conn, &throughput) {
		// filter unnecessary connection

		// skip loopback connections
		if conn.Tuple.SrcIp == conn.Tuple.DstIp {
			loopbackCounter++
			continue
		}

		// filter unroled connections (probably indicates a bug)
		if conn.Role == UnknownConnectionRole {
			unroledCounter++
			continue
		}

		link := reduceConnectionToLink(conn, resolver)
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
		link := reduceConnectionToLink(conn, resolver)
		deleteAndStoreConnection(objs.bpfMaps.Connections, &conn, pastLinks, &link)
	}

	return pastLinks, currentLinks

}

func deleteAndStoreConnection(connections *ebpf.Map, conn *ConnectionIdentifier, pastLinks map[NetworkLink]uint64, link *NetworkLink) {
	// newer kernels introduce batch map operation, but it might not be available so we delete item-by-item
	var throughput ConnectionThroughputStats
	err := connections.Lookup(conn, &throughput)
	if err != nil {
		log.Printf("Error retreiving connecion to delete, skipping it: %v", err)
		failedConnectionDeletion.Inc()
		return
	}
	err = connections.Delete(conn)
	if err != nil {
		log.Printf("Error deleting connection from map: %v", err)
		failedConnectionDeletion.Inc()
		return
	}
	// if deletion is successful, add it to past links
	pastLinks[*link] += throughput.BytesSent
}

// reduce a specific connection to a general link
func reduceConnectionToLink(connection ConnectionIdentifier, resolver k8s.IPResolver) NetworkLink {
	var link NetworkLink
	link.Role = connection.Role

	srcHost := resolver.ResolveIP(IP(connection.Tuple.SrcIp).String())
	dstHost := resolver.ResolveIP(IP(connection.Tuple.DstIp).String())

	if connection.Role == ClientConnectionRole {
		// Src is Client, Dst is Server, Port is DstPort
		link.ClientHost = srcHost
		link.ServerHost = dstHost
		link.ServerPort = connection.Tuple.DstPort
	} else if connection.Role == ServerConnectionRole {
		// Dst is Client, Src is Server, Port is SrcPort
		link.ClientHost = dstHost
		link.ServerHost = srcHost
		link.ServerPort = connection.Tuple.SrcPort
	} else {
		// shouldn't get here
		log.Fatal("Un-roled connection")
	}
	return link
}
