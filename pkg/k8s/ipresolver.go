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

type clusterSnapshot struct {
	Pods         []v1.Pod
	Nodes        []v1.Node
	ReplicaSets  map[types.UID]appsv1.ReplicaSet
	DaemonSets   map[types.UID]appsv1.DaemonSet
	StatefulSets map[types.UID]appsv1.StatefulSet
	Jobs         map[types.UID]batchv1.Job
	Services     map[types.UID]v1.Service
}

type ipMapping map[string]string

type IPResolver struct {
	clientset *kubernetes.Clientset
	snapshot  clusterSnapshot
	ipsMap    ipMapping
}

func NewIPResolver(clientset *kubernetes.Clientset) *IPResolver {
	return &IPResolver{
		clientset: clientset,
		snapshot: clusterSnapshot{
			ReplicaSets:  make(map[types.UID]appsv1.ReplicaSet),
			DaemonSets:   make(map[types.UID]appsv1.DaemonSet),
			StatefulSets: make(map[types.UID]appsv1.StatefulSet),
			Jobs:         make(map[types.UID]batchv1.Job),
			Services:     make(map[types.UID]v1.Service),
		},
		ipsMap: make(ipMapping),
	}
}

// update the resolver's cache to the current cluster's state
func (resolver IPResolver) UpdateIPResolver() {
	resolver.updateClusterSnapshot()
	resolver.updateIpMapping()
}

// resolve the given IP from the resolver's cache
// if not available, return the IP itself.
func (resolver IPResolver) ResolveIP(ip string) string {
	if val, ok := resolver.ipsMap[ip]; ok {
		return val
	}
	return ip
}

func (resolver IPResolver) updateClusterSnapshot() {
	pods, err := resolver.clientset.CoreV1().Pods("").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		log.Fatal(err)
	}
	resolver.snapshot.Pods = pods.Items

	nodes, err := resolver.clientset.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		log.Fatal(err)
	}
	resolver.snapshot.Nodes = nodes.Items

	replicasets, err := resolver.clientset.AppsV1().ReplicaSets("").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		log.Fatal(err)
	}
	for _, rs := range replicasets.Items {
		resolver.snapshot.ReplicaSets[rs.ObjectMeta.UID] = rs
	}

	daemonsets, err := resolver.clientset.AppsV1().DaemonSets("").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		log.Fatal(err)
	}
	for _, ds := range daemonsets.Items {
		resolver.snapshot.DaemonSets[ds.ObjectMeta.UID] = ds
	}

	statefulsets, err := resolver.clientset.AppsV1().StatefulSets("").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		log.Fatal(err)
	}
	for _, ss := range statefulsets.Items {
		resolver.snapshot.StatefulSets[ss.ObjectMeta.UID] = ss
	}

	jobs, err := resolver.clientset.BatchV1().Jobs("").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		log.Fatal(err)
	}
	for _, job := range jobs.Items {
		resolver.snapshot.Jobs[job.ObjectMeta.UID] = job
	}

	services, err := resolver.clientset.CoreV1().Services("").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		log.Fatal(err)
	}
	for _, service := range services.Items {
		resolver.snapshot.Services[service.UID] = service
	}
}

// add mapping from ip to resolved host to an existing map,
// based on the given cluster snapshot
func (resolver IPResolver) updateIpMapping() {
	// because IP collisions may occur and lead to overwritings in the map, the order is important
	// we go from less "favorable" to more "favorable" -
	// services -> running pods -> nodes

	for _, service := range resolver.snapshot.Services {
		// services has (potentially multiple) ClusterIP
		name := service.Name + ":" + service.Namespace

		// TODO maybe try to match service to workload

		for _, clusterIp := range service.Spec.ClusterIPs {
			if clusterIp != "None" {
				resolver.ipsMap[clusterIp] = name
			}
		}
	}

	for _, pod := range resolver.snapshot.Pods {
		name := resolvePodName(&resolver.snapshot, &pod)
		podPhase := pod.Status.Phase
		for _, podIp := range pod.Status.PodIPs {
			// if ip already in the map, override only if current pod is running
			_, ok := resolver.ipsMap[podIp.IP]
			if !ok || podPhase == v1.PodRunning {
				resolver.ipsMap[podIp.IP] = name
			}
		}
	}

	for _, node := range resolver.snapshot.Nodes {
		for _, nodeAddress := range node.Status.Addresses {
			resolver.ipsMap[nodeAddress.Address] = string(nodeAddress.Type) + "/" + node.Name + ":INTERNAL"
		}
	}
}

// an ugly function to go up one level in hierarchy. maybe there's a better way to do it
// the snapshot is maintained to avoid using an API request for each resolving
func getControllerOfOwner(snapshot *clusterSnapshot, originalOwner *metav1.OwnerReference) (*metav1.OwnerReference, error) {
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

func resolvePodName(snapshot *clusterSnapshot, pod *v1.Pod) string {
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
