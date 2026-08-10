package web

import (
	"strings"
	"testing"
)

func TestPlaceRelevantElectricalDropsNoise(t *testing.T) {
	kw := []string{"panel listrik"}
	noise := []Place{
		{Title: "BAGUS PAPOJAYA POOL", Category: "Kontraktor Kolam Renang"},
		{Title: "Polsek Bekasi Barat", Category: "Kantor Polisi"},
		{Title: "ENJOY SERVICE MESIN PENGERING PAKAIAN", Category: "Jasa Tukang"},
		{Title: "AZKO Summarecon Mall Bekasi", Category: "Toko Perlengkapan Rumah"},
		{Title: "Electronic City Bintaro", Category: "Toko Elektronik"},
		{Title: "Utama Elektronik", Category: "Toko Elektronik"},
		{Title: "Pusat Komponen Elektronik - Jabbar Electronics", Category: "Pemasok Komponen Elektronik"},
		{Title: "Nasional Pedia Payment", Category: "Kantor Perusahaan"},
		{Title: "PT DUTA LIANA JAYA", Category: "Pabrik Kertas"},
		{Title: "Green Soris Elektronik", Category: "Reparasi Oven Microwave"},
		{Title: "Lapak scrup,panel,mmc,hp jadul", Category: "Gudang"},
		{
			Title:    "Jual Beli Barang Bekas Besi Tembaga Kabel Kertas Aluminium UD Lapak Berkah Mandiri",
			Category: "Toko Barang Bekas",
		},
		{Title: "Pengepul Rongsok Jaya", Category: "Jasa Daur Ulang"},
		{Title: "Instalasi AC central, ducting", Category: "Jasa Instalasi Listrik"},
		{Title: "Instalasi CCTV", Category: "Jasa Instalasi Listrik"},
	}
	for _, p := range noise {
		if PlaceRelevantToKeywords(p, kw) {
			t.Fatalf("expected noise drop: %s (%s)", p.Title, p.Category)
		}
	}
}

func TestPlaceRelevantElectricalKeepsLeads(t *testing.T) {
	kw := []string{"panel listrik"}
	good := []Place{
		{Title: "Toko listrik Cahaya Bintang", Category: "Toko Alat Listrik"},
		{Title: "LINTAS TEKNIK LISTRIK JABODETABEK", Category: "Jasa Instalasi Listrik"},
		{Title: "Sanjaya Electrical Service", Category: "Jasa Instalasi Listrik"},
		{Title: "Xurya Daya Indonesia", Category: "Perusahaan tenaga surya"},
		{Title: "CV. Wijaya Lightning Protection", Category: "Toko Elektronik"},
		{Title: "Panelenginer box panel", Category: "Kantor Perusahaan"},
		{Title: "Pratama Listrik", Category: "Toko Alat Listrik"},
		{Title: "PT. Sahabat Harapan Nusantara", Category: "Insinyur Elektro"},
		{Title: "TUKANG LISTRIK BSD | service AC Cisauk", Category: "Tukang Listrik"},
		{Title: "ACK Tech (Jasa Pemasangan dan Perbaikan Instalasi Listrik, AC, CCTV)", Category: "Jasa Instalasi Listrik"},
	}
	for _, p := range good {
		if !PlaceRelevantToKeywords(p, kw) {
			t.Fatalf("expected keep: %s (%s)", p.Title, p.Category)
		}
	}
}

func TestFilterRelevantPlacesReducesNoise(t *testing.T) {
	kw := []string{"panel listrik"}
	in := []Place{
		{Title: "Toko listrik A", Category: "Toko Alat Listrik"},
		{Title: "Electronic City", Category: "Toko Elektronik"},
		{Title: "Polsek X", Category: "Kantor Polisi"},
	}
	out := FilterRelevantPlaces(in, kw)
	if len(out) != 1 || out[0].Title != "Toko listrik A" {
		t.Fatalf("got %+v", out)
	}
}

func TestFoodZeroNoise(t *testing.T) {
	kw := []string{"kedai kopi"}
	noise := []Place{
		{Title: "Polsek Menteng", Category: "Kantor Polisi"},
		{Title: "RS Cipto Mangunkusumo", Category: "Rumah Sakit"},
		{Title: "Indomaret Menteng", Category: "Minimarket"},
		{Title: "Hotel Indonesia Kempinski", Category: "Hotel"},
		{Title: "SD Negeri Menteng 01", Category: "Sekolah Dasar"},
	}
	for _, p := range noise {
		if PlaceRelevantToKeywords(p, kw) {
			t.Fatalf("expected food noise drop: %s (%s)", p.Title, p.Category)
		}
	}
	if !PlaceRelevantToKeywords(Place{Title: "Kopi Kenangan", Category: "Kedai Kopi"}, kw) {
		t.Fatal("expected keep kopi")
	}
	if !PlaceRelevantToKeywords(Place{Title: "Random Cafe", Category: "Cafe"}, kw) {
		t.Fatal("expected keep cafe category")
	}
}

func TestFilterPlacesForJobEnforcesRadius(t *testing.T) {
	data := JobData{
		Keywords: []string{"kedai kopi"},
		GridMode: true,
		Lat:      "-6.1944",
		Lon:      "106.8294",
		Radius:   1000,
	}
	in := []Place{
		{Title: "Nearby Cafe", Category: "Cafe", Latitude: -6.1950, Longitude: 106.8300},
		{Title: "Hong Kong Cafe", Category: "Cafe", Latitude: 22.3209, Longitude: 114.1612},
		{Title: "Missing Coordinates", Category: "Cafe"},
	}
	out := FilterPlacesForJob(in, data)
	if len(out) != 1 || out[0].Title != "Nearby Cafe" {
		t.Fatalf("got %+v", out)
	}
}

func TestGridJobWithoutAnchorReturnsNoPlaces(t *testing.T) {
	out := FilterPlacesForJob(
		[]Place{{Title: "Global Noise", Category: "Cafe", Latitude: 22.3, Longitude: 114.1}},
		JobData{Keywords: []string{"cafe"}, GridMode: true, Radius: 2000},
	)
	if len(out) != 0 {
		t.Fatalf("want empty, got %+v", out)
	}
}

func TestDedupPlacesKeepsBestContact(t *testing.T) {
	const pid = "ChIJ9UFwzvbzaS4R91dtdzt6_U8"
	in := []Place{
		{Title: "Kopi A", PlaceID: pid, Phone: "1", Latitude: -6.1, Longitude: 106.8},
		{Title: "Kopi A", PlaceID: pid, WhatsApp: "6281", Emails: "a@b.c", Latitude: -6.1, Longitude: 106.8},
		{Title: "Kopi B", Cid: "999", Latitude: -6.2, Longitude: 106.9},
		{Title: "Kopi B Dup", Link: "https://maps.google.com/?cid=999", Latitude: -6.2, Longitude: 106.9},
	}
	out := DedupPlaces(in)
	if len(out) != 2 {
		t.Fatalf("want 2, got %d %+v", len(out), out)
	}
	var kopiA Place
	for _, p := range out {
		if p.PlaceID == pid || strings.Contains(StablePlaceKey(p), pid) {
			kopiA = p
		}
	}
	if kopiA.WhatsApp == "" || kopiA.Emails == "" {
		t.Fatalf("expected richer contact kept: %+v", kopiA)
	}
}
