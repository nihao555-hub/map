package deduper

import (
	"context"
	"fmt"
	"testing"
)

func TestBloomAddIfNotExists(t *testing.T) {
	d := NewBloom(1000, 0.01)
	ctx := context.Background()

	if !d.AddIfNotExists(ctx, "a") {
		t.Fatal("first add should succeed")
	}
	if d.AddIfNotExists(ctx, "a") {
		t.Fatal("duplicate should fail")
	}
	if !d.AddIfNotExists(ctx, "b") {
		t.Fatal("new key should succeed")
	}
}

func TestBloomNoFalseNegatives(t *testing.T) {
	d := NewBloom(2000, 0.001)
	ctx := context.Background()
	for i := 0; i < 500; i++ {
		key := fmt.Sprintf("place-%d", i)
		if !d.AddIfNotExists(ctx, key) {
			t.Fatalf("unexpected reject on first insert %s", key)
		}
		if d.AddIfNotExists(ctx, key) {
			t.Fatalf("false negative / missed duplicate for %s", key)
		}
	}
}
