package web

import "testing"

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
		// Maps miscategorizes HVAC/CCTV under electrical install categories.
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
		// Mixed electricians who also mention AC still keep via listrik in title.
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

func TestNonElectricalPassthrough(t *testing.T) {
	kw := []string{"火锅店"}
	p := Place{Title: "Random Cafe", Category: "Cafe"}
	if !PlaceRelevantToKeywords(p, kw) {
		t.Fatal("non-electrical jobs should not filter")
	}
}
