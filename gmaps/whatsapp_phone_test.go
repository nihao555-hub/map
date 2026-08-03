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
}
