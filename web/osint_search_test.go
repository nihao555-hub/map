package web

import "testing"

func TestExtractLinkedInHitsFromHTML(t *testing.T) {
	html := `
	<a href="https://id.linkedin.com/in/inis-sahib-538952182">INIS Sahib - Legal Officer at PT Sempurna</a>
	<a href="https://www.linkedin.com/company/fore-coffee">Fore Coffee</a>
	spam https://www.linkedin.com/in/bobby-purnama-8b226aab more
	`
	hits := extractLinkedInHitsFromHTML(html)
	if len(hits) < 3 {
		t.Fatalf("hits=%d %+v", len(hits), hits)
	}
	var inN, coN int
	for _, h := range hits {
		switch h.Kind {
		case "in":
			inN++
		case "company":
			coN++
		}
	}
	if inN < 2 || coN < 1 {
		t.Fatalf("in=%d co=%d hits=%+v", inN, coN, hits)
	}
}

func TestSearchHTMLLooksBlocked(t *testing.T) {
	if !searchHTMLLooksBlocked("short", 200) {
		t.Fatal("short body should look blocked")
	}
	if !searchHTMLLooksBlocked(string(make([]byte, 3000)), 429) {
		t.Fatal("429 blocked")
	}
	ok := `<!doctype html>` + string(make([]byte, 5000)) + `linkedin.com/in/foo`
	if searchHTMLLooksBlocked(ok, 200) {
		t.Fatal("valid page flagged")
	}
}
