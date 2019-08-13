package cache

import "sync"

func (g *Group) Reorder() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	for i := 0; i < len(g.list); i++ {
		g.list[i].rwmu.Lock()
		defer g.list[i].rwmu.Unlock()
	}

	newg := &Group{
		pool:     g.pool,
		size:     g.size,
		freesize: g.size,
		block: &indicator{
			start: 0,
			end:   g.size,
			next:  nil,
		},
		mu:   sync.Mutex{},
		list: g.list,
	}
	// Add all data back.
	for i := 0; i < len(g.list); i++ {
		if m, err := newg.Put(g.list[i].Dump()); err != nil {
			return err
		} else {
			nblock := *(m.block)
			newg.list[i].block = &nblock
		}
	}
	g = newg
	return nil
}
