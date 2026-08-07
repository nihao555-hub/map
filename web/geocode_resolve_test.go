package web

import "testing"

func TestResolveLocationAnchorOfflineJakarta(t *testing.T) {
	// 不依赖外网：词典+离线表应直接给出雅加达坐标
	point, err := ResolveLocationAnchor(t.Context(), "雅加达", "id")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if point.Lat > -5 || point.Lat < -7 || point.Lon < 106 || point.Lon > 108 {
		t.Fatalf("unexpected jakarta coords: %+v", point)
	}
	if point.MinLat == 0 && point.MaxLat == 0 {
		t.Fatalf("bbox missing: %+v", point)
	}
}

func TestLookupKnownCityFuzzy(t *testing.T) {
	p, ok := lookupKnownCity("Jakarta")
	if !ok || p.CountryCode != "id" {
		t.Fatalf("jakarta offline miss: %+v ok=%v", p, ok)
	}
	p, ok = lookupKnownCity("巴厘岛")
	if !ok {
		t.Fatal("bali chinese lexicon offline miss")
	}
	_ = p
}

func TestLookupKnownCityCompoundPrefersDistrict(t *testing.T) {
	p, ok := lookupKnownCity("Menteng, Jakarta")
	if !ok {
		t.Fatal("compound district should resolve offline")
	}
	if p.DisplayName != "Menteng, Jakarta" {
		t.Fatalf("got parent/wrong anchor: %+v", p)
	}

	p, ok = lookupKnownCity("Jakarta Selatan, Indonesia")
	if !ok || p.DisplayName != "Jakarta Selatan" {
		t.Fatalf("jakarta district should resolve offline: %+v ok=%v", p, ok)
	}
}
