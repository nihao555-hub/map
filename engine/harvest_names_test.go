package engine

import (
	"context"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

func TestNameHitMatchesRequiresSharedTokens(t *testing.T) {
	row := Merchant{Name: "Crompton Lamps Limited", Country: "GB"}
	ok := Hit{Platform: PlatformFacebook, Name: "Crompton Lamps", Handle: "cromptonlamps", HomepageURL: "https://www.facebook.com/cromptonlamps"}
	if !nameHitMatches(row, ok) {
		t.Fatal("expected match")
	}
	bad := Hit{Platform: PlatformFacebook, Name: "Random Shop", Handle: "randomshop", HomepageURL: "https://www.facebook.com/randomshop"}
	if nameHitMatches(row, bad) {
		t.Fatal("false positive")
	}
}

func TestNameDorkCandidateSkipsFunds(t *testing.T) {
	if nameDorkCandidate(Merchant{Name: "Emerging Markets Fund", Country: "US"}) {
		t.Fatal("fund leaked")
	}
	if !nameDorkCandidate(Merchant{Name: "Crompton Lamps Limited", Country: "GB"}) {
		t.Fatal("lamp maker dropped")
	}
}

func TestHarvestNameDorksAttachesExistingGLEIF(t *testing.T) {
	db := filepath.Join(t.TempDir(), "m.db")
	dir, err := OpenDirectory(db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := dir.InsertBatch(context.Background(), []Merchant{
		{ExtID: "gleif:CROMPTON1", Source: "gleif", Name: "Crompton Lamps Limited", Country: "GB",
			Homepage: "https://search.gleif.org/#/record/CROMPTON1"},
	}); err != nil {
		t.Fatal(err)
	}
	_ = dir.Close()
	html := `<html><body><div id="search">
	  <a href="/url?q=https://www.facebook.com/cromptonlamps&amp;sa=U">Crompton Lamps</a>
	</div></body></html>`
	c := &Client{HTTP: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(html)), Header: make(http.Header), Request: req}, nil
	})}}
	st, err := c.HarvestNameDorks(context.Background(), HarvestOptions{DBPath: db, QueryLimit: 10, Workers: 1})
	if err != nil {
		t.Fatal(err)
	}
	if st.Inserted < 1 || st.Profiles < 1 {
		t.Fatalf("%+v", st)
	}
}
