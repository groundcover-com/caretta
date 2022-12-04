package tracing

import (
	"log"

	"github.com/groundcover-com/caretta/pkg/k8s"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	pollsMade = promauto.NewCounter(prometheus.CounterOpts{
		Name: "caretta_polls_made",
		Help: "Counter of polls made by caretta",
	})
)

// reduce a specific connection to a general link
func reduceConnectionToLink(connection ConnectionIdentifier, resolver k8s.IPResolver) NetworkLink {
	var link NetworkLink
	link.Role = connection.Role

	// TODO add k8s resolving here after k8s package is implemented
	// in the meantime, host is the ip
	srcHost := resolver.ResolveIP(IP(connection.Tuple.SrcIp).String())
	dstHost := resolver.ResolveIP(IP(connection.Tuple.DstIp).String())
	log.Printf("Resolving %d, %d", connection.Tuple.SrcIp, connection.Tuple.DstIp)

	if connection.Role == CONNECTION_ROLE_CLIENT {
		// Src is Client, Dst is Server, Port is DstPort
		link.ClientHost = srcHost
		link.ServerHost = dstHost
		link.ServerPort = connection.Tuple.DstPort
	} else if connection.Role == CONNECTION_ROLE_SERVER {
		// Dst is Client, Src is Server, Port is SrcPort
		link.ClientHost = dstHost
		link.ServerHost = srcHost
		link.ServerPort = connection.Tuple.SrcPort
	} else {
		log.Fatal("Un-roled connection")
	}
	return link
}

type BpfObjects *bpfObjects

// a single polling from the eBPF maps
// iterating the traces from the kernel-space, summing each network link
func TracesPollingIteration(objs *bpfObjects, pastLinks map[NetworkLink]uint64, resolver k8s.IPResolver) (map[NetworkLink]uint64, map[NetworkLink]uint64) {
	// outline of an iteration -
	// filter unwanted connections, sum all connections as links, add past links, and return the new map
	pollsMade.Inc()

	var connectionsToDelete []ConnectionIdentifier
	currentLinks := make(map[NetworkLink]uint64)
	entries := objs.bpfMaps.Connections.Iterate()

	var (
		conn       ConnectionIdentifier
		throughput ConnectionThroughputStats
	)

	// iterate the map from the eBPF program
	for entries.Next(&conn, &throughput) {
		// filter unnecessary connection
		// TODO
		// TODO add metrics on skips

		if conn.Role == CONNECTION_ROLE_UNKNOWN {
			continue
		}

		link := reduceConnectionToLink(conn, resolver)
		currentLinks[link] += throughput.BytesSent

		if throughput.IsActive == 0 {
			connectionsToDelete = append(connectionsToDelete, conn)
		}
	}

	// add past links
	for pastLink, pastThroughput := range pastLinks {
		currentLinks[pastLink] += pastThroughput
	}

	// delete connections marked to delete
	for _, conn := range connectionsToDelete {
		// newer kernels introduce batch map operation, but it might not be available so we delete item-by-item
		var throughput ConnectionThroughputStats
		err := objs.bpfMaps.Connections.Lookup(conn, &throughput)
		if err != nil {
			log.Printf("Error retreiving connecion to delete, skipping it: %v", err)
			continue
		}
		err = objs.bpfMaps.Connections.Delete(conn)
		if err != nil {
			log.Printf("Error deleting connection from map: %v", err)
			continue
		}
		// if deletion is successful, add it to past links
		pastLinks[reduceConnectionToLink(conn, resolver)] += throughput.BytesSent
	}

	return pastLinks, currentLinks

}
