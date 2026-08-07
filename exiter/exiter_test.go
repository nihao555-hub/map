package exiter

import "testing"

func TestSeedsFinishedBeforeEmailsComplete(t *testing.T) {
	e := New()
	e.SetSeedCount(2)
	e.IncrPlacesFound(5)
	e.IncrSeedCompleted(1)
	if e.SeedsFinished() {
		t.Fatal("expected seeds not finished")
	}
	e.IncrSeedCompleted(1)
	if !e.SeedsFinished() {
		t.Fatal("expected seeds finished")
	}
	p := e.Snapshot()
	if p.SeedCompleted != 2 || p.PlacesFound != 5 || p.PlacesCompleted != 0 {
		t.Fatalf("unexpected progress: %+v", p)
	}
}
