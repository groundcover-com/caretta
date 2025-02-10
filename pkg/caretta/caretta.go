package caretta

import (
	"context"
	"hash/fnv"
	"log"
	"net/http"
	"strconv"
	"time"

	caretta_k8s "github.com/groundcover-com/caretta/pkg/k8s"
	"github.com/groundcover-com/caretta/pkg/metrics"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

var (
	linksMetrics = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "caretta_links_observed",
		Help: "total bytes_sent value of links observed by caretta since its launch",
	}, []string{
		"link_id", "client_id", "client_name", "client_namespace", "client_kind", "client_owner", "server_id", "server_name", "server_namespace", "server_kind", "server_port", "role",
	})
	connectionsMetrics = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "caretta_connections_observed",
		Help: "state of connections observed by caretta since its launch",
	}, []string{
		"connection_id", "connection_state", "client_id", "client_name", "client_namespace", "client_kind", "client_owner", "server_id", "server_name", "server_namespace", "server_kind", "server_port", "role",
	})
)

type Caretta struct {
	stopSignal    chan bool
	tracer        LinksTracer
	metricsServer *http.Server
	config        carettaConfig
}

func NewCaretta() *Caretta {
	return &Caretta{
		stopSignal: make(chan bool, 1),
		config:     readConfig(),
	}
}

func (caretta *Caretta) Start() {
	caretta.metricsServer = metrics.StartMetricsServer(caretta.config.prometheusEndpoint, caretta.config.prometheusPort)

	clientset, err := caretta.getClientSet()
	if err != nil {
		log.Fatalf("Error getting kubernetes clientset: %v", err)
	}
	resolver, err := caretta_k8s.NewK8sIPResolver(clientset, caretta.config.shouldResolveDns, caretta.config.traverseUpHierarchy)
	if err != nil {
		log.Fatalf("Error creating resolver: %v", err)
	}
	err = resolver.StartWatching()
	if err != nil {
		log.Fatalf("Error watching cluster's state: %v", err)
	}

	// wait for resolver to populate
	time.Sleep(10 * time.Second)

	caretta.tracer = NewTracer(resolver)
	err = caretta.tracer.Start()
	if err != nil {
		log.Fatalf("Couldn't load probes - %v", err)
	}

	pollingTicker := time.NewTicker(time.Duration(caretta.config.pollingIntervalSeconds) * time.Second)

	pastLinks := make(map[NetworkLink]uint64)

	go func() {
		for {
			select {
			case <-caretta.stopSignal:
				return
			case <-pollingTicker.C:
				var links map[NetworkLink]uint64
				var connections []ConnectionLink

				if err != nil {
					log.Printf("Error updating snapshot of cluster state, skipping iteration")
					continue
				}

				pastLinks, links, connections = caretta.tracer.TracesPollingIteration(pastLinks)
				for link, throughput := range links {
					caretta.handleLink(&link, throughput)
				}

				for _, connection := range connections {
					caretta.handleConnection(&connection)
				}
			}
		}
	}()
}

func (caretta *Caretta) Stop() {
	log.Print("Stopping Caretta...")
	caretta.stopSignal <- true
	err := caretta.tracer.Stop()
	if err != nil {
		log.Printf("Error unloading bpf objects: %v", err)
	}
	err = caretta.metricsServer.Shutdown(context.Background())
	if err != nil {
		log.Printf("Error shutting Prometheus server down: %v", err)
	}

}

func (caretta *Caretta) handleLink(link *NetworkLink, throughput uint64) {
	linksMetrics.With(prometheus.Labels{
		"link_id":          strconv.Itoa(int(fnvHash(link.Client.Name+link.Client.Namespace+link.Server.Name+link.Server.Namespace) + link.Role)),
		"client_id":        strconv.Itoa(int(fnvHash(link.Client.Name + link.Client.Namespace))),
		"client_name":      link.Client.Name,
		"client_namespace": link.Client.Namespace,
		"client_kind":      link.Client.Kind,
		"client_owner":     link.Client.Owner,
		"server_id":        strconv.Itoa(int(fnvHash(link.Server.Name + link.Server.Namespace))),
		"server_name":      link.Server.Name,
		"server_namespace": link.Server.Namespace,
		"server_kind":      link.Server.Kind,
		"server_port":      strconv.Itoa(int(link.ServerPort)),
		"role":             strconv.Itoa(int(link.Role)),
	}).Set(float64(throughput))
}

func (caretta *Caretta) handleConnection(connection *ConnectionLink) {
	connectionsMetrics.With(prometheus.Labels{
		"connection_id":    strconv.Itoa(int(fnvHash(connection.Client.Name+connection.Client.Namespace+connection.Server.Name+connection.Server.Namespace) + connection.Role)),
		"connection_state": connection.State,
		"client_id":        strconv.Itoa(int(fnvHash(connection.Client.Name + connection.Client.Namespace))),
		"client_name":      connection.Client.Name,
		"client_namespace": connection.Client.Namespace,
		"client_kind":      connection.Client.Kind,
		"client_owner":     connection.Client.Owner,
		"server_id":        strconv.Itoa(int(fnvHash(connection.Server.Name + connection.Server.Namespace))),
		"server_name":      connection.Server.Name,
		"server_namespace": connection.Server.Namespace,
		"server_kind":      connection.Server.Kind,
		"server_port":      strconv.Itoa(int(connection.ServerPort)),
		"role":             strconv.Itoa(int(connection.Role)),
	}).Set(1)
}

func (caretta *Caretta) getClientSet() (*kubernetes.Clientset, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	return clientset, nil
}

// simple fnvHash function from string to uint32
func fnvHash(s string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(s))
	return h.Sum32()
}

// gets a hostname (probably in the pattern name:namespace) and split it to name and namespace
// basically a wrapped Split function to handle some edge cases
// func splitNamespace(fullname string) (string, string) {
// 	if !strings.Contains(fullname, ":") {
// 		return fullname, ""
// 	}
// 	s := strings.Split(fullname, ":")
// 	if len(s) > 1 {
// 		return s[0], s[1]
// 	}
// 	return fullname, ""
// }
