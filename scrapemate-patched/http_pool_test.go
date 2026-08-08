package scrapemate

import "testing"

func TestJobNeedsBrowser(t *testing.T) {
	cases := []struct {
		url  string
		want bool
	}{
		{"https://www.google.com/maps/place/Foo", true},
		{"https://maps.google.com/search", true},
		{"https://www.kopikenangan.com/", false},
		{"https://linktr.ee/shop", false},
	}
	for _, tc := range cases {
		j := &Job{URL: tc.url}
		if got := jobNeedsBrowser(j); got != tc.want {
			t.Fatalf("url=%s got=%v want=%v", tc.url, got, tc.want)
		}
	}
}
