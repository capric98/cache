package cache

import (
	"sync"
)

func (g *Group) Reorder() error {
	g.mu.Lock()
	defer g.mu.Unlock()

	tmpPool := make([]byte, g.size)
	// Copy old pool to tmpPool in order.

	newg := &Group{
		pool:     tmpPool,
		size:     g.size,
		freesize: g.size,
		block: &indicator{
			start: 0,
			end:   g.size,
			next:  nil,
		},
		mu:   sync.Mutex{},
		list: make([]*Manifest, 0, len(g.list)+64),
	}

	// Dump all data.
	for i := 0; i < len(g.list); i++ {
		iface, ack := g.list[i].Dump()
		if _, err := newg.Put(iface); err != nil {
			ack <- true
			return err
		}
		ack <- true
	}
	// Copy back.
	for i := 0; i < len(g.list); i++ {
		g.list[i].rwmu.Lock()
		defer g.list[i].rwmu.Unlock()
		// Lock all RWMutex.
	}
	_ = copy(g.pool, tmpPool)
	g.block = newg.block
	for i := 0; i < len(g.list); i++ {
		g.list[i] = newg.list[i]
	}

	return nil
}
