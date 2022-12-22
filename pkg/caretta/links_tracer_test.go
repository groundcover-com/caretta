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
func (resolver *MockResolver) StopWatching() {

}

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

func TestAggregationClient(t *testing.T) {
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

	clientIp := 1
	serverIp := 2
	conn1 := caretta.ConnectionIdentifier{
		Id:  1,
		Pid: 1,
		Tuple: caretta.ConnectionTuple{
			SrcIp:   uint32(clientIp),
			DstIp:   uint32(serverIp),
			SrcPort: 55555,
			DstPort: 80,
		},
		Role: 1,
	}
	throughput1 := caretta.ConnectionThroughputStats{
		BytesSent:     10,
		BytesReceived: 0,
		IsActive:      1,
	}
	m.Update(conn1, throughput1, ebpf.UpdateAny)

	tracer := caretta.NewTracerWithObjs(&MockResolver{}, m, nil)

	pastLinks := make(map[caretta.NetworkLink]uint64)

	_, currentLinks := tracer.TracesPollingIteration(pastLinks)
	assert.True(isLinkInMap(clientIp, serverIp, currentLinks))
	assert.False(isLinkInMap(serverIp, clientIp, currentLinks))
}

func TestAggregationServer(t *testing.T) {
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

	clientIp := 1
	serverIp := 2
	conn1 := caretta.ConnectionIdentifier{
		Id:  1,
		Pid: 1,
		Tuple: caretta.ConnectionTuple{
			SrcIp:   uint32(serverIp),
			DstIp:   uint32(clientIp),
			SrcPort: 80,
			DstPort: 55555,
		},
		Role: caretta.ServerConnectionRole,
	}
	throughput1 := caretta.ConnectionThroughputStats{
		BytesSent:     10,
		BytesReceived: 0,
		IsActive:      1,
	}
	m.Update(conn1, throughput1, ebpf.UpdateAny)

	tracer := caretta.NewTracerWithObjs(&MockResolver{}, m, nil)

	pastLinks := make(map[caretta.NetworkLink]uint64)

	_, currentLinks := tracer.TracesPollingIteration(pastLinks)
	assert.True(isLinkInMap(clientIp, serverIp, currentLinks))
	assert.False(isLinkInMap(serverIp, clientIp, currentLinks))
}

func TestAggregationInactive(t *testing.T) {
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

	clientIp := 1
	serverIp := 2
	firstThroughputSize := 10
	inactiveThroughputSIze := 20
	thirdThroughputSize := 15

	conn1 := caretta.ConnectionIdentifier{
		Id:  1,
		Pid: 1,
		Tuple: caretta.ConnectionTuple{
			SrcIp:   uint32(serverIp),
			DstIp:   uint32(clientIp),
			SrcPort: 80,
			DstPort: 55555,
		},
		Role: caretta.ServerConnectionRole,
	}
	throughput1 := caretta.ConnectionThroughputStats{
		BytesSent:     uint64(firstThroughputSize),
		BytesReceived: 0,
		IsActive:      1,
	}
	m.Update(conn1, throughput1, ebpf.UpdateAny)

	tracer := caretta.NewTracerWithObjs(&MockResolver{}, m, nil)

	pastLinks := make(map[caretta.NetworkLink]uint64)

	pastLinks, currentLinks := tracer.TracesPollingIteration(pastLinks)
	assert.True(isLinkInMap(clientIp, serverIp, currentLinks))
	assert.False(isLinkInMap(serverIp, clientIp, currentLinks))

	// make sure connection is still in map

	var resultThroughput caretta.ConnectionThroughputStats
	err = m.Lookup(&conn1, &resultThroughput)
	assert.NoError(err)

	// update the throughput so the connection is inactive
	throughput2 := caretta.ConnectionThroughputStats{
		BytesSent:     uint64(inactiveThroughputSIze),
		BytesReceived: 0,
		IsActive:      0,
	}
	m.Update(conn1, throughput2, ebpf.UpdateAny)
	pastLinks, currentLinks = tracer.TracesPollingIteration(pastLinks)

	// check the past connection is both in past links and in current links
	assert.True(isLinkInMap(clientIp, serverIp, pastLinks))
	assert.True(isLinkInMap(clientIp, serverIp, currentLinks))
	// check connection is deleted from ebpf map
	err = m.Lookup(&conn1, &resultThroughput)
	assert.Error(err)

	// new connection, same link
	throughput3 := caretta.ConnectionThroughputStats{
		BytesSent:     uint64(thirdThroughputSize),
		BytesReceived: 0,
		IsActive:      1,
	}
	m.Update(conn1, throughput3, ebpf.UpdateAny)
	_, currentLinks = tracer.TracesPollingIteration(pastLinks)
	assert.Equal(uint64(inactiveThroughputSIze+thirdThroughputSize), getThroughputFromMap(clientIp, serverIp, currentLinks))
}
