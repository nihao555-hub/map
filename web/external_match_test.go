package web

import "testing"

func TestIdentifiableCompanyName(t *testing.T) {
	generic := []string{
		"Indo",
		"Toko Listrik Jaya",
		"Panel Maker",
		"Jasa Teknik Listrik",
		"Sinar Abadi",
	}
	for _, name := range generic {
		if IdentifiableCompanyName(name) {
			t.Fatalf("expected %q to be too generic for external lookups", name)
		}
	}

	specific := []string{
		"Indo Karya Panel Kenari",
		"PT Schneider Electric Indonesia",
		"Xurya Daya Indonesia",
	}
	for _, name := range specific {
		if !IdentifiableCompanyName(name) {
			t.Fatalf("expected %q to be identifiable", name)
		}
	}
}

func TestExternalRecordMatchesBusiness(t *testing.T) {
	// The live failure: a Jakarta panel shop was matched to a US bedding
	// importer literally named "Indo".
	if ExternalRecordMatchesBusiness("Indo", "Indo Karya Panel pasar kenari lama lantai 1 no 17") {
		t.Fatal("generic prefix must not count as a customs match")
	}
	// Wikipedia returned "The Lace Maker" for "Johan panel maker".
	if ExternalRecordMatchesBusiness("The Lace Maker", "Johan panel maker") {
		t.Fatal("unrelated encyclopedia page must not match")
	}

	if !ExternalRecordMatchesBusiness("INDO KARYA PANEL PT", "Indo Karya Panel Kenari") {
		t.Fatal("record echoing the distinctive token should match")
	}
	if !ExternalRecordMatchesBusiness("Schneider Electric SE", "PT Schneider Electric Indonesia") {
		t.Fatal("brand match should be accepted")
	}
}
