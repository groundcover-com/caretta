package k8s_test

import (
	"log"
	"testing"
	"time"

	"github.com/groundcover-com/caretta/pkg/k8s"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	testclient "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

type podDescriptor struct {
	Name       string
	Namespace  string
	IP         string
	Phase      v1.PodPhase
	UID        types.UID
	Controller *workloadResourceDescriptor
}

type nodeDescriptor struct {
	Name string
	IP   string
	UID  types.UID
}

type workloadResourceDescriptor struct {
	Name      string
	Namespace string
	UID       types.UID
	Kind      string
}

func (desc *workloadResourceDescriptor) CreateObject() runtime.Object {
	switch desc.Kind {
	case "Deployment":
		{
			return &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      desc.Name,
					Namespace: desc.Namespace,
					UID:       desc.UID,
				},
			}
		}
	case "ReplicaSet":
		{
			return &appsv1.ReplicaSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      desc.Name,
					Namespace: desc.Namespace,
					UID:       desc.UID,
				},
			}
		}
	case "DaemonSet":
		{
			return &appsv1.DaemonSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      desc.Name,
					Namespace: desc.Namespace,
					UID:       desc.UID,
				},
			}
		}
	case "StatefulSet":
		{
			return &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      desc.Name,
					Namespace: desc.Namespace,
					UID:       desc.UID,
				},
			}
		}
	case "Job":
		{
			return &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      desc.Name,
					Namespace: desc.Namespace,
					UID:       desc.UID,
				},
			}
		}
	case "Service":
		{
			return &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      desc.Name,
					Namespace: desc.Namespace,
					UID:       desc.UID,
				},
			}
		}
	case "CronJob":
		{
			return &batchv1.CronJob{
				ObjectMeta: metav1.ObjectMeta{
					Name:      desc.Name,
					Namespace: desc.Namespace,
					UID:       desc.UID,
				},
			}
		}
	}
	return nil
}

func generatePod(pod podDescriptor) runtime.Object {
	newPod := v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pod.Name,
			Namespace: pod.Namespace,
			UID:       pod.UID,
		},
		Status: v1.PodStatus{
			PodIP: pod.IP,
			PodIPs: []v1.PodIP{
				{IP: pod.IP},
			},
		},
	}
	if pod.Controller != nil {
		newTrue := new(bool)
		*newTrue = true
		ref := metav1.OwnerReference{
			Kind:       pod.Controller.Kind,
			Name:       pod.Controller.Name,
			UID:        pod.Controller.UID,
			Controller: newTrue,
		}
		newPod.OwnerReferences = append(newPod.OwnerReferences, ref)
	}
	return &newPod

}

func generateWorkloadResource(desc workloadResourceDescriptor) runtime.Object {
	return desc.CreateObject()
}

func generateNode(node nodeDescriptor) runtime.Object {
	return &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: node.Name,
			UID:  node.UID,
		},
		Status: v1.NodeStatus{
			Addresses: []v1.NodeAddress{
				{
					Type:    "InternalIP",
					Address: node.IP,
				},
			},
		},
	}
}

func generateClusterObjects(pods []podDescriptor, workloadsResources []workloadResourceDescriptor, nodes []nodeDescriptor) []runtime.Object {
	result := make([]runtime.Object, 0, len(pods)+len(workloadsResources)+len(nodes))
	for _, pod := range pods {
		newPod := generatePod(pod)
		result = append(result, newPod)
	}
	for _, desc := range workloadsResources {
		result = append(result, generateWorkloadResource(desc))
	}
	for _, node := range nodes {
		result = append(result, generateNode(node))
	}
	return result
}

type testStep struct {
	shouldWait                bool
	newPods                   []podDescriptor
	newNodes                  []nodeDescriptor
	newWorkloadResource       []workloadResourceDescriptor
	modifiedPods              []podDescriptor
	modifiedNodes             []nodeDescriptor
	modifiedWorkloadResources []workloadResourceDescriptor
	expectedResolves          map[string]k8s.Workload
}

type test struct {
	description  string
	initialState testStep
	updateSteps  []testStep
}

type fakeWatchers struct {
	nodesWatcher        *watch.FakeWatcher
	podsWatcher         *watch.FakeWatcher
	deploymentsWatcher  *watch.FakeWatcher
	replicasetsWatcher  *watch.FakeWatcher
	daemonsetsWatcher   *watch.FakeWatcher
	statefulsetsWatcher *watch.FakeWatcher
	jobsWatcher         *watch.FakeWatcher
	servicesWatcher     *watch.FakeWatcher
	cronjobsWatcher     *watch.FakeWatcher
}

func createPrependWatchers(clientset *testclient.Clientset) fakeWatchers {
	watchers := fakeWatchers{
		nodesWatcher:        watch.NewFake(),
		podsWatcher:         watch.NewFake(),
		deploymentsWatcher:  watch.NewFake(),
		replicasetsWatcher:  watch.NewFake(),
		daemonsetsWatcher:   watch.NewFake(),
		statefulsetsWatcher: watch.NewFake(),
		jobsWatcher:         watch.NewFake(),
		servicesWatcher:     watch.NewFake(),
		cronjobsWatcher:     watch.NewFake(),
	}
	clientset.PrependWatchReactor("nodes", k8stesting.DefaultWatchReactor(watchers.nodesWatcher, nil))
	clientset.PrependWatchReactor("pods", k8stesting.DefaultWatchReactor(watchers.podsWatcher, nil))
	clientset.PrependWatchReactor("deployments", k8stesting.DefaultWatchReactor(watchers.deploymentsWatcher, nil))
	clientset.PrependWatchReactor("replicasets", k8stesting.DefaultWatchReactor(watchers.replicasetsWatcher, nil))
	clientset.PrependWatchReactor("daemonsets", k8stesting.DefaultWatchReactor(watchers.daemonsetsWatcher, nil))
	clientset.PrependWatchReactor("statefulsets", k8stesting.DefaultWatchReactor(watchers.statefulsetsWatcher, nil))
	clientset.PrependWatchReactor("jobs", k8stesting.DefaultWatchReactor(watchers.jobsWatcher, nil))
	clientset.PrependWatchReactor("services", k8stesting.DefaultWatchReactor(watchers.servicesWatcher, nil))
	clientset.PrependWatchReactor("cronjobs", k8stesting.DefaultWatchReactor(watchers.cronjobsWatcher, nil))
	return watchers
}

func addObject(watchers fakeWatchers, obj runtime.Object, kind string) {
	switch kind {
	case "Pod":
		{
			watchers.podsWatcher.Add(obj)
		}
	case "Node":
		{
			watchers.nodesWatcher.Add(obj)
		}
	case "Deployment":
		{
			watchers.deploymentsWatcher.Add(obj)
		}
	case "ReplicaSet":
		{
			watchers.replicasetsWatcher.Add(obj)
		}
	case "DaemonSet":
		{
			watchers.daemonsetsWatcher.Add(obj)
		}
	case "StatefulSet":
		{
			watchers.statefulsetsWatcher.Add(obj)
		}
	case "Job":
		{
			watchers.jobsWatcher.Add(obj)
		}
	case "Service":
		{
			watchers.servicesWatcher.Add(obj)
		}
	case "CronJob":
		{
			watchers.cronjobsWatcher.Add(obj)
		}
	}
}

func modifyObject(watchers fakeWatchers, obj runtime.Object, kind string) {
	switch kind {
	case "Pod":
		{
			watchers.podsWatcher.Modify(obj)
		}
	case "Node":
		{
			watchers.nodesWatcher.Modify(obj)
		}
	case "Deployment":
		{
			watchers.deploymentsWatcher.Modify(obj)
		}
	case "ReplicaSet":
		{
			watchers.replicasetsWatcher.Modify(obj)
		}
	case "DaemonSet":
		{
			watchers.daemonsetsWatcher.Modify(obj)
		}
	case "StatefulSet":
		{
			watchers.statefulsetsWatcher.Modify(obj)
		}
	case "Job":
		{
			watchers.jobsWatcher.Modify(obj)
		}
	case "Service":
		{
			watchers.servicesWatcher.Modify(obj)
		}
	case "CronJob":
		{
			watchers.cronjobsWatcher.Modify(obj)
		}
	default:
		{
			log.Printf("unhandled kind %v", kind)
		}
	}
}

func TestResolving(t *testing.T) {
	var tests = []test{
		{
			description: "initial snapshot 1 pod",
			initialState: testStep{
				shouldWait: false,
				newPods: []podDescriptor{
					{"pod1", "namespaceA", "1.1.1.1", v1.PodRunning, types.UID(uuid.New().String()), nil},
				},
				newNodes:                  []nodeDescriptor{},
				newWorkloadResource:       []workloadResourceDescriptor{},
				modifiedPods:              []podDescriptor{},
				modifiedNodes:             []nodeDescriptor{},
				modifiedWorkloadResources: []workloadResourceDescriptor{},
				expectedResolves: map[string]k8s.Workload{
					"1.1.1.1": {
						Name:      "pod1",
						Namespace: "namespaceA",
						Kind:      "pod",
					},
					"1.1.1.2": {
						Name:      "1.1.1.2",
						Namespace: "External",
						Kind:      "External",
					},
				},
			},
			updateSteps: []testStep{},
		},
		{
			description: "initial snapshot 3 pods",
			initialState: testStep{
				shouldWait: false,
				newPods: []podDescriptor{
					{"pod1", "namespaceA", "1.1.1.1", v1.PodRunning, types.UID(uuid.New().String()), nil},
					{"pod2", "namespaceA", "1.1.1.2", v1.PodRunning, types.UID(uuid.New().String()), nil},
					{"pod3", "namespaceA", "1.1.1.3", v1.PodRunning, types.UID(uuid.New().String()), nil},
				},
				newNodes:                  []nodeDescriptor{},
				newWorkloadResource:       []workloadResourceDescriptor{},
				modifiedPods:              []podDescriptor{},
				modifiedNodes:             []nodeDescriptor{},
				modifiedWorkloadResources: []workloadResourceDescriptor{},
				expectedResolves: map[string]k8s.Workload{
					"1.1.1.1": {
						Name:      "pod1",
						Namespace: "namespaceA",
						Kind:      "pod",
					},
					"1.1.1.2": {
						Name:      "pod2",
						Namespace: "namespaceA",
						Kind:      "pod",
					},
					"1.1.1.3": {
						Name:      "pod3",
						Namespace: "namespaceA",
						Kind:      "pod",
					},
				},
			},
			updateSteps: []testStep{},
		},
		{
			description: "empty initial 1 pod added",
			initialState: testStep{
				shouldWait:                false,
				newPods:                   []podDescriptor{},
				newNodes:                  []nodeDescriptor{},
				newWorkloadResource:       []workloadResourceDescriptor{},
				modifiedPods:              []podDescriptor{},
				modifiedNodes:             []nodeDescriptor{},
				modifiedWorkloadResources: []workloadResourceDescriptor{},
				expectedResolves: map[string]k8s.Workload{
					"1.1.1.1": {
						Name:      "1.1.1.1",
						Namespace: "External",
						Kind:      "External",
					},
				},
			},
			updateSteps: []testStep{
				{
					shouldWait: true,
					newPods: []podDescriptor{
						{"pod1", "namespaceA", "1.1.1.1", v1.PodRunning, types.UID(uuid.New().String()), nil},
					},
					newNodes:                  []nodeDescriptor{},
					newWorkloadResource:       []workloadResourceDescriptor{},
					modifiedPods:              []podDescriptor{},
					modifiedNodes:             []nodeDescriptor{},
					modifiedWorkloadResources: []workloadResourceDescriptor{},
					expectedResolves: map[string]k8s.Workload{
						"1.1.1.1": {
							Name:      "pod1",
							Namespace: "namespaceA",
							Kind:      "pod",
						},
					},
				},
			},
		},
		{
			description: "empty initial 1 node added",
			initialState: testStep{
				shouldWait:                false,
				newPods:                   []podDescriptor{},
				newNodes:                  []nodeDescriptor{},
				newWorkloadResource:       []workloadResourceDescriptor{},
				modifiedPods:              []podDescriptor{},
				modifiedNodes:             []nodeDescriptor{},
				modifiedWorkloadResources: []workloadResourceDescriptor{},
				expectedResolves: map[string]k8s.Workload{
					"1.1.1.0": {
						Name:      "1.1.1.0",
						Namespace: "External",
						Kind:      "External",
					},
				},
			},
			updateSteps: []testStep{
				{
					shouldWait: true,
					newPods:    []podDescriptor{},
					newNodes: []nodeDescriptor{
						{"Node1", "1.1.1.0", types.UID(uuid.NewString())},
					},
					newWorkloadResource:       []workloadResourceDescriptor{},
					modifiedPods:              []podDescriptor{},
					modifiedNodes:             []nodeDescriptor{},
					modifiedWorkloadResources: []workloadResourceDescriptor{},
					expectedResolves: map[string]k8s.Workload{
						"1.1.1.0": {
							Name:      "Node1",
							Namespace: "Node",
							Kind:      "Node",
						},
					},
				},
			},
		},
		{
			description: "empty initial 1 node, 1 pod added",
			initialState: testStep{
				shouldWait:                false,
				newPods:                   []podDescriptor{},
				newNodes:                  []nodeDescriptor{},
				newWorkloadResource:       []workloadResourceDescriptor{},
				modifiedPods:              []podDescriptor{},
				modifiedNodes:             []nodeDescriptor{},
				modifiedWorkloadResources: []workloadResourceDescriptor{},
				expectedResolves: map[string]k8s.Workload{
					"1.1.1.0": {
						Name:      "1.1.1.0",
						Namespace: "External",
						Kind:      "External",
					},
				},
			},
			updateSteps: []testStep{
				{
					shouldWait: true,
					newPods: []podDescriptor{
						{"pod1", "namespaceA", "1.1.1.1", v1.PodRunning, types.UID(uuid.New().String()), nil},
					},
					newNodes: []nodeDescriptor{
						{"Node1", "1.1.1.0", types.UID(uuid.NewString())},
					},
					newWorkloadResource:       []workloadResourceDescriptor{},
					modifiedPods:              []podDescriptor{},
					modifiedNodes:             []nodeDescriptor{},
					modifiedWorkloadResources: []workloadResourceDescriptor{},
					expectedResolves: map[string]k8s.Workload{
						"1.1.1.0": {
							Name:      "Node1",
							Namespace: "Node",
							Kind:      "Node",
						},
						"1.1.1.1": {
							Name:      "pod1",
							Namespace: "namespaceA",
							Kind:      "pod",
						},
					},
				},
			},
		},
		{
			description: "1 pod changing ip",
			initialState: testStep{
				shouldWait: false,
				newPods: []podDescriptor{
					{"pod1", "namespaceA", "1.1.1.1", v1.PodRunning, types.UID(uuid.New().String()), nil},
				},
				newNodes:                  []nodeDescriptor{},
				newWorkloadResource:       []workloadResourceDescriptor{},
				modifiedPods:              []podDescriptor{},
				modifiedNodes:             []nodeDescriptor{},
				modifiedWorkloadResources: []workloadResourceDescriptor{},
				expectedResolves: map[string]k8s.Workload{
					"1.1.1.1": {
						Name:      "pod1",
						Namespace: "namespaceA",
						Kind:      "pod",
					},
					"1.1.1.2": {
						Name:      "1.1.1.2",
						Namespace: "External",
						Kind:      "External",
					},
				},
			},
			updateSteps: []testStep{
				{
					shouldWait:          true,
					newPods:             []podDescriptor{},
					newNodes:            []nodeDescriptor{},
					newWorkloadResource: []workloadResourceDescriptor{},
					modifiedPods: []podDescriptor{
						{"pod1", "namespaceA", "1.1.1.2", v1.PodRunning, types.UID(uuid.New().String()), nil},
					},
					modifiedNodes:             []nodeDescriptor{},
					modifiedWorkloadResources: []workloadResourceDescriptor{},
					expectedResolves: map[string]k8s.Workload{
						"1.1.1.1": { // the resolver shouldn't delete old not-reused entries
							Name:      "pod1",
							Namespace: "namespaceA",
							Kind:      "pod",
						},
						"1.1.1.2": {
							Name:      "pod1",
							Namespace: "namespaceA",
							Kind:      "pod",
						},
					},
				},
			},
		},
		{
			description: "1 pod changing ip old ip is reused",
			initialState: testStep{
				shouldWait: false,
				newPods: []podDescriptor{
					{"pod1", "namespaceA", "1.1.1.1", v1.PodRunning, types.UID("1"), nil},
				},
				newNodes:                  []nodeDescriptor{},
				newWorkloadResource:       []workloadResourceDescriptor{},
				modifiedPods:              []podDescriptor{},
				modifiedNodes:             []nodeDescriptor{},
				modifiedWorkloadResources: []workloadResourceDescriptor{},
				expectedResolves: map[string]k8s.Workload{
					"1.1.1.1": {
						Name:      "pod1",
						Namespace: "namespaceA",
						Kind:      "pod",
					},
					"1.1.1.2": {
						Name:      "1.1.1.2",
						Namespace: "External",
						Kind:      "External",
					},
				},
			},
			updateSteps: []testStep{
				{
					shouldWait:          false,
					newPods:             []podDescriptor{},
					newNodes:            []nodeDescriptor{},
					newWorkloadResource: []workloadResourceDescriptor{},
					modifiedPods: []podDescriptor{
						{"pod1", "namespaceA", "1.1.1.2", v1.PodRunning, types.UID("1"), nil},
					},
					modifiedNodes:             []nodeDescriptor{},
					modifiedWorkloadResources: []workloadResourceDescriptor{},
					expectedResolves:          map[string]k8s.Workload{},
				},
				{
					shouldWait: true,
					newPods: []podDescriptor{
						{"pod2", "namespaceA", "1.1.1.1", v1.PodRunning, types.UID(uuid.New().String()), nil},
					},
					newNodes:                  []nodeDescriptor{},
					newWorkloadResource:       []workloadResourceDescriptor{},
					modifiedPods:              []podDescriptor{},
					modifiedNodes:             []nodeDescriptor{},
					modifiedWorkloadResources: []workloadResourceDescriptor{},
					expectedResolves: map[string]k8s.Workload{
						"1.1.1.1": {
							Name:      "pod2",
							Namespace: "namespaceA",
							Kind:      "pod",
						},
						"1.1.1.2": {
							Name:      "pod1",
							Namespace: "namespaceA",
							Kind:      "pod",
						},
					},
				},
			},
		},
		{
			description: "1 pod changing ip old ip is reused by node",
			initialState: testStep{
				shouldWait: false,
				newPods: []podDescriptor{
					{"pod1", "namespaceA", "1.1.1.1", v1.PodRunning, types.UID("1"), nil},
				},
				newNodes:                  []nodeDescriptor{},
				newWorkloadResource:       []workloadResourceDescriptor{},
				modifiedPods:              []podDescriptor{},
				modifiedNodes:             []nodeDescriptor{},
				modifiedWorkloadResources: []workloadResourceDescriptor{},
				expectedResolves: map[string]k8s.Workload{
					"1.1.1.1": {
						Name:      "pod1",
						Namespace: "namespaceA",
						Kind:      "pod",
					},
					"1.1.1.2": {
						Name:      "1.1.1.2",
						Namespace: "External",
						Kind:      "External",
					},
				},
			},
			updateSteps: []testStep{
				{
					shouldWait:          false,
					newPods:             []podDescriptor{},
					newNodes:            []nodeDescriptor{},
					newWorkloadResource: []workloadResourceDescriptor{},
					modifiedPods: []podDescriptor{
						{"pod1", "namespaceA", "1.1.1.2", v1.PodRunning, types.UID("1"), nil},
					},
					modifiedNodes:             []nodeDescriptor{},
					modifiedWorkloadResources: []workloadResourceDescriptor{},
					expectedResolves:          map[string]k8s.Workload{},
				},
				{
					shouldWait: true,
					newPods:    []podDescriptor{},
					newNodes: []nodeDescriptor{
						{"Node1", "1.1.1.1", types.UID(uuid.NewString())},
					},
					newWorkloadResource:       []workloadResourceDescriptor{},
					modifiedPods:              []podDescriptor{},
					modifiedNodes:             []nodeDescriptor{},
					modifiedWorkloadResources: []workloadResourceDescriptor{},
					expectedResolves: map[string]k8s.Workload{
						"1.1.1.1": {
							Name:      "Node1",
							Namespace: "Node",
							Kind:      "Node",
						},
						"1.1.1.2": {
							Name:      "pod1",
							Namespace: "namespaceA",
							Kind:      "pod",
						},
					},
				},
			},
		},
		{
			description: "1 node changing ip",
			initialState: testStep{
				shouldWait: false,
				newPods:    []podDescriptor{},
				newNodes: []nodeDescriptor{
					{"Node1", "1.1.1.0", types.UID("1")},
				},
				newWorkloadResource:       []workloadResourceDescriptor{},
				modifiedPods:              []podDescriptor{},
				modifiedNodes:             []nodeDescriptor{},
				modifiedWorkloadResources: []workloadResourceDescriptor{},
				expectedResolves:          map[string]k8s.Workload{},
			},
			updateSteps: []testStep{
				{
					shouldWait:          true,
					newPods:             []podDescriptor{},
					newNodes:            []nodeDescriptor{},
					newWorkloadResource: []workloadResourceDescriptor{},
					modifiedPods:        []podDescriptor{},
					modifiedNodes: []nodeDescriptor{
						{"Node1", "1.1.2.0", types.UID("1")},
					},
					modifiedWorkloadResources: []workloadResourceDescriptor{},
					expectedResolves: map[string]k8s.Workload{
						"1.1.1.0": { // resolver isn't expected to delete old not-reused entries
							Name:      "Node1",
							Namespace: "Node",
							Kind:      "Node",
						},
						"1.1.2.0": {
							Name:      "Node1",
							Namespace: "Node",
							Kind:      "Node",
						},
					},
				},
			},
		},
		{
			description: "1 node changing ip, reused by another node",
			initialState: testStep{
				shouldWait: true,
				newPods:    []podDescriptor{},
				newNodes: []nodeDescriptor{
					{"Node1", "1.1.1.0", types.UID("1")},
				},
				newWorkloadResource:       []workloadResourceDescriptor{},
				modifiedPods:              []podDescriptor{},
				modifiedNodes:             []nodeDescriptor{},
				modifiedWorkloadResources: []workloadResourceDescriptor{},
				expectedResolves:          map[string]k8s.Workload{},
			},
			updateSteps: []testStep{
				{
					shouldWait:          true,
					newPods:             []podDescriptor{},
					newNodes:            []nodeDescriptor{},
					newWorkloadResource: []workloadResourceDescriptor{},
					modifiedPods:        []podDescriptor{},
					modifiedNodes: []nodeDescriptor{
						{"Node1", "1.1.2.0", types.UID("1")},
					},
					modifiedWorkloadResources: []workloadResourceDescriptor{},
					expectedResolves:          map[string]k8s.Workload{},
				},
				{
					shouldWait: true,
					newPods:    []podDescriptor{},
					newNodes: []nodeDescriptor{
						{"Node2", "1.1.1.0", types.UID("2")},
					},
					newWorkloadResource: []workloadResourceDescriptor{},
					modifiedPods:        []podDescriptor{},
					modifiedNodes: []nodeDescriptor{
						{"Node1", "1.1.2.0", types.UID("1")},
					},
					modifiedWorkloadResources: []workloadResourceDescriptor{},
					expectedResolves: map[string]k8s.Workload{
						"1.1.1.0": {
							Name:      "Node2",
							Namespace: "Node",
							Kind:      "Node",
						},
						"1.1.2.0": {
							Name:      "Node1",
							Namespace: "Node",
							Kind:      "Node",
						},
					},
				},
			},
		},
		{
			description: "pod with hostip wont override node",
			initialState: testStep{
				shouldWait: false,
				newPods:    []podDescriptor{},
				newNodes: []nodeDescriptor{
					{"Node1", "1.1.1.0", types.UID(uuid.NewString())},
				},
				newWorkloadResource:       []workloadResourceDescriptor{},
				modifiedPods:              []podDescriptor{},
				modifiedNodes:             []nodeDescriptor{},
				modifiedWorkloadResources: []workloadResourceDescriptor{},
				expectedResolves:          map[string]k8s.Workload{},
			},
			updateSteps: []testStep{
				{
					shouldWait: true,
					newPods: []podDescriptor{
						{"pod1", "namespaceA", "1.1.1.0", v1.PodRunning, types.UID(uuid.New().String()), nil},
					},
					newNodes:                  []nodeDescriptor{},
					newWorkloadResource:       []workloadResourceDescriptor{},
					modifiedPods:              []podDescriptor{},
					modifiedNodes:             []nodeDescriptor{},
					modifiedWorkloadResources: []workloadResourceDescriptor{},
					expectedResolves: map[string]k8s.Workload{
						"1.1.1.0": {
							Name:      "Node1",
							Namespace: "Node",
							Kind:      "Node",
						},
					},
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.description, func(t *testing.T) {
			assert := assert.New(t)
			// Arrange 1: mocks and initial state
			originalObjs := generateClusterObjects(test.initialState.newPods, test.initialState.newWorkloadResource, test.initialState.newNodes)
			fakeClient := testclient.NewSimpleClientset(originalObjs...)
			fakeWatchers := createPrependWatchers(fakeClient)

			resolver, err := k8s.NewK8sIPResolver(fakeClient, false)
			assert.NoError(err)

			// Act 1: process initial state
			err = resolver.StartWatching()
			assert.NoError(err)

			// Assert 1: resolve and compare to expected, original state
			for ipToCheck, expectedWorkload := range test.initialState.expectedResolves {
				resultWorkload := resolver.ResolveIP(ipToCheck)
				assert.Equal(expectedWorkload, resultWorkload)
			}

			for _, step := range test.updateSteps {
				// Arrange 2+n: update the state via watchers
				for _, newPod := range step.newPods {
					podObj := generatePod(newPod)
					addObject(fakeWatchers, podObj, "Pod")
				}
				for _, newWorkloadResource := range step.newWorkloadResource {
					obj := generateWorkloadResource(newWorkloadResource)
					addObject(fakeWatchers, obj, newWorkloadResource.Kind)
				}
				for _, newNode := range step.newNodes {
					obj := generateNode(newNode)
					addObject(fakeWatchers, obj, "Node")
				}
				for _, modifiedPod := range step.modifiedPods {
					podObj := generatePod(modifiedPod)
					modifyObject(fakeWatchers, podObj, "Pod")
				}
				for _, modifiedWorkloadResource := range step.newWorkloadResource {
					obj := generateWorkloadResource(modifiedWorkloadResource)
					modifyObject(fakeWatchers, obj, modifiedWorkloadResource.Kind)
				}
				for _, modifiedNode := range step.modifiedNodes {
					obj := generateNode(modifiedNode)
					modifyObject(fakeWatchers, obj, "Node")
				}

				if step.shouldWait {
					time.Sleep(1 * time.Second)
				}

				// Act+Assert 2+n
				for ipToResolve, expectedWorkload := range step.expectedResolves {
					assert.Equal(expectedWorkload, resolver.ResolveIP(ipToResolve))
				}

			}
		})
	}
}

func TestPodNameResolving(t *testing.T) {
	assert := assert.New(t)
	workloadResources := []workloadResourceDescriptor{
		{"deployment1", "namespaceA", types.UID(uuid.NewString()), "Deployment"},
		{"replicaset1", "namespaceB", types.UID(uuid.NewString()), "ReplicaSet"},
	}
	pods := []podDescriptor{
		{
			Name:       "pod1",
			Namespace:  "namespaceA",
			IP:         "1.1.1.1",
			Phase:      v1.PodRunning,
			UID:        types.UID(uuid.NewString()),
			Controller: &workloadResources[0],
		},
	}
	nodes := []nodeDescriptor{
		{"Node1", "1.1.1.0", types.UID(uuid.NewString())},
	}
	clusterObjs := generateClusterObjects(pods, workloadResources, nodes)
	fakeClient := testclient.NewSimpleClientset(clusterObjs...)

	resolver, err := k8s.NewK8sIPResolver(fakeClient, false)
	if err != nil {
		t.Fatalf("Error creating resolver %v", err)
	}

	// call the tested function
	err = resolver.StartWatching()
	if err != nil {
		t.Fatalf("Error in StartWatching")
	}

	t.Log(resolver.ResolveIP(pods[0].IP)) // TODO change to compare, should be deployment1
	workload := resolver.ResolveIP(pods[0].IP)
	assert.Equal(workloadResources[0].Name, workload.Name, "Incorrect IP resolving")
	assert.Equal(workloadResources[0].Namespace, workload.Namespace, "Incorrect IP resolving")
	assert.Equal(workloadResources[0].Kind, workload.Kind, "Incorrect IP resolving")
}
