package web

import "testing"

func TestIsBusinessNameNotPerson(t *testing.T) {
	cases := []struct {
		name      string
		person    string
		business  string
		wantJunk  bool
		rationale string
	}{
		{"role suffix", "Doxionte cafe (Pemilik)", "Doxionte cafe", true, "店名加印尼语「老板」不是人名"},
		{"role suffix en", "Cafe Ade (Owner)", "Cafe Ade", true, "店名加 Owner 不是人名"},
		{"exact match", "Tuang Coffee", "Tuang Coffee", true, "与店名完全相同"},
		{"case insensitive", "tuang coffee", "Tuang Coffee", true, "大小写不同仍是店名"},
		{"substring of business", "Giyanti", "Giyanti Coffee Roastery", true, "只是店名的一部分"},
		{"real person", "Enristia P", "Aquatic Cafe", false, "真实店主姓名应保留"},
		{"real founder", "Edward Tirtanata", "Kopi Kenangan", false, "真实创始人应保留"},
		{"empty", "", "Some Cafe", true, "空名不是人"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isBusinessNameNotPerson(tc.person, tc.business); got != tc.wantJunk {
				t.Fatalf("isBusinessNameNotPerson(%q, %q) = %v, want %v (%s)",
					tc.person, tc.business, got, tc.wantJunk, tc.rationale)
			}
		})
	}
}

func TestIsRegistrarContact(t *testing.T) {
	blocked := []string{
		"abuse-complaints@squarespace.com",
		"abuse@namecheap.com",
		"whois@godaddy.com",
		"support@domainsbyproxy.com",
		"registrar@tucows.com",
		"privacy@withheldforprivacy.com",
	}
	for _, e := range blocked {
		if !isRegistrarContact(e) {
			t.Errorf("expected %q to be treated as registrar contact", e)
		}
	}

	allowed := []string{
		"enristiap@gmail.com",
		"hello@tuangcoffee.com",
		"marketing@tanamera.coffee",
		"",
	}
	for _, e := range allowed {
		if isRegistrarContact(e) {
			t.Errorf("expected %q to be kept", e)
		}
	}
}

func TestSanitizeDecisionMakersDropsRegistrarAndBusinessNames(t *testing.T) {
	place := Place{Title: "Tuang Coffee", Phone: "+6285219569191"}
	in := []DecisionMaker{
		{Name: "Abuse Complaints", Title: "Contact", Email: "abuse-complaints@squarespace.com"},
		{Name: "Tuang Coffee (Pemilik)", Title: "Owner", Phone: "+6285219569191"},
		{Name: "Enristia P", Title: "Owner", Email: "enristiap@gmail.com"},
	}

	out := sanitizeDecisionMakers(in, place)

	for _, d := range out {
		if d.Email == "abuse-complaints@squarespace.com" {
			t.Error("registrar abuse contact should be dropped")
		}
		if d.Name == "Tuang Coffee (Pemilik)" {
			t.Error("business name with role suffix should not survive as a person name")
		}
	}

	var kept bool
	for _, d := range out {
		if d.Name == "Enristia P" {
			kept = true
		}
	}
	if !kept {
		t.Error("real named owner should be kept")
	}
}
