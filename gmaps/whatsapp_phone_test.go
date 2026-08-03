package gmaps

import "testing"

func TestFillWhatsAppFromPhone(t *testing.T) {
	e := &Entry{Phone: "081234567890"}
	e.FillWhatsAppFromPhone()
	if e.WhatsApp != "+6281234567890" {
		t.Fatalf("got %q", e.WhatsApp)
	}
	e2 := &Entry{Phone: "+1 415-555-0101", WhatsApp: ""}
	e2.FillWhatsAppFromPhone()
	if e2.WhatsApp != "+14155550101" {
		t.Fatalf("got %q", e2.WhatsApp)
	}
	e3 := &Entry{Phone: "08111", WhatsApp: ""}
	e3.FillWhatsAppFromPhone()
	if e3.WhatsApp != "" {
		t.Fatalf("short phone should not convert: %q", e3.WhatsApp)
	}
	// 印尼座机不得标成 WhatsApp
	e4 := &Entry{Phone: "+62 21 50948892", WhatsApp: ""}
	e4.FillWhatsAppFromPhone()
	if e4.WhatsApp != "" {
		t.Fatalf("landline should not become WA: %q", e4.WhatsApp)
	}
	e5 := &Entry{Phone: "+62 811-1976-4586", WhatsApp: ""}
	e5.FillWhatsAppFromPhone()
	if e5.WhatsApp != "+6281119764586" {
		t.Fatalf("ID mobile got %q", e5.WhatsApp)
	}
}

func TestEnrichContactsFromMapsFieldsWaMeWebsite(t *testing.T) {
	e := &Entry{
		WebSite: "http://wa.me/+62%20881-0224-46109",
		Phone:   "+62 881-0224-46109",
	}
	e.EnrichContactsFromMapsFields()
	if e.WhatsApp != "+62881022446109" {
		t.Fatalf("wa.me website should yield WA, got %q", e.WhatsApp)
	}
}

func TestEnrichContactsFromMapsFieldsInstagramWebsite(t *testing.T) {
	e := &Entry{WebSite: "https://www.instagram.com/kopinette.id"}
	e.EnrichContactsFromMapsFields()
	if e.Instagram == "" {
		t.Fatal("expected instagram promoted from website")
	}
	if e.IsWebsiteValidForEmail() {
		t.Fatal("instagram website should not spawn email job")
	}
}
