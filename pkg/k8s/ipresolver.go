package k8s

import (
	"context"
	"errors"
	"log"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type ClusterSnapshot struct {
	Pods         []v1.Pod
	Nodes        []v1.Node
	ReplicaSets  map[types.UID]appsv1.ReplicaSet
	DaemonSets   map[types.UID]appsv1.DaemonSet
	StatefulSets map[types.UID]appsv1.StatefulSet
	Jobs         map[types.UID]batchv1.Job
	Services     map[types.UID]v1.Service
}

type IPMapping map[string]string

type IPResolver struct {
	clientset *kubernetes.Clientset
	snapshot  ClusterSnapshot
	ipsMap    IPMapping
}

func NewIPResolver(clientset *kubernetes.Clientset) *IPResolver {
	return &IPResolver{
		clientset: clientset,
		snapshot: ClusterSnapshot{
			ReplicaSets:  make(map[types.UID]appsv1.ReplicaSet),
			DaemonSets:   make(map[types.UID]appsv1.DaemonSet),
			StatefulSets: make(map[types.UID]appsv1.StatefulSet),
			Jobs:         make(map[types.UID]batchv1.Job),
			Services:     make(map[types.UID]v1.Service),
		},
		ipsMap: make(IPMapping),
	}
}

func (resolver IPResolver) UpdateIPResolver() {
	resolver.updateClusterSnapshot()
	resolver.updateIpMapping()
}

func (resolver IPResolver) ResolveIP(ip string) string {
	if val, ok := resolver.ipsMap[ip]; ok {
		return val
	}
	return ip
}

func (ipResolver IPResolver) updateClusterSnapshot() {
	pods, err := ipResolver.clientset.CoreV1().Pods("").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		log.Fatal(err)
	}
	ipResolver.snapshot.Pods = pods.Items

	nodes, err := ipResolver.clientset.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		log.Fatal(err)
	}
	ipResolver.snapshot.Nodes = nodes.Items

	replicasets, err := ipResolver.clientset.AppsV1().ReplicaSets("").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		log.Fatal(err)
	}
	for _, rs := range replicasets.Items {
		ipResolver.snapshot.ReplicaSets[rs.ObjectMeta.UID] = rs
	}

	daemonsets, err := ipResolver.clientset.AppsV1().DaemonSets("").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		log.Fatal(err)
	}
	for _, ds := range daemonsets.Items {
		ipResolver.snapshot.DaemonSets[ds.ObjectMeta.UID] = ds
	}

	statefulsets, err := ipResolver.clientset.AppsV1().StatefulSets("").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		log.Fatal(err)
	}
	for _, ss := range statefulsets.Items {
		ipResolver.snapshot.StatefulSets[ss.ObjectMeta.UID] = ss
	}

	jobs, err := ipResolver.clientset.BatchV1().Jobs("").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		log.Fatal(err)
	}
	for _, job := range jobs.Items {
		ipResolver.snapshot.Jobs[job.ObjectMeta.UID] = job
	}

	services, err := ipResolver.clientset.CoreV1().Services("").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		log.Fatal(err)
	}
	for _, service := range services.Items {
		ipResolver.snapshot.Services[service.UID] = service
	}
}

// add mapping from ip to resolved host to an existing map,
// based on the given cluster snapshot
func (ipResolver IPResolver) updateIpMapping() {
	// because IP collisions may occur and lead to overwritings in the map, the order is important
	// we go from less "favorable" to more "favorable" -
	// services -> running pods -> nodes

	for _, service := range ipResolver.snapshot.Services {
		// services has (potentially multiple) ClusterIP
		name := service.Name + ":" + service.Namespace

		// TODO maybe try to match service to workload

		for _, clusterIp := range service.Spec.ClusterIPs {
			if clusterIp != "None" {
				ipResolver.ipsMap[clusterIp] = name
			}
		}
	}

	for _, pod := range ipResolver.snapshot.Pods {
		name := resolvePodName(&ipResolver.snapshot, &pod)
		podPhase := pod.Status.Phase
		for _, podIp := range pod.Status.PodIPs {
			// if ip already in the map, override only if current pod is running
			_, ok := ipResolver.ipsMap[podIp.IP]
			if !ok || podPhase == v1.PodRunning {
				ipResolver.ipsMap[podIp.IP] = name
			}
		}
	}

	for _, node := range ipResolver.snapshot.Nodes {
		for _, nodeAddress := range node.Status.Addresses {
			ipResolver.ipsMap[nodeAddress.Address] = string(nodeAddress.Type) + "/" + node.Name + ":INTERNAL"
		}
	}
}

// an ugly function to go up one level in hierarchy. maybe there's a better way to do it
// the snapshot is maintained to avoid using an API request for each resolving
func getControllerOfOwner(snapshot *ClusterSnapshot, originalOwner *metav1.OwnerReference) (*metav1.OwnerReference, error) {
	switch originalOwner.Kind {
	case "ReplicaSet":
		replicaSet, ok := snapshot.ReplicaSets[originalOwner.UID]
		if !ok {
			return nil, errors.New("Missing replicaset for UID " + string(originalOwner.UID))
		}
		return metav1.GetControllerOf(&replicaSet), nil
	case "DaemonSet":
		daemonset, ok := snapshot.DaemonSets[originalOwner.UID]
		if !ok {
			return nil, errors.New("Missing daemonset for UID " + string(originalOwner.UID))
		}
		return metav1.GetControllerOf(&daemonset), nil
	case "StatefulSet":
		statefulSet, ok := snapshot.StatefulSets[originalOwner.UID]
		if !ok {
			return nil, errors.New("Missing statefulset for UID " + string(originalOwner.UID))
		}
		return metav1.GetControllerOf(&statefulSet), nil
	case "Job":
		job, ok := snapshot.Jobs[originalOwner.UID]
		if !ok {
			return nil, errors.New("Missing job for UID " + string(originalOwner.UID))
		}
		return metav1.GetControllerOf(&job), nil
	}
	return nil, errors.New("Unsupported kind for lookup - " + originalOwner.Kind)
}

func resolvePodName(snapshot *ClusterSnapshot, pod *v1.Pod) string {
	name := pod.Name + ":" + pod.Namespace
	owner := metav1.GetControllerOf(pod)
	for owner != nil {
		var err error // to avoid declaring an unused var
		name = owner.Name + ":" + pod.Namespace
		owner, err = getControllerOfOwner(snapshot, owner)
		if err != nil {
			log.Printf("Error retreiving owner of %v - %v", name, err)
		}
	}
	return name
}
