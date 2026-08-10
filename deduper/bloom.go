package deduper

import (
	"context"
	"hash/fnv"
	"math"
	"sync"
)

// BloomDeduper is a memory-efficient approximate set (Google Bigtable/LevelDB style).
// False positives may skip a unique key (~FP rate); false negatives never occur.
// Prefer New() (exact hashmap) when RAM allows; use NewBloom when tracking millions of keys.
type BloomDeduper struct {
	mux      sync.Mutex
	bits     []uint64
	k        uint
	nbits    uint64
	count    uint64
	exact    *hashmap // exact until threshold, then bloom-only for growth
	switchAt int
}

// NewBloom builds a bloom filter sized for expectedKeys at the given false-positive rate.
// expectedKeys <= 0 defaults to 500_000; fpRate outside (0,1) defaults to 0.001.
func NewBloom(expectedKeys int, fpRate float64) Deduper {
	if expectedKeys <= 0 {
		expectedKeys = 500_000
	}
	if fpRate <= 0 || fpRate >= 1 {
		fpRate = 0.001
	}

	// m = -n*ln(p) / (ln2)^2 ; k = (m/n)*ln2
	n := float64(expectedKeys)
	m := -n * math.Log(fpRate) / (math.Ln2 * math.Ln2)
	k := math.Max(1, math.Round((m/n)*math.Ln2))
	nbits := uint64(math.Ceil(m))
	if nbits < 64 {
		nbits = 64
	}
	words := (nbits + 63) / 64

	return &BloomDeduper{
		bits:     make([]uint64, words),
		k:        uint(k),
		nbits:    nbits,
		exact:    &hashmap{seen: make(map[uint64]struct{}), mux: &sync.RWMutex{}},
		switchAt: expectedKeys / 4, // keep exact for first quarter (zero FP)
	}
}

func (d *BloomDeduper) AddIfNotExists(_ context.Context, key string) bool {
	h1, h2 := bloomHashes(key)

	d.mux.Lock()
	defer d.mux.Unlock()

	if d.exact != nil && int(d.count) < d.switchAt {
		if !d.exact.AddIfNotExists(context.Background(), key) {
			return false
		}
		d.addLocked(h1, h2)
		d.count++
		if int(d.count) >= d.switchAt {
			d.exact = nil // drop exact map to free RAM
		}
		return true
	}

	if d.containsLocked(h1, h2) {
		return false
	}
	d.addLocked(h1, h2)
	d.count++
	return true
}

func (d *BloomDeduper) containsLocked(h1, h2 uint64) bool {
	for i := uint(0); i < d.k; i++ {
		idx := (h1 + uint64(i)*h2) % d.nbits
		if d.bits[idx/64]&(1<<(idx%64)) == 0 {
			return false
		}
	}
	return true
}

func (d *BloomDeduper) addLocked(h1, h2 uint64) {
	for i := uint(0); i < d.k; i++ {
		idx := (h1 + uint64(i)*h2) % d.nbits
		d.bits[idx/64] |= 1 << (idx % 64)
	}
}

func bloomHashes(key string) (uint64, uint64) {
	h := fnv.New64a()
	_, _ = h.Write([]byte(key))
	a := h.Sum64()
	h.Reset()
	_, _ = h.Write([]byte(key))
	_, _ = h.Write([]byte{0x5f})
	b := h.Sum64()
	if b == 0 {
		b = 1
	}
	return a, b
}
