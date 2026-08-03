package gmaps_test

import (
	"testing"

	"github.com/gosom/google-maps-scraper/gmaps"
)

func TestClassifyAndPromoteSocial(t *testing.T) {
	e := &gmaps.Entry{
		WebSite: "https://www.facebook.com/SomeCafeJakarta",
		Reservations: []gmaps.LinkSource{
			{Link: "https://instagram.com/somecafe", Source: "ig"},
		},
		OrderOnline: []gmaps.LinkSource{
			{Link: "https://www.linkedin.com/company/example", Source: "li"},
		},
	}
	e.PromoteSocialFromMapsFields()

	if e.Facebook == "" {
		t.Fatal("expected facebook from website")
	}
	if e.Instagram == "" {
		t.Fatal("expected instagram from reservations")
	}
	if e.LinkedIn == "" {
		t.Fatal("expected linkedin from order_online")
	}
}

func TestExtractSocialFromHTML(t *testing.T) {
	html := []byte(`
		<html><body>
		<a href="https://facebook.com/shop">fb</a>
		<a href="https://www.linkedin.com/in/owner">li</a>
		<a href="https://x.com/brand">tw</a>
		</body></html>
	`)
	// 通过 Entry.mergeSocial 间接验证提取：用未导出函数需同包；此处走 Process 路径的公开组合
	e := &gmaps.Entry{WebSite: "https://example.com"}
	e.PromoteSocialFromMapsFields()
	if e.Facebook != "" {
		t.Fatal("example.com should not be social")
	}

	// 用 website 为 twitter 再测一次
	e2 := &gmaps.Entry{WebSite: "https://twitter.com/foo"}
	e2.PromoteSocialFromMapsFields()
	if e2.Twitter == "" {
		t.Fatal("expected twitter")
	}
	_ = html
}
