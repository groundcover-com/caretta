package caretta

import (
	"hash/fnv"
	"log"
	"strconv"
	"strings"
	"time"

	caretta_k8s "github.com/groundcover-com/caretta/pkg/k8s"
	"github.com/groundcover-com/caretta/pkg/metrics"
	"github.com/groundcover-com/caretta/pkg/tracing"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	prometheusEndpoint     = "/metrics"
	prometheusPort         = ":7117"
	pollingIntervalSeconds = 5
)

var (
	linksMetrics = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "caretta_links_observed",
		Help: "total bytes_sent value of links observed by caretta since its launch",
	}, []string{
		"LinkId", "ClientId", "ClientName", "ClientNamespace", "ServerId", "ServerName", "ServerNamespace", "ServerPort", "Role",
	})
)

type Caretta struct {
	stopSignal chan bool
	tracer     tracing.LinksTracer
}

func NewCaretta() *Caretta {
	return &Caretta{
		stopSignal: make(chan bool, 1),
	}
}

func (caretta *Caretta) Start() {
	metrics.StartMetricsServer(prometheusEndpoint, prometheusPort)

	clientset, err := caretta.getClientSet()
	if err != nil {
		log.Fatalf("Error getting kubernetes clientset: %v", err)
	}
	resolver := caretta_k8s.NewIPResolver(clientset)

	caretta.tracer = tracing.NewTracer(resolver)
	err = caretta.tracer.LoadBpf()
	if err != nil {
		log.Fatalf("Couldn't load probes - %v", err)
	}

	pollingTicker := time.NewTicker(pollingIntervalSeconds * time.Second)

	pastLinks := make(map[tracing.NetworkLink]uint64)

	go func() {
		for {
			select {
			case <-caretta.stopSignal:
				return
			case <-pollingTicker.C:
				var links map[tracing.NetworkLink]uint64

				err := resolver.Update()
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
	err := caretta.tracer.UnloadBpf()
	if err != nil {
		log.Printf("Error unloading bpf objects: %v", err)
	}
}

func (caretta *Caretta) handleLink(link *tracing.NetworkLink, throughput uint64) {
	clientName, clientNamespace := splitNamespace(link.ClientHost)
	serverName, serverNamespace := splitNamespace(link.ServerHost)
	linksMetrics.With(prometheus.Labels{
		"LinkId":          strconv.Itoa(int(fnvHash(link.ClientHost+link.ServerHost) + link.Role)),
		"ClientId":        strconv.Itoa(int(fnvHash(link.ClientHost))),
		"ClientName":      clientName,
		"ClientNamespace": clientNamespace,
		"ServerId":        strconv.Itoa(int(fnvHash(link.ServerHost))),
		"ServerName":      serverName,
		"ServerNamespace": serverNamespace,
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
