package cache

import (
	"fmt"
	"reflect"
	"runtime"
	"strconv"
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
	ErrGNotExist    = fmt.Errorf("cache: Group not found.")
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
	list     []*Manifest
}

type Manifest struct {
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

	p.block = p.block.next

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
		list: make([]*Manifest, 0, p.groupsize/64),
	}
	return ng, nil
}

func (p *Pool) DeleteGroup(g *Group) error {
	gAddress, gerr := strconv.ParseInt(fmt.Sprintf("%v", &(g.pool[0])), 0, 64)
	pAddress, perr := strconv.ParseInt(fmt.Sprintf("%v", &(p.pool[0])), 0, 64)
	if gerr != nil || perr != nil {
		return fmt.Errorf("cache: Address parse error with g:%v,p:%v", gerr, perr)
	}

	offset := gAddress - pAddress
	if offset < 0 || offset > p.size-p.groupsize {
		return ErrGNotExist
	}

	g.mu.Lock()
	for i := 0; i < len(g.list); i++ {
		g.list[i].rwmu.Lock()
		// Would not return these lock.
	}

	if p.block == nil {
		// All groups in Pool have been allocated,
		// return directly.
		p.block = &indicator{
			start: offset,
			end:   offset + p.groupsize,
			next:  nil,
		}
	} else {
		// Still have vacancy, find the last vacancy,
		// and return space after it.
		pp := p.block
		for pp.next != nil {
			pp = pp.next
		}
		pp.next = &indicator{
			start: offset,
			end:   offset + p.groupsize,
			next:  nil,
		}
	}
	g = nil // Point to nil and wait to GC.
	return nil
}

func (g *Group) Put(data interface{}) (*Manifest, error) {
	len := int64(unsafe.Sizeof(data))
	g.mu.Lock()
	defer g.mu.Unlock()
	if len > g.freesize {
		return nil, ErrNoSpareSpace
	}

	g.freesize -= len

	nm := &Manifest{
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

	//printblock(g.block)
	p := nm.block
	for len != 0 {
		cblk := g.block
		//fmt.Println("Place to:", cblk.start, "-", cblk.end)
		// copy body
		n := copy(g.pool[cblk.start:cblk.end], *(*[]byte)(unsafe.Pointer(&fakeslice)))
		p.start = cblk.start
		if int64(n) == len {
			// all data copied
			p.end = cblk.start + int64(n)
			p.next = nil
			g.block.start = p.end
		} else {
			// block too small, copy another
			p.end = cblk.end
			p.next = &indicator{}
			g.block = g.block.next
			p = p.next
		}
		len -= int64(n)
	}

	g.list = append(g.list, nm)
	//printblock(g.block)
	return nm, nil
}

func (g *Group) Delete(m *Manifest) error {
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

	g.mu.Lock()

	for p != nil {
		g.returnspace(p.start, p.end)
		p = p.next
		// Return every part of body.
	}
	g.reunion()                                     // Concat neighbour.
	g.freesize += waitD.len                         // Add freesize marker.
	g.list = append(g.list[0:i], (g.list[i+1:])...) // Delete Manifest.

	g.mu.Unlock()
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
	//printblock(g.block)
}

func (g *Group) reunion() {
	p := g.block
	for p.next != nil {
		if p.end == p.next.start {
			p.end = p.next.end
			p.next = p.next.next
		} else {
			p = p.next
		}
	}
	//printblock(g.block)
}

func (m *Manifest) Dump() (interface{}, chan bool) {
	m.rwmu.RLock()
	ack := make(chan bool)

	if m.block.next == nil {
		go throwAck(ack, &m.rwmu)
		// The memory of m->Obj should be protect until finish using its
		// resource, so we pass sync.RWMutex to throwAck func.
		return (*(*interface{})(unsafe.Pointer(&m.body[m.block.start]))), ack
	} else {
		a := make([]byte, 0, m.len)
		for p := m.block; p != nil; p = p.next {
			a = append(a, m.body[p.start:p.end]...)
		}
		go keepAlive(a, ack) // Now the memory of m->Obj has a copy in slice a,
		m.rwmu.RUnlock()     // so just keep it alive until finish using it and release RWMutex immediately.
		return (*(*interface{})(unsafe.Pointer(&a[0]))), ack
	}
}

func keepAlive(a []byte, ack chan bool) {
	<-ack
	runtime.KeepAlive(a)
}

func throwAck(ack chan bool, mu *sync.RWMutex) {
	<-ack
	mu.RUnlock()
}

func printblock(b *indicator) {
	p := b
	for p != nil {
		fmt.Printf(" -> %v", *p)
		p = p.next
	}
	fmt.Println("")
} // For debug only.
