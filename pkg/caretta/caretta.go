package caretta

import (
	"context"
	"hash/fnv"
	"log"
	"net/http"
	"strconv"
	"strings"
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
		"LinkId", "ClientId", "ClientName", "ClientNamespace", "ClientKind", "ServerId", "ServerName", "ServerNamespace", "ServerKind", "ServerPort", "Role",
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
	resolver, err := caretta_k8s.NewK8sIPResolver(clientset, caretta.config.shouldResolveDns)
	if err != nil {
		log.Fatalf("Error creating resolver: %v", err)
	}
	if resolver.StartWatching() != nil {
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

				if err != nil {
					log.Printf("Error updating snapshot of cluster state, skipping iteration")
					continue
				}

				pastLinks, links = caretta.tracer.TracesPollingIteration(pastLinks)
				for link, throughput := range links {
					caretta.handleLink(&link, throughput)
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
		"LinkId":          strconv.Itoa(int(fnvHash(link.Client.Name+link.Client.Namespace+link.Server.Name+link.Server.Namespace) + link.Role)),
		"ClientId":        strconv.Itoa(int(fnvHash(link.Client.Name + link.Client.Namespace))),
		"ClientName":      link.Client.Name,
		"ClientNamespace": link.Client.Namespace,
		"ClientKind":      link.Client.Kind,
		"ServerId":        strconv.Itoa(int(fnvHash(link.Server.Name + link.Server.Namespace))),
		"ServerName":      link.Server.Name,
		"ServerNamespace": link.Server.Namespace,
		"ServerKind":      link.Server.Kind,
		"ServerPort":      strconv.Itoa(int(link.ServerPort)),
		"Role":            strconv.Itoa(int(link.Role)),
	}).Set(float64(throughput))
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
func splitNamespace(fullname string) (string, string) {
	if !strings.Contains(fullname, ":") {
		return fullname, ""
	}
	s := strings.Split(fullname, ":")
	if len(s) > 1 {
		return s[0], s[1]
	}
	return fullname, ""
}
