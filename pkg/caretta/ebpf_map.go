package caretta

import "github.com/cilium/ebpf"

type ConnectionsMapIterator interface {
	Next(interface{}, interface{}) bool
}

type ConnectionsMap interface {
	Lookup(interface{}, interface{}) error
	Iterate() ConnectionsMapIterator
	Delete(interface{}) error
}

type EbpfMap struct {
	innerMap *ebpf.Map
}

type EbpfMapItertor struct {
	innerIterator *ebpf.MapIterator
}

func (m *EbpfMap) Lookup(key interface{}, val interface{}) error {
	return m.innerMap.Lookup(key, val)
}

func (m *EbpfMap) Iterate() ConnectionsMapIterator {
	return &EbpfMapItertor{innerIterator: m.innerMap.Iterate()}
}

func (m *EbpfMap) Delete(key interface{}) error {
	return m.innerMap.Delete(key)
}

func (it *EbpfMapItertor) Next(key interface{}, val interface{}) bool {
	return it.innerIterator.Next(key, val)
}
