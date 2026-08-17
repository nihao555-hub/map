package engine

import "testing"

func TestNormalizeKeyword(t *testing.T) {
	if got := NormalizeKeyword("  配电  柜  "); got != "配电 柜" {
		t.Fatalf("got %q", got)
	}
	if got := NormalizeKeyword("配\u200b电柜"); got != "配电柜" {
		t.Fatalf("zero-width got %q", got)
	}
}

func TestValidateKeyword(t *testing.T) {
	if err := ValidateKeyword("配电柜", false); err != nil {
		t.Fatal(err)
	}
	if err := ValidateKeyword("power tools", false); err != nil {
		t.Fatal(err)
	}
	if err := ValidateKeyword("https://www.douyin.com/user/MS4wLjABAAAAtest", false); err != nil {
		t.Fatal(err)
	}
	if err := ValidateKeyword("LED", true); err != nil {
		t.Fatal(err)
	}
	if err := ValidateKeyword("配电", true); err == nil {
		t.Fatal("precise should require 3 runes")
	}
	if err := ValidateKeyword("配电柜", true); err != nil {
		t.Fatal(err)
	}

	cases := []string{"", "  ", "啊", "!!", "the", "搜索", "12345", "啊啊", "aaa", "<script>", "javascript:alert(1)", "http://127.0.0.1/x"}
	for _, c := range cases {
		if err := ValidateKeyword(c, false); err == nil {
			t.Fatalf("expected error for %q", c)
		}
	}
	long := ""
	for i := 0; i < 70; i++ {
		long += "电"
	}
	if err := ValidateKeyword(long, false); err == nil {
		t.Fatal("expected too long")
	}
}

func TestFilterPreciseHits(t *testing.T) {
	hits := []Hit{
		{Name: "河北配电柜厂", HomepageURL: "https://www.douyin.com/user/a"},
		{Name: "某主播", Snippet: "日常vlog", HomepageURL: "https://www.douyin.com/user/b"},
		{Title: "power tools factory", Contact: "sales@example.com"},
	}
	got := filterPreciseHits(hits, "配电柜")
	if len(got) != 1 || got[0].Name != "河北配电柜厂" {
		t.Fatalf("got %+v", got)
	}
	got = filterPreciseHits(hits, "power tools")
	if len(got) != 1 || got[0].Title != "power tools factory" {
		t.Fatalf("got %+v", got)
	}
}
