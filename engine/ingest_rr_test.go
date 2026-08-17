package engine

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseGLEIFRRKeepsActiveParents(t *testing.T) {
	csv := `Relationship.StartNode.NodeID,Relationship.EndNode.NodeID,Relationship.RelationshipType,Relationship.RelationshipStatus
549300CHILD0000000001,549300PARENT000000001,IS_ULTIMATELY_CONSOLIDATED_BY,ACTIVE
549300CHILD0000000002,549300PARENT000000001,IS_DIRECTLY_CONSOLIDATED_BY,ACTIVE
549300CHILD0000000003,549300PARENT000000001,IS_FUND-MANAGED_BY,ACTIVE
549300CHILD0000000004,549300PARENT000000001,IS_ULTIMATELY_CONSOLIDATED_BY,RETIRED
`
	edges, err := parseGLEIFRR(strings.NewReader(csv))
	if err != nil {
		t.Fatal(err)
	}
	if len(edges) != 2 {
		t.Fatalf("%+v", edges)
	}
	if edges[0].Child != "549300CHILD0000000001" || edges[0].Parent != "549300PARENT000000001" {
		t.Fatalf("%+v", edges[0])
	}
}

func TestInheritParentSocialsCopiesToBareChild(t *testing.T) {
	dir, err := OpenDirectory(filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	if _, err := dir.InsertBatch(context.Background(), []Merchant{
		{ExtID: "gleif:549300PARENT000000001", Source: "gleif", Name: "Signify Holding B.V.", Country: "NL", Shop: "GENERAL",
			Homepage: "https://www.signify.com",
			Profiles: []Profile{
				{ExtID: "gleif:549300PARENT000000001", Platform: PlatformFacebook, URL: "https://www.facebook.com/signify", Handle: "signify", Verified: true, Source: "website"},
				{ExtID: "gleif:549300PARENT000000001", Platform: PlatformWebsite, URL: "https://www.signify.com", Source: "website"},
			}},
		{ExtID: "gleif:549300CHILD0000000001", Source: "gleif", Name: "Signify Belgium", Country: "BE", Shop: "GENERAL",
			Homepage: "https://search.gleif.org/#/record/549300CHILD0000000001"},
		{ExtID: "gleif:549300FUND00000000001", Source: "gleif", Name: "Signify Fund", Country: "LU", Shop: "FUND",
			Homepage: "https://search.gleif.org/#/record/549300FUND00000000001"},
	}); err != nil {
		t.Fatal(err)
	}
	matched, profiles, err := dir.inheritParentSocials(context.Background(), []gleifRel{
		{Child: "549300CHILD0000000001", Parent: "549300PARENT000000001"},
		{Child: "549300FUND00000000001", Parent: "549300PARENT000000001"},
	})
	if err != nil || matched != 1 || profiles < 1 {
		t.Fatalf("matched=%d profiles=%d err=%v", matched, profiles, err)
	}
	got, err := dir.ProfilesFor(context.Background(), []string{"gleif:549300CHILD0000000001", "gleif:549300FUND00000000001"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got["gleif:549300CHILD0000000001"]) == 0 {
		t.Fatal("child missing inherited socials")
	}
	if len(got["gleif:549300FUND00000000001"]) != 0 {
		t.Fatalf("fund inherited brand page: %+v", got["gleif:549300FUND00000000001"])
	}
}

func TestMergeWikidataGlobalSocialAndLEI(t *testing.T) {
	raw := []byte(`{"results":{"bindings":[{
		"item":{"value":"http://www.wikidata.org/entity/Q123"},
		"itemLabel":{"value":"Signify"},
		"lei":{"value":"549300PARENT00000001"},
		"cc":{"value":"NL"},
		"val":{"value":"signify"}
	}]}}`)
	byQID := map[string]*Merchant{}
	leiByQID := map[string]string{}
	n := mergeWikidataGlobalSocial(byQID, leiByQID, raw, "https://www.facebook.com/", PlatformFacebook)
	if n != 1 || byQID["Q123"] == nil || leiByQID["Q123"] != "549300PARENT00000001" {
		t.Fatalf("n=%d by=%+v lei=%v", n, byQID, leiByQID)
	}
	if len(byQID["Q123"].Profiles) != 1 || !strings.Contains(byQID["Q123"].Profiles[0].URL, "facebook.com/signify") {
		t.Fatalf("%+v", byQID["Q123"])
	}
}

func TestMergeWikidataGlobalSocialWebsite(t *testing.T) {
	raw := []byte(`{"results":{"bindings":[{
		"item":{"value":"http://www.wikidata.org/entity/Q123"},
		"itemLabel":{"value":"Signify"},
		"lei":{"value":"549300PARENT00000001"},
		"cc":{"value":"NL"},
		"val":{"value":"https://www.signify.com"}
	}]}}`)
	byQID := map[string]*Merchant{}
	leiByQID := map[string]string{}
	n := mergeWikidataGlobalSocial(byQID, leiByQID, raw, "", PlatformWebsite)
	if n != 1 || byQID["Q123"] == nil || !strings.Contains(byQID["Q123"].Homepage, "signify.com") {
		t.Fatalf("n=%d by=%+v", n, byQID)
	}
	if leiByQID["Q123"] != "549300PARENT00000001" {
		t.Fatalf("lei=%v", leiByQID)
	}
}

func TestEnsureWikidataSPARQLPrefixes(t *testing.T) {
	got := ensureWikidataSPARQLPrefixes("SELECT ?x WHERE { ?x wdt:P856 ?v }")
	if !strings.Contains(got, "PREFIX wdt:") || !strings.Contains(got, "SELECT ?x") {
		t.Fatal(got)
	}
	if ensureWikidataSPARQLPrefixes("PREFIX wdt: <x>\nSELECT ?x") != "PREFIX wdt: <x>\nSELECT ?x" {
		t.Fatal("should keep existing prefix")
	}
}

func TestShouldShardWikidataSocial(t *testing.T) {
	if !shouldShardWikidataSocial(0, 0) {
		t.Fatal("empty first page should shard")
	}
	if shouldShardWikidataSocial(10, 0) || shouldShardWikidataSocial(0, 80000) {
		t.Fatal("later empty pages should not shard")
	}
}

func TestWikidataGlobalSocialSPARQLShards(t *testing.T) {
	q := wikidataGlobalSocialSPARQL("P2013", "s", 0)
	if strings.Contains(q, "P856") {
		t.Fatal("global social dump should not include unfiltered websites")
	}
	if !strings.Contains(q, "P2013") || !strings.Contains(q, `STRSTARTS(LCASE(STR(?val)), "s")`) {
		t.Fatal(q)
	}
	if strings.Contains(wikidataGlobalSocialSPARQL("P2013", "", 80000), "STRSTARTS") {
		t.Fatal("empty prefix should not shard")
	}
	if !strings.Contains(wikidataGlobalSocialSPARQL("P2013", "", 80000), "OFFSET 80000") {
		t.Fatal("missing offset")
	}
	if !strings.Contains(q, "zhLabel") {
		t.Fatal("missing Chinese label fallback")
	}
	all := wikidataSocialSPARQL("P7085", "", 0, false)
	if strings.Contains(all, "FILTER NOT EXISTS") || !strings.Contains(all, "P7085") {
		t.Fatal(all)
	}
	official := wikidataOfficialShortVideoSPARQL()
	if !strings.Contains(official, "P856") || !strings.Contains(official, "tiktok.com/@") || !strings.Contains(official, "douyin.com/user/") {
		t.Fatal(official)
	}
}

func TestMergeWikidataSocialFallbackName(t *testing.T) {
	raw := []byte(`{"results":{"bindings":[{
		"item":{"value":"http://www.wikidata.org/entity/Q9"},
		"zhLabel":{"value":"东成电动工具"},
		"cc":{"value":"CN"},
		"val":{"value":"dongcheng"}
	}]}}`)
	byQID := map[string]*Merchant{}
	n := mergeWikidataGlobalSocial(byQID, map[string]string{}, raw, "https://www.tiktok.com/@", PlatformTikTok)
	if n != 1 || byQID["Q9"] == nil || byQID["Q9"].Name != "东成电动工具" {
		t.Fatalf("n=%d by=%+v", n, byQID)
	}
	if socialFallbackName("https://www.tiktok.com/@bosch", "Q1") != "bosch" {
		t.Fatal(socialFallbackName("https://www.tiktok.com/@bosch", "Q1"))
	}
}

func TestOSMContactQueryUsesContactTags(t *testing.T) {
	q := osmContactQuery(ingestBox{city: "t", country: "DE", south: 1, west: 2, north: 3, east: 4})
	for _, want := range []string{"contact:facebook", "contact:instagram", "contact:linkedin", "1.0000,2.0000,3.0000,4.0000"} {
		if !strings.Contains(q, want) {
			t.Fatalf("missing %s in %s", want, q)
		}
	}
}

func TestAttachUniqueNameSocialsCopiesGLEIFSibling(t *testing.T) {
	dir, err := OpenDirectory(filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	if _, err := dir.InsertBatch(context.Background(), []Merchant{
		{ExtID: "gleif:parent", Source: "gleif", Name: "Acme Lighting GmbH", Country: "DE",
			Homepage: "https://www.acme-lighting.de",
			Profiles: []Profile{{ExtID: "gleif:parent", Platform: PlatformFacebook, URL: "https://www.facebook.com/acmelighting", Handle: "acmelighting", Verified: true, Source: "website"}}},
		{ExtID: "gleif:child", Source: "gleif", Name: "Acme Lighting UG", Country: "DE",
			Homepage: "https://search.gleif.org/#/record/child"},
	}); err != nil {
		t.Fatal(err)
	}
	st := dir.attachUniqueNameSocials(context.Background())
	if st.Err != "" || st.Rows != 1 {
		t.Fatalf("%+v", st)
	}
	got, err := dir.ProfilesFor(context.Background(), []string{"gleif:child"})
	if err != nil || len(got["gleif:child"]) == 0 {
		t.Fatalf("sibling not copied: %+v err=%v", got, err)
	}
}
