//go:build livecustoms

package web

import (
	"context"
	"testing"
	"time"
)

func TestLiveLookupUSCustomsStarbucks(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	tr, err := lookupUSCustomsTrade(ctx, "Starbucks Coffee Company", "starbucks.com")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%+v", tr)
}
