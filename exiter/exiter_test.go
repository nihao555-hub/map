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

func TestMapsPlacesFinishedBeforeEmails(t *testing.T) {
	e := New()
	e.SetSeedCount(1)
	e.IncrPlacesFound(2)
	e.IncrSeedCompleted(1)
	if e.MapsPlacesFinished() {
		t.Fatal("maps places not done yet")
	}
	e.IncrMapsPlacesDone(1)
	if e.MapsPlacesFinished() {
		t.Fatal("only 1/2 maps places done")
	}
	e.IncrMapsPlacesDone(1)
	if !e.MapsPlacesFinished() {
		t.Fatal("expected maps places finished")
	}
	if e.Snapshot().PlacesCompleted != 0 {
		t.Fatal("emails must still be pending (PlacesCompleted)")
	}
}
