package placeref

import "testing"

func TestStableKeyPrefersPlaceID(t *testing.T) {
	k := StableKey(Identifiers{
		PlaceID: "ChIJ9UFwzvbzaS4R91dtdzt6_U8",
		Cid:     "123",
		Link:    "https://maps.google.com/?cid=999",
	})
	if k != "pid:ChIJ9UFwzvbzaS4R91dtdzt6_U8" {
		t.Fatalf("got %q", k)
	}
}

func TestKeyFromMapsURLUnifiesDeepHrefs(t *testing.T) {
	a := "https://www.google.com/maps/place/Joe's+Pizza/@40.75,-73.98,17z/data=!4m7!3m6!1s0x89c259ab3c1ef289:0x3b67a41175949f55!8m2!3d40.75!4d-73.98"
	b := "https://www.google.com/maps/place/Joes+Pizza/@40.7546795,-73.9870291,17z/data=!3m1!4b1!4m5!3m4!1s0x89c259ab3c1ef289:0x3b67a41175949f55!8m2!3d40.7546795!4d-73.9870291"
	ka, kb := KeyFromMapsURL(a), KeyFromMapsURL(b)
	if ka == "" || ka != kb {
		t.Fatalf("keys differ: %q vs %q", ka, kb)
	}
	if !stringsHasPrefix(ka, "did:0x89c259ab3c1ef289:") {
		t.Fatalf("want did: key, got %q", ka)
	}
}

func TestKeyFromPlaceIDQuery(t *testing.T) {
	k := KeyFromMapsURL("https://www.google.com/maps/place/?q=place_id:ChIJDdnwdv0y5xQRRytw1ihZQeU")
	if k != "pid:ChIJDdnwdv0y5xQRRytw1ihZQeU" {
		t.Fatalf("got %q", k)
	}
}

func TestStableKeyGeoFallbackNormalized(t *testing.T) {
	a := StableKey(Identifiers{Title: "Kopi Kenangan!", Latitude: -6.19441, Longitude: 106.82945})
	b := StableKey(Identifiers{Title: "kopi kenangan", Latitude: -6.194409, Longitude: 106.829451})
	if a == "" || a != b {
		t.Fatalf("geo keys should match: %q vs %q", a, b)
	}
}

func TestFoodDropsCivicAndKeepsCafe(t *testing.T) {
	kw := []string{"kedai kopi"}
	noise := []Place{
		{Title: "Polsek Menteng", Category: "Kantor Polisi"},
		{Title: "RS Cipto", Category: "Rumah Sakit"},
		{Title: "SD Negeri 1", Category: "Sekolah Dasar"},
		{Title: "Indomaret Menteng", Category: "Minimarket"},
		{Title: "Toko Listrik Jaya", Category: "Toko Alat Listrik"},
		{Title: "Hotel Indonesia Kempinski", Category: "Hotel"},
	}
	for _, p := range noise {
		if Relevant(p, kw) {
			t.Fatalf("expected drop: %s", p.Title)
		}
	}
	good := []Place{
		{Title: "Kopi Kenangan Menteng", Category: "Kedai Kopi"},
		{Title: "Starbucks Grand Indonesia", Category: "Coffee shop"},
		{Title: "Warung Kopi Aceh", Category: "Cafe"},
	}
	for _, p := range good {
		if !Relevant(p, kw) {
			t.Fatalf("expected keep: %s", p.Title)
		}
	}
}

func TestElectricalStillDropsNoise(t *testing.T) {
	kw := []string{"panel listrik"}
	if Relevant(Place{Title: "Polsek Bekasi", Category: "Kantor Polisi"}, kw) {
		t.Fatal("police should drop")
	}
	if Relevant(Place{Title: "Electronic City", Category: "Toko Elektronik"}, kw) {
		t.Fatal("electronics retail should drop")
	}
	if !Relevant(Place{Title: "Toko listrik Cahaya", Category: "Toko Alat Listrik"}, kw) {
		t.Fatal("electrical shop should keep")
	}
}

func TestGenericDropsPolice(t *testing.T) {
	kw := []string{"supplier packaging"}
	if Relevant(Place{Title: "Polsek X", Category: "Kantor Polisi"}, kw) {
		t.Fatal("universal civic noise")
	}
}

func stringsHasPrefix(s, p string) bool {
	return len(s) >= len(p) && s[:len(p)] == p
}
