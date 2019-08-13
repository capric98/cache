package cache

import (
	"fmt"
	"reflect"
	"runtime"
	"sync"
	"unsafe"
)

const (
	maxGroupSize = 256 * 1024 * 1024
)

var (
	ErrDivisible    = fmt.Errorf("cache: PoolSize must be divisible by GroupSize.")
	ErrInvalidGSize = fmt.Errorf("cache: GroupSize must be between 0 and %d", maxGroupSize+1)
	ErrNoSpareSpace = fmt.Errorf("cache: No spare space!")
	ErrMNotExist    = fmt.Errorf("cache: Object not found.")
)

type indicator struct {
	start, end int64
	next       *indicator
}

type Pool struct {
	pool      []byte
	size      int64
	groupsize int64
	block     *indicator
	mu        sync.Mutex
}

type Group struct {
	pool     []byte
	size     int64
	freesize int64
	block    *indicator
	mu       sync.Mutex
	list     []*manifest
}

type manifest struct {
	body  []byte
	block *indicator
	len   int64
	rwmu  sync.RWMutex
}

func NewPool(psize int64, gsize int64) (*Pool, error) {
	if psize%gsize != 0 {
		return nil, ErrDivisible
	}
	if gsize > maxGroupSize {
		return nil, ErrInvalidGSize
	}
	np := &Pool{
		pool:      make([]byte, psize),
		size:      psize,
		groupsize: gsize,
		block: &indicator{
			start: 0,
			end:   gsize,
			next:  nil,
		},
		mu: sync.Mutex{},
	}

	p := np.block
	for i := gsize; i < psize; i += gsize {
		p.next = &indicator{
			start: i,
			end:   i + gsize,
			next:  nil,
		}
		p = p.next
	}
	return np, nil
}

func (p *Pool) NewGroup() (*Group, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.block == nil {
		return nil, ErrNoSpareSpace
	}

	ng := &Group{
		pool:     p.pool[p.block.start:p.block.end],
		size:     p.groupsize,
		freesize: p.groupsize,
		block: &indicator{
			start: 0,
			end:   p.groupsize,
			next:  nil,
		},
		mu:   sync.Mutex{},
		list: make([]*manifest, 0, p.groupsize/64),
	}
	return ng, nil
}

func (g *Group) Put(data interface{}) (*manifest, error) {
	len := int64(unsafe.Sizeof(data))
	g.mu.Lock()
	defer g.mu.Unlock()
	if len > g.freesize {
		return nil, ErrNoSpareSpace
	}

	g.freesize -= len

	nm := &manifest{
		body:  g.pool,
		block: &indicator{},
		len:   len,
		rwmu:  sync.RWMutex{},
	}

	us := unsafe.Pointer(&data)
	fakeslice := reflect.SliceHeader{
		Data: uintptr(us),
		Len:  int(len),
		Cap:  int(len),
	}

	p := nm.block
	for len != 0 {
		cblk := g.block
		n := copy(g.pool[cblk.start:cblk.end], *(*[]byte)(unsafe.Pointer(&fakeslice)))
		p.start = cblk.start
		if int64(n) == len {
			p.end = cblk.start + int64(n)
			p.next = nil
			g.block.start = p.end
		} else {
			p.end = cblk.end
			g.block = g.block.next
			p.next = &indicator{}
		}
		len -= int64(n)
	}
	copy(g.pool, *(*[]byte)(unsafe.Pointer(&fakeslice)))
	g.list = append(g.list, nm)
	return nm, nil
}

func (m *manifest) Dump() (interface{}, chan bool) {
	m.rwmu.RLock()
	defer m.rwmu.RUnlock()
	ack := make(chan bool)

	if m.block.next == nil {
		go throwAck(ack)
		return (*(*interface{})(unsafe.Pointer(&m.body[m.block.start]))), ack
	} else {
		a := make([]byte, 0, m.len)
		for p := m.block; p.next != nil; p = p.next {
			a = append(a, m.body[p.start:p.end]...)
		}
		go keepAlive(a, ack) // In case of GC.
		return (*(*interface{})(unsafe.Pointer(&a[0]))), ack
	}
}

func (g *Group) Delete(m *manifest) error {
	i := 0
	for ; i < len(g.list); i++ {
		if g.list[i] == m {
			break
		}
	}
	if i == len(g.list) {
		return ErrMNotExist
	}
	waitD := g.list[i]
	waitD.rwmu.Lock()
	defer waitD.rwmu.Unlock()
	p := waitD.block
	for p != nil {
		g.returnspace(p.start, p.end)
		p = p.next
	}
	g.reunion()
	g.freesize += int64(waitD.len)
	return nil
}

func (g *Group) returnspace(start, end int64) {
	if end <= g.block.start {
		g.block = &indicator{
			start: start,
			end:   end,
			next:  g.block,
		}
		return
	}
	p := g.block
	for p != nil {
		if p.end <= start {
			p.next = &indicator{
				start: start,
				end:   end,
				next:  p.next,
			}
			return
		}
	}
}

func (g *Group) reunion() {
	p := g.block
	for p.next != nil {
		if p.end == p.next.start {
			p.end = p.next.end
			p.next = p.next.next
		}
		p = p.next
	}
}

func keepAlive(a []byte, ack chan bool) {
	<-ack
	runtime.KeepAlive(a)
}

func throwAck(ack chan bool) {
	<-ack
}
