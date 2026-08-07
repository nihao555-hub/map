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

func TestPlaceEntryShouldDropRadiusAndNoise(t *testing.T) {
	kw := []string{"kedai kopi"}
	anchorLat, anchorLon := -6.1944, 106.8294
	radiusM := 3000.0
	if !placeEntryShouldDrop(
		&Entry{Title: "Polsek Menteng", Category: "Kantor Polisi", Latitude: -6.195, Longtitude: 106.830},
		kw, anchorLat, anchorLon, radiusM,
	) {
		t.Fatal("police should drop")
	}
	if placeEntryShouldDrop(
		&Entry{Title: "Kopi Kenangan", Category: "Kedai Kopi", Latitude: -6.195, Longtitude: 106.830},
		kw, anchorLat, anchorLon, radiusM,
	) {
		t.Fatal("nearby cafe should keep")
	}
	if !placeEntryShouldDrop(
		&Entry{Title: "Hong Kong Cafe", Category: "Cafe", Latitude: 22.32, Longtitude: 114.16},
		kw, anchorLat, anchorLon, radiusM,
	) {
		t.Fatal("far cafe should drop on radius")
	}
}

func TestFeedHitShouldSkipOutOfRadius(t *testing.T) {
	href := "https://www.google.com/maps/place/Foo/@22.3209,114.1612,17z/data=!3d22.3209!4d114.1612"
	if !feedHitShouldSkip("Foo Cafe", href, []string{"kedai kopi"}, -6.1944, 106.8294, 3000) {
		t.Fatal("expected skip for far coords")
	}
	near := "https://www.google.com/maps/place/Bar/@-6.1950,106.8300,17z/data=!3d-6.1950!4d106.8300"
	if feedHitShouldSkip("Kopi Kenangan", near, []string{"kedai kopi"}, -6.1944, 106.8294, 3000) {
		t.Fatal("nearby cafe title should not skip")
	}
}
