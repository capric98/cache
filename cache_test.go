package cache

import (
	"fmt"
	"testing"
)

type TS struct {
	A int
	b int64
}

func TestNewGroup(t *testing.T) {
	for i := 0; i < 10; i++ {
		_, err := NewPool(1024, 256)
		if err != nil {
			fmt.Println("NewPool fails.")
			t.Fail()
		}
	}
}

var (
	GP, _ = NewPool(256, 256)
)

func TestPutGet(t *testing.T) {
	a := TS{
		A: 1,
		b: 9,
	}

	P, _ := NewPool(256, 256)

	G, err := P.NewGroup()
	if err != nil {
		//fmt.Println
		t.Fail()
	}
	m, err := G.Put(a)
	if err != nil {
		t.Fail()
	}
	i, ack := m.Dump()
	da := i.(TS)
	ack <- true
	if da != a {
		t.Fail()
	}

	b := TS{
		A: 123,
		b: 0,
	}
	nm, err := G.Put(b)
	if err != nil {
		t.Fail()
	}
	i, ack = m.Dump()
	da = i.(TS)
	ack <- true
	if da != a {
		fmt.Println("dump 2 fails")
		fmt.Println(da, a)
		t.Fail()
	}

	if err := G.Delete(m); err != nil {
		fmt.Println("Delete fails.")
		t.Fail()
	}
	printblock(G.block)
	ni, nack := nm.Dump()
	db := ni.(TS)
	if db != b {
		fmt.Println("b dump fails.")
		t.Fail()
	}
	nack <- true
	//if err := G.Delete(nm); err != nil {
	//	fmt.Println("Delete 2 fails.")
	//	t.Fail()
	//}
	_ = G.Reorder()
	fmt.Println(P.pool[0:32])
	printblock(G.block)

	//t.Fail()
}

func BenchmarkOverall(b *testing.B) {
	aa := TS{
		A: 1,
		b: 0,
	}
	bb := TS{
		A: 100,
		b: 9,
	}
	b.ReportAllocs()
	b.StartTimer()
	G, err := GP.NewGroup()
	if err != nil {
		b.Fail()
	}
	am, err := G.Put(aa)
	if err != nil {
		b.Fail()
	}
	bm, err := G.Put(bb)
	if err != nil {
		b.Fail()
	}
	ia, aack := am.Dump()
	ib, back := bm.Dump()
	if ia.(TS) != aa || ib.(TS) != bb {
		fmt.Println("Get false.")
	}
	aack <- true
	back <- true
	//printblock(G.block)
	//fmt.Println(G.pool[:32])
	_ = G.Delete(am)
	_ = G.Reorder()
	//printblock(G.block)
	//fmt.Println(G.pool[:32])
	b.StopTimer()
}
