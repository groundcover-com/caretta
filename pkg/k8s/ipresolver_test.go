package k8s_test

import (
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

func (desc *workloadResourceDescriptor) CrateObject() runtime.Object {
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

func generateClusterObjects(pods []podDescriptor, workloadsResources []workloadResourceDescriptor, nodes []nodeDescriptor) []runtime.Object {
	result := make([]runtime.Object, 0, len(pods)+len(workloadsResources)+len(nodes))
	for _, pod := range pods {
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
		result = append(result, &newPod)
	}
	for _, desc := range workloadsResources {
		result = append(result, desc.CrateObject())
	}
	for _, node := range nodes {
		result = append(result, &v1.Node{
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
		})
	}
	return result
}

func TestInitialSnapshot(t *testing.T) {
	assert := assert.New(t)
	// create mocks
	pods := []podDescriptor{
		{"pod1", "namespaceA", "1.1.1.1", v1.PodRunning, types.UID(uuid.New().String()), nil},
		{"pod2", "namespaceA", "1.1.1.2", v1.PodRunning, types.UID(uuid.New().String()), nil},
		{"pod3", "namespaceA", "1.1.1.3", v1.PodRunning, types.UID(uuid.New().String()), nil},
	}
	workloadResources := []workloadResourceDescriptor{
		{"deployment1", "namespaceA", types.UID(uuid.NewString()), "Deployment"},
		{"replicaset1", "namespaceB", types.UID(uuid.NewString()), "ReplicaSet"},
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

	for _, pod := range pods {
		workload := resolver.ResolveIP(pod.IP)
		assert.Equal(pod.Name, workload.Name, "Incorrect IP resolving")
		assert.Equal(pod.Namespace, workload.Namespace, "Incorrect IP resolving")
		assert.Equal("pod", workload.Kind, "Incorrect IP resolving")
	}

}

func TestWatchersAddModify(t *testing.T) {
	assert := assert.New(t)
	// create mocks - initial state
	pods := []podDescriptor{
		{"pod1", "namespaceA", "1.1.1.1", v1.PodRunning, types.UID(uuid.New().String()), nil},
		{"pod2", "namespaceA", "1.1.1.2", v1.PodRunning, types.UID(uuid.New().String()), nil},
		{"pod3", "namespaceA", "1.1.1.3", v1.PodRunning, types.UID(uuid.New().String()), nil},
	}
	workloadResources := []workloadResourceDescriptor{
		{"deployment1", "namespaceA", types.UID(uuid.NewString()), "Deployment"},
		{"replicaset1", "namespaceB", types.UID(uuid.NewString()), "ReplicaSet"},
	}
	nodes := []nodeDescriptor{
		{"Node1", "1.1.1.0", types.UID(uuid.NewString())},
	}
	clusterObjs := generateClusterObjects(pods, workloadResources, nodes)
	fakeClient := testclient.NewSimpleClientset(clusterObjs...)
	fakeWatch := watch.NewFake()
	fakeClient.PrependWatchReactor("pods", k8stesting.DefaultWatchReactor(fakeWatch, nil))

	resolver, err := k8s.NewK8sIPResolver(fakeClient, false)
	if err != nil {
		t.Fatalf("Error creating resolver %v", err)
	}
	// set initial state
	err = resolver.StartWatching()
	if err != nil {
		t.Fatalf("Error in StartWatching")
	}

	newPods := []podDescriptor{
		{"pod4", "namespaceA", "", v1.PodRunning, types.UID(uuid.NewString()), nil},
		{"pod5", "namespaceB", "", v1.PodRunning, types.UID(uuid.NewString()), nil},
	}
	newObjs := generateClusterObjects(newPods, []workloadResourceDescriptor{}, []nodeDescriptor{})

	fakeWatch.Add(newObjs[0])
	fakeWatch.Add(newObjs[1])

	// assign IPs
	newPods[0].IP = "1.1.1.4"
	newPods[1].IP = "1.1.2.1"
	modifiedObjs := generateClusterObjects(newPods, []workloadResourceDescriptor{}, []nodeDescriptor{})
	fakeWatch.Modify(modifiedObjs[0])
	fakeWatch.Modify(modifiedObjs[1])

	time.Sleep(1 * time.Second)

	for _, pod := range newPods {
		workload := resolver.ResolveIP(pod.IP)
		assert.Equal(pod.Name, workload.Name, "Incorrect IP resolving")
		assert.Equal(pod.Namespace, workload.Namespace, "Incorrect IP resolving")
		assert.Equal("pod", workload.Kind, "Incorrect IP resolving")
	}

}

func TestWatchOverride(t *testing.T) {
	assert := assert.New(t)
	// create mocks - initial state
	pods := []podDescriptor{
		{"pod1", "namespaceA", "1.1.1.1", v1.PodRunning, types.UID(uuid.New().String()), nil},
		{"pod2", "namespaceA", "1.1.1.2", v1.PodRunning, types.UID(uuid.New().String()), nil},
		{"pod3", "namespaceA", "1.1.1.3", v1.PodRunning, types.UID(uuid.New().String()), nil},
	}
	workloadResources := []workloadResourceDescriptor{
		{"deployment1", "namespaceA", types.UID(uuid.NewString()), "Deployment"},
		{"replicaset1", "namespaceB", types.UID(uuid.NewString()), "ReplicaSet"},
	}
	nodes := []nodeDescriptor{
		{"Node1", "1.1.1.0", types.UID(uuid.NewString())},
	}
	clusterObjs := generateClusterObjects(pods, workloadResources, nodes)
	fakeClient := testclient.NewSimpleClientset(clusterObjs...)
	fakeWatch := watch.NewFake()
	fakeClient.PrependWatchReactor("pods", k8stesting.DefaultWatchReactor(fakeWatch, nil))

	resolver, err := k8s.NewK8sIPResolver(fakeClient, false)
	if err != nil {
		t.Fatalf("Error creating resolver %v", err)
	}
	// set initial state
	err = resolver.StartWatching()
	if err != nil {
		t.Fatalf("Error in StartWatching")
	}

	newPods := []podDescriptor{
		{"pod4", "namespaceA", "1.1.1.2", v1.PodRunning, types.UID(uuid.NewString()), nil},
	}
	newObjs := generateClusterObjects(newPods, []workloadResourceDescriptor{}, []nodeDescriptor{})

	fakeWatch.Add(newObjs[0])
	time.Sleep(1 * time.Second)
	workload := resolver.ResolveIP(newPods[0].IP)
	assert.Equal(newPods[0].Name, workload.Name, "Incorrect IP resolving")
	assert.Equal(newPods[0].Namespace, workload.Namespace, "Incorrect IP resolving")
	assert.Equal("pod", workload.Kind, "Incorrect IP resolving")
}

func TestWatchOverrideNode(t *testing.T) {
	assert := assert.New(t)
	// create mocks - initial state
	pods := []podDescriptor{
		{"pod1", "namespaceA", "1.1.1.1", v1.PodRunning, types.UID(uuid.New().String()), nil},
		{"pod2", "namespaceA", "1.1.1.2", v1.PodRunning, types.UID(uuid.New().String()), nil},
		{"pod3", "namespaceA", "1.1.1.3", v1.PodRunning, types.UID(uuid.New().String()), nil},
	}
	workloadResources := []workloadResourceDescriptor{
		{"deployment1", "namespaceA", types.UID(uuid.NewString()), "Deployment"},
		{"replicaset1", "namespaceB", types.UID(uuid.NewString()), "ReplicaSet"},
	}
	nodes := []nodeDescriptor{
		{"Node1", "1.1.1.0", types.UID(uuid.NewString())},
	}
	clusterObjs := generateClusterObjects(pods, workloadResources, nodes)
	fakeClient := testclient.NewSimpleClientset(clusterObjs...)
	fakePodsWatch := watch.NewFake()
	fakeNodesWatch := watch.NewFake()
	fakeClient.PrependWatchReactor("pods", k8stesting.DefaultWatchReactor(fakePodsWatch, nil))
	fakeClient.PrependWatchReactor("nodes", k8stesting.DefaultWatchReactor(fakeNodesWatch, nil))

	resolver, err := k8s.NewK8sIPResolver(fakeClient, false)
	if err != nil {
		t.Fatalf("Error creating resolver %v", err)
	}
	// set initial state
	err = resolver.StartWatching()
	if err != nil {
		t.Fatalf("Error in StartWatching")
	}

	// pod releases its IP
	modifiedPod := pods[0]
	ip1 := modifiedPod.IP
	ip2 := "1.1.2.2"
	modifiedPod.IP = ip2
	fakePodsWatch.Modify(generateClusterObjects([]podDescriptor{modifiedPod}, []workloadResourceDescriptor{}, []nodeDescriptor{})[0])

	time.Sleep(1 * time.Second)

	workload := resolver.ResolveIP(ip2)
	assert.Equal(modifiedPod.Name, workload.Name, "Incorrect IP resolving")
	assert.Equal(modifiedPod.Namespace, workload.Namespace, "Incorrect IP resolving")
	assert.Equal("pod", workload.Kind, "Incorrect IP resolving")

	// let a node take it
	newNode := nodeDescriptor{"Node2", ip1, types.UID(uuid.NewString())}
	fakeNodesWatch.Add(generateClusterObjects([]podDescriptor{}, []workloadResourceDescriptor{}, []nodeDescriptor{newNode})[0])

	time.Sleep(1 * time.Second)

	workload = resolver.ResolveIP(ip1)
	assert.Equal(newNode.Name, workload.Name, "Incorrect IP resolving")
	assert.Equal("Node", workload.Kind, "Incorrect IP resolving")

	// new pod with hostIP
	newPod := podDescriptor{"pod6", "namespaceC", ip1, v1.PodRunning, types.UID(uuid.NewString()), nil}
	fakePodsWatch.Add(generateClusterObjects([]podDescriptor{newPod}, []workloadResourceDescriptor{}, []nodeDescriptor{})[0])

	time.Sleep(1 * time.Second)

	workload = resolver.ResolveIP(ip1)
	assert.Equal(newNode.Name, workload.Name, "Incorrect IP resolving")
	assert.Equal("Node", workload.Kind, "Incorrect IP resolving")
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
