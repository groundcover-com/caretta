package caretta_test

import (
	"testing"

	"github.com/groundcover-com/caretta/pkg/caretta"

	"github.com/cilium/ebpf"
	"github.com/groundcover-com/caretta/pkg/k8s"
	"github.com/stretchr/testify/assert"
)

type MockResolver struct{}

func (resolver *MockResolver) ResolveIP(ip string) k8s.Workload {
	return k8s.Workload{
		Name:      ip,
		Namespace: "Namespace",
		Kind:      "Kind",
	}
}

func (resolver *MockResolver) StartWatching() error {
	return nil
}
func (resolver *MockResolver) StopWatching() {}

func isLinkInMap(clientIp int, serverIp int, linksMap map[caretta.NetworkLink]uint64) bool {
	for link := range linksMap {
		if link.Client.Name == caretta.IP(clientIp).String() && link.Server.Name == caretta.IP(serverIp).String() {
			return true
		}
	}
	return false
}

func getThroughputFromMap(clientIp int, serverIp int, linksMap map[caretta.NetworkLink]uint64) uint64 {
	for link, throughput := range linksMap {
		if link.Client.Name == caretta.IP(clientIp).String() && link.Server.Name == caretta.IP(serverIp).String() {
			return throughput
		}
	}
	return 0
}

func createMap() (*ebpf.Map, error) {
	return ebpf.NewMap(&ebpf.MapSpec{
		Name:       "ConnectionsMock",
		Type:       ebpf.Hash,
		KeySize:    24,
		ValueSize:  24,
		MaxEntries: 8,
	})
}

type testConnection struct {
	connId     caretta.ConnectionIdentifier
	throughput caretta.ConnectionThroughputStats
}

type aggregationTest struct {
	description        string
	connections        []testConnection
	expectedLink       caretta.NetworkLink
	expectedThroughput uint64
}

var clientTuple = caretta.ConnectionTuple{
	SrcIp:   1,
	DstIp:   2,
	SrcPort: 55555,
	DstPort: 80,
}
var serverTuple = caretta.ConnectionTuple{
	DstIp:   1,
	SrcIp:   2,
	DstPort: 55555,
	SrcPort: 80,
}
var activeThroughput = caretta.ConnectionThroughputStats{
	BytesSent:     10,
	BytesReceived: 2,
	IsActive:      1,
}
var inactiveThroughput = caretta.ConnectionThroughputStats{
	BytesSent:     10,
	BytesReceived: 2,
	IsActive:      0,
}
var clientLink = caretta.NetworkLink{
	Client: k8s.Workload{
		Name:      caretta.IP(1).String(),
		Namespace: "Namespace",
		Kind:      "Kind",
	},
	Server: k8s.Workload{
		Name:      caretta.IP(2).String(),
		Namespace: "Namespace",
		Kind:      "Kind",
	},
	ServerPort: 80,
	Role:       caretta.ClientConnectionRole,
}
var serverLink = caretta.NetworkLink{
	Client: k8s.Workload{
		Name:      caretta.IP(1).String(),
		Namespace: "Namespace",
		Kind:      "Kind",
	},
	Server: k8s.Workload{
		Name:      caretta.IP(2).String(),
		Namespace: "Namespace",
		Kind:      "Kind",
	},
	ServerPort: 80,
	Role:       caretta.ServerConnectionRole,
}
var aggregationTests = []aggregationTest{
	{
		description: "single client connection",
		connections: []testConnection{
			{
				connId: caretta.ConnectionIdentifier{
					Id:    1,
					Pid:   1,
					Tuple: clientTuple,
					Role:  caretta.ClientConnectionRole,
				},
				throughput: activeThroughput,
			},
		},
		expectedLink:       clientLink,
		expectedThroughput: 10,
	},
	{
		description: "single server connection",
		connections: []testConnection{
			{
				connId: caretta.ConnectionIdentifier{
					Id:    1,
					Pid:   1,
					Tuple: serverTuple,
					Role:  caretta.ServerConnectionRole,
				},
				throughput: activeThroughput,
			},
		},
		expectedLink:       serverLink,
		expectedThroughput: 10,
	},
	{
		description: "2 client connections",
		connections: []testConnection{
			{
				connId: caretta.ConnectionIdentifier{
					Id:    1,
					Pid:   1,
					Tuple: clientTuple,
					Role:  caretta.ClientConnectionRole,
				},
				throughput: activeThroughput,
			},
			{
				connId: caretta.ConnectionIdentifier{
					Id:    2,
					Pid:   1,
					Tuple: clientTuple,
					Role:  caretta.ClientConnectionRole,
				},
				throughput: activeThroughput,
			},
		},
		expectedLink:       clientLink,
		expectedThroughput: 20,
	},
	{
		description: "2 server connections",
		connections: []testConnection{
			{
				connId: caretta.ConnectionIdentifier{
					Id:    1,
					Pid:   1,
					Tuple: serverTuple,
					Role:  caretta.ServerConnectionRole,
				},
				throughput: activeThroughput,
			},
			{
				connId: caretta.ConnectionIdentifier{
					Id:    2,
					Pid:   1,
					Tuple: serverTuple,
					Role:  caretta.ServerConnectionRole,
				},
				throughput: activeThroughput,
			},
		},
		expectedLink:       serverLink,
		expectedThroughput: 20,
	},
	{
		description: "3 active client connections, 2 inactive",
		connections: []testConnection{
			{
				connId: caretta.ConnectionIdentifier{
					Id:    1,
					Pid:   1,
					Tuple: clientTuple,
					Role:  caretta.ClientConnectionRole,
				},
				throughput: activeThroughput,
			},
			{
				connId: caretta.ConnectionIdentifier{
					Id:    2,
					Pid:   1,
					Tuple: clientTuple,
					Role:  caretta.ClientConnectionRole,
				},
				throughput: activeThroughput,
			},
			{
				connId: caretta.ConnectionIdentifier{
					Id:    3,
					Pid:   1,
					Tuple: clientTuple,
					Role:  caretta.ClientConnectionRole,
				},
				throughput: activeThroughput,
			},
			{
				connId: caretta.ConnectionIdentifier{
					Id:    4,
					Pid:   1,
					Tuple: clientTuple,
					Role:  caretta.ClientConnectionRole,
				},
				throughput: inactiveThroughput,
			},
			{
				connId: caretta.ConnectionIdentifier{
					Id:    5,
					Pid:   1,
					Tuple: clientTuple,
					Role:  caretta.ClientConnectionRole,
				},
				throughput: inactiveThroughput,
			},
		},
		expectedLink:       clientLink,
		expectedThroughput: 50,
	},
	{
		description: "3 active server connections, 2 inactive",
		connections: []testConnection{
			{
				connId: caretta.ConnectionIdentifier{
					Id:    1,
					Pid:   1,
					Tuple: serverTuple,
					Role:  caretta.ServerConnectionRole,
				},
				throughput: activeThroughput,
			},
			{
				connId: caretta.ConnectionIdentifier{
					Id:    2,
					Pid:   1,
					Tuple: serverTuple,
					Role:  caretta.ServerConnectionRole,
				},
				throughput: activeThroughput,
			},
			{
				connId: caretta.ConnectionIdentifier{
					Id:    3,
					Pid:   1,
					Tuple: serverTuple,
					Role:  caretta.ServerConnectionRole,
				},
				throughput: activeThroughput,
			},
			{
				connId: caretta.ConnectionIdentifier{
					Id:    4,
					Pid:   1,
					Tuple: serverTuple,
					Role:  caretta.ServerConnectionRole,
				},
				throughput: inactiveThroughput,
			},
			{
				connId: caretta.ConnectionIdentifier{
					Id:    5,
					Pid:   1,
					Tuple: serverTuple,
					Role:  caretta.ServerConnectionRole,
				},
				throughput: inactiveThroughput,
			},
		},
		expectedLink:       serverLink,
		expectedThroughput: 50,
	},
}

func TestAggregations(t *testing.T) {
	for _, test := range aggregationTests {
		t.Run(test.description, func(t *testing.T) {
			assert := assert.New(t)
			m, err := createMap()
			assert.NoError(err)
			defer m.Close()

			tracer := caretta.NewTracerWithObjs(&MockResolver{}, m, nil)
			pastLinks := make(map[caretta.NetworkLink]uint64)
			var currentLinks map[caretta.NetworkLink]uint64
			for _, connection := range test.connections {
				m.Update(connection.connId, connection.throughput, ebpf.UpdateAny)
				_, currentLinks = tracer.TracesPollingIteration(pastLinks)
			}
			resultThroughput, ok := currentLinks[test.expectedLink]
			assert.True(ok, "expected link not in result map")
			assert.Equal(test.expectedThroughput, resultThroughput, "wrong throughput value")
		})

	}
}

func TestDeletion(t *testing.T) {
	assert := assert.New(t)
	m, err := ebpf.NewMap(&ebpf.MapSpec{
		Name:       "ConnectionsMock",
		Type:       ebpf.Hash,
		KeySize:    24,
		ValueSize:  24,
		MaxEntries: 8,
	})
	assert.NoError(err)
	defer m.Close()

	conn1 := caretta.ConnectionIdentifier{
		Id:    1,
		Pid:   1,
		Tuple: serverTuple,
		Role:  caretta.ServerConnectionRole,
	}
	throughput1 := activeThroughput
	m.Update(conn1, throughput1, ebpf.UpdateAny)

	tracer := caretta.NewTracerWithObjs(&MockResolver{}, m, nil)

	pastLinks := make(map[caretta.NetworkLink]uint64)

	pastLinks, currentLinks := tracer.TracesPollingIteration(pastLinks)
	resultThroughput, ok := currentLinks[serverLink]
	assert.True(ok, "link not in map, map is %v", currentLinks)
	assert.Equal(throughput1.BytesSent, resultThroughput)

	// make sure connection is still in map

	err = m.Lookup(&conn1, &resultThroughput)
	assert.NoError(err)

	// update the throughput so the connection is inactive
	throughput2 := inactiveThroughput
	m.Update(conn1, throughput2, ebpf.UpdateAny)
	pastLinks, currentLinks = tracer.TracesPollingIteration(pastLinks)

	// check the past connection is both in past links and in current links
	resultThroughput, ok = currentLinks[serverLink]
	assert.True(ok, "link not in map, map is %v", currentLinks)
	assert.Equal(throughput1.BytesSent, resultThroughput)
	_, ok = pastLinks[serverLink]
	assert.True(ok, "inactive link not in past links: %v", pastLinks)
	// check connection is deleted from ebpf map
	err = m.Lookup(&conn1, &resultThroughput)
	assert.Error(err, "inactive connection not deleted from connections map")

	// new connection, same link
	throughput3 := activeThroughput
	m.Update(conn1, throughput3, ebpf.UpdateAny)
	_, currentLinks = tracer.TracesPollingIteration(pastLinks)
	resultThroughput, ok = currentLinks[serverLink]
	assert.True(ok, "link not in map, map is %v", currentLinks)
	assert.Equal(throughput1.BytesSent+throughput3.BytesSent, resultThroughput)
}
