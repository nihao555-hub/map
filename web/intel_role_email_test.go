package web

import "testing"

func TestTradeRoleEmailsAreGenericNotDecisionMakers(t *testing.T) {
	place := Place{Title: "PT Artan", Phone: "+62811", WhatsApp: "+62811", Website: "https://artanin.com/"}
	emails := []string{
		"purchase@artanin.com",
		"procurement@artanin.com",
		"buyer@artanin.com",
		"export@artanin.com",
		"sales@artanin.com",
		"contact@artan-in.com",
	}
	for _, e := range emails {
		if !isGenericOfficeEmailLocal(emailLocal(e)) {
			t.Fatalf("%s should be generic office/role email", e)
		}
		if isPersonLikeEmailLocal(emailLocal(e)) {
			t.Fatalf("%s must not be person-like", e)
		}
	}
	makers := heuristicDecisionMakers(nil, emails, place)
	makers = sanitizeDecisionMakers(makers, place)
	makers = pruneOfficeInboxesWhenPeopleExist(makers)
	named := 0
	for _, d := range makers {
		if d.Name != "" {
			named++
			t.Fatalf("role emails must not create named makers: %+v", d)
		}
	}
	if len(makers) > 2 {
		t.Fatalf("too many channel makers from role emails: %d %+v", len(makers), makers)
	}
	_ = named
}

func TestSeedTradeRoleEmailsCapped(t *testing.T) {
	got := seedTradeRoleEmails("example.com")
	if len(got) > 2 {
		t.Fatalf("seed too many: %v", got)
	}
}
