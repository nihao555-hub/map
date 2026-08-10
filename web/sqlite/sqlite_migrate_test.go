package sqlite_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/gosom/google-maps-scraper/web/sqlite"
)

func TestMigrateLegacyJobsAddsOwner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE jobs (
		id TEXT PRIMARY KEY, name TEXT NOT NULL, status TEXT NOT NULL,
		data TEXT NOT NULL, created_at INT NOT NULL, updated_at INT NOT NULL
	)`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO jobs VALUES ('x','n','pending','{"keywords":["a"],"lang":"en","zoom":1,"lat":"1","lon":"1","depth":1,"max_time":60000000000}',1,1)`)
	if err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	store, err := sqlite.New(path)
	if err != nil {
		t.Fatalf("open migrate: %v", err)
	}
	job, err := store.Get(context.Background(), "x")
	if err != nil {
		t.Fatal(err)
	}
	if job.Owner != "" {
		t.Fatalf("legacy owner=%q", job.Owner)
	}
	job.Owner = "GMS-TEST-TEST"
	job.Date = time.Unix(1, 0).UTC()
	if err := store.Update(context.Background(), &job); err != nil {
		t.Fatal(err)
	}
}
