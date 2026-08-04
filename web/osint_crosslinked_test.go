package web

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseCrossLinkedCSV(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "names.csv")
	content := `Datetime,Search,Name,Title,URL,rawText
"08-04-2026 01:00:00","bing","budi santoso","Purchasing Manager","https://www.linkedin.com/in/budi-santoso-123","Budi Santoso - Purchasing Manager - PT Contoh",
"08-04-2026 01:00:00","bing","not-a-person","","https://www.linkedin.com/in/x","noise",
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := parseCrossLinkedCSV(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %#v", got)
	}
	if got[0].Name != "Budi Santoso" {
		t.Fatalf("name=%q", got[0].Name)
	}
	if got[0].Source != "crosslinked" {
		t.Fatalf("source=%q", got[0].Source)
	}
	if !strings.Contains(got[0].LinkedIn, "/in/budi-santoso") {
		t.Fatalf("li=%q", got[0].LinkedIn)
	}
}

func TestTitleCasePersonName(t *testing.T) {
	if titleCasePersonName("budi santoso") != "Budi Santoso" {
		t.Fatal(titleCasePersonName("budi santoso"))
	}
}

func TestMaigretUsernamesForPerson(t *testing.T) {
	d := DecisionMaker{
		Name:     "Budi Santoso",
		LinkedIn: "https://www.linkedin.com/in/budi-santoso-a1b2",
		Email:    "budi.santoso@example.com",
	}
	users := maigretUsernamesFor(d)
	if len(users) == 0 {
		t.Fatal("expected usernames")
	}
	joined := strings.Join(users, ",")
	if !strings.Contains(joined, "budisantoso") && !strings.Contains(joined, "budi.santoso") {
		t.Fatalf("unexpected users: %v", users)
	}
	d2 := DecisionMaker{Name: "Budi Santoso", Email: "info@example.com"}
	for _, u := range maigretUsernamesFor(d2) {
		if u == "info" {
			t.Fatal("generic email local leaked as username")
		}
	}
}

func TestFilterMaigretProfileURLs(t *testing.T) {
	got := filterMaigretProfileURLs([]string{"GitHub", "https://github.com/foo", "twitter"})
	if len(got) != 1 || got[0] != "https://github.com/foo" {
		t.Fatalf("got %v", got)
	}
}

func TestCrossLinkedAvailableLocal(t *testing.T) {
	if !fileExists(filepath.Join(repoRoot(), "tools", "CrossLinked", "crosslinked.py")) {
		t.Skip("CrossLinked repo not cloned")
	}
	if !CrossLinkedAvailable() {
		t.Fatal("CrossLinked repo present but CrossLinkedAvailable=false — install crosslinked-venv")
	}
}
