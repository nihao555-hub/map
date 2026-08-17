package engine

import (
	"strings"
	"testing"
)

func TestTradeGuruQueriesClassicFacebookDorks(t *testing.T) {
	wanted := map[string]bool{PlatformFacebook: true, PlatformInstagram: true, PlatformLinkedIn: true}
	qs := tradeGuruQueries([]string{"LED灯", "LED light"}, wanted, "MY", RoleBuyer)
	got := queryStrings(qs)
	for _, want := range []string{
		"inurl:facebook.com LED灯 -inurl:posts -inurl:photos -inurl:videos -inurl:reel",
		`inurl:facebook.com "LED light" -inurl:posts -inurl:photos -inurl:videos -inurl:reel`,
		"site:facebook.com/pages LED灯",
		"intitle:LED灯 site:facebook.com",
		`site:linkedin.com/company "LED light"`,
		`inurl:instagram.com "LED light" -inurl:/p/ -inurl:/reel/`,
		`LED灯 "facebook.com/" "instagram.com/"`,
		`inurl:facebook.com LED灯 Malaysia -inurl:posts -inurl:photos`,
	} {
		if !containsString(got, want) {
			t.Fatalf("missing %q in %+v", want, got)
		}
	}
	for _, q := range qs {
		if !q.preferGoogle {
			t.Fatalf("trade dork should prefer Google: %+v", q)
		}
	}
}

func TestPublicSearchQueriesPutsTradeDorksFirst(t *testing.T) {
	wanted := map[string]bool{PlatformFacebook: true}
	qs := publicSearchQueries("LED灯", wanted, "", RoleBuyer)
	if len(qs) == 0 || !strings.Contains(qs[0].query, "inurl:facebook.com") {
		t.Fatalf("expected inurl dork first, got %+v", qs[:min(3, len(qs))])
	}
	if !containsString(queryStrings(qs), "site:facebook.com LED灯") {
		t.Fatalf("still need volume site: query %+v", queryStrings(qs))
	}
}

func TestDecodeGoogleRedirect(t *testing.T) {
	target := "https://www.facebook.com/Signify"
	got := decodeGoogleRedirect("/url?q=" + target + "&sa=U&ved=1")
	if got != target {
		t.Fatalf("got %s", got)
	}
	if decodeGoogleRedirect(target) != target {
		t.Fatal("plain href changed")
	}
}

func TestExtractProfilesFromGoogleSERP(t *testing.T) {
	html := []byte(`<html><body><div id="search">
	  <div class="g"><a href="/url?q=https://www.facebook.com/PowerbiltTools&amp;sa=U">Powerbilt Tools</a></div>
	  <div class="g"><a href="/url?q=https://www.instagram.com/signify&amp;sa=U">Signify</a></div>
	</div></body></html>`)
	hits := extractProfilesFromHTML(html, "google")
	var sawFB, sawIG bool
	for _, h := range hits {
		if h.Platform == PlatformFacebook && strings.Contains(h.HomepageURL, "facebook.com/PowerbiltTools") {
			sawFB = true
		}
		if h.Platform == PlatformInstagram && strings.Contains(h.HomepageURL, "instagram.com/signify") {
			sawIG = true
		}
	}
	if !sawFB || !sawIG {
		t.Fatalf("google serp missed socials fb=%v ig=%v hits=%+v", sawFB, sawIG, hits)
	}
}

func TestLooksLikeGoogleSorry(t *testing.T) {
	if !looksLikeChallenge([]byte(`<html><a href="https://www.google.com/sorry/index">unusual traffic</a></html>`)) {
		t.Fatal("expected google sorry")
	}
	if !looksLikeChallenge([]byte(`<html>Before you continue to Google consent.google.com</html>`)) {
		t.Fatal("expected google consent")
	}
}
