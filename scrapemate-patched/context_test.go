package scrapemate

import (
	"context"
	"testing"
)

func TestJobPusherContextRoundTrip(t *testing.T) {
	ctx := context.Background()
	if GetJobPusherFromContext(ctx) != nil {
		t.Fatal("expected nil pusher")
	}
	called := false
	ctx = ContextWithJobPusher(ctx, func(_ context.Context, _ IJob) error {
		called = true
		return nil
	})
	push := GetJobPusherFromContext(ctx)
	if push == nil {
		t.Fatal("expected pusher")
	}
	if err := push(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("pusher not called")
	}
}
