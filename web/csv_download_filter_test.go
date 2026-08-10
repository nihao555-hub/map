package web

import (
	"strings"
	"testing"
)

func TestFilterCSVBytesForJobDropsNoiseAndDedups(t *testing.T) {
	raw := "" +
		"title,category,latitude,longitude,place_id,cid,phone,emails,whatsapp\n" +
		"Kopi A,Cafe,-6.1950,106.8300,ChIJ9UFwzvbzaS4R91dtdzt6_U8,,1,,\n" +
		"Kopi A,Cafe,-6.1950,106.8300,ChIJ9UFwzvbzaS4R91dtdzt6_U8,,,a@b.c,6281\n" +
		"Polsek Menteng,Kantor Polisi,-6.1951,106.8301,,,,,\n" +
		"Hong Kong Cafe,Cafe,22.3209,114.1612,,,,,\n"
	data := JobData{
		Keywords: []string{"kedai kopi"},
		GridMode: true,
		Lat:      "-6.1944",
		Lon:      "106.8294",
		Radius:   1000,
	}
	out, err := filterCSVBytesForJob([]byte(raw), data)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if strings.Contains(s, "Polsek") {
		t.Fatalf("police should be filtered: %s", s)
	}
	if strings.Contains(s, "Hong Kong") {
		t.Fatalf("out-of-radius should be filtered: %s", s)
	}
	if strings.Count(s, "Kopi A") != 1 {
		t.Fatalf("expected single Kopi A row, got %s", s)
	}
	if !strings.Contains(s, "6281") || !strings.Contains(s, "a@b.c") {
		t.Fatalf("expected richer contact row kept: %s", s)
	}
}
