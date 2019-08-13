package cache

import (
	"log"
	"testing"
)

func BenchmarkNewGroup(b *testing.B) {
	for i := 0; i < 10; i++ {
		_, err := NewPool(256*1024*1024, 1024*256)
		if err != nil {
			log.Fatal("NewPool fails!")
		}
	}
}
