package gmaps

import "testing"

func TestEntryDedupKeyPrefersPlaceID(t *testing.T) {
	e := &Entry{
		PlaceID:    "ChIJ9UFwzvbzaS4R91dtdzt6_U8",
		Cid:        "123",
		Link:       "https://maps.google.com/?cid=999",
		Title:      "Toko",
		Latitude:   -6.2,
		Longtitude: 106.8,
	}
	got := EntryDedupKey(e)
	if got != "pid:ChIJ9UFwzvbzaS4R91dtdzt6_U8" {
		t.Fatalf("got %q", got)
	}
}

func TestMapsURLDedupKeyUnifiesVariants(t *testing.T) {
	a := "https://www.google.com/maps/place/A/@1,2,17z/data=!4m7!3m6!1s0x89c259ab3c1ef289:0x3b67a41175949f55!8m2!3d1!4d2"
	b := "https://www.google.com/maps/place/B/@1.0,2.0,17z/data=!3m1!4b1!4m5!3m4!1s0x89c259ab3c1ef289:0x3b67a41175949f55!8m2!3d1!4d2"
	if MapsURLDedupKey(a) != MapsURLDedupKey(b) {
		t.Fatalf("%q vs %q", MapsURLDedupKey(a), MapsURLDedupKey(b))
	}
}

func TestFilterEntriesByKeywordsFood(t *testing.T) {
	in := []*Entry{
		{Title: "Kopi Kenangan", Category: "Kedai Kopi"},
		{Title: "Polsek Menteng", Category: "Kantor Polisi"},
		{Title: "Indomaret", Category: "Minimarket"},
	}
	out := FilterEntriesByKeywords(in, []string{"kedai kopi"})
	if len(out) != 1 || out[0].Title != "Kopi Kenangan" {
		t.Fatalf("got %+v", out)
	}
}
