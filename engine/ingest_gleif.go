package engine

import (
	"archive/zip"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const gleifLatestCSV = "https://goldencopy.gleif.org/api/v2/golden-copies/publishes/lei2/latest.csv"

func (c *Client) ingestGLEIF(ctx context.Context, dir *Directory, opt IngestOptions) IngestStats {
	started := time.Now()
	path := strings.TrimSpace(opt.GLEIFZip)
	if path == "" {
		cache := filepath.Join(os.TempDir(), "merchant-ingest", "gleif-lei2.csv.zip")
		if err := downloadGLEIF(ctx, c.httpClient(), cache); err != nil {
			return IngestStats{Source: "gleif", Took: time.Since(started), Err: err.Error()}
		}
		path = cache
	}
	n, err := importGLEIFZip(ctx, dir, path, opt.GLEIFLimit)
	st := IngestStats{Source: "gleif", Rows: n, Took: time.Since(started), Note: filepath.Base(path)}
	if err != nil {
		st.Err = err.Error()
	}
	_ = dir.RecordRun(ctx, "gleif", started, n, st.Err)
	return st
}

func downloadGLEIF(ctx context.Context, httpc *http.Client, dest string) error {
	if st, err := os.Stat(dest); err == nil && st.Size() > 100*1024*1024 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, gleifLatestCSV, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; gmaps-engine/1.0)")
	req.Header.Set("Accept", "*/*")
	resp, err := httpc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("gleif download: %s", resp.Status)
	}
	tmp := dest + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(f, resp.Body)
	closeErr := f.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return closeErr
	}
	return os.Rename(tmp, dest)
}

func importGLEIFZip(ctx context.Context, dir *Directory, zipPath string, limit int) (int, error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return 0, err
	}
	defer zr.Close()

	var csvFile *zip.File
	for i := range zr.File {
		name := strings.ToLower(zr.File[i].Name)
		if strings.HasSuffix(name, ".csv") {
			csvFile = zr.File[i]
			break
		}
	}
	if csvFile == nil {
		return 0, fmt.Errorf("gleif zip 里没有 csv")
	}
	rc, err := csvFile.Open()
	if err != nil {
		return 0, err
	}
	defer rc.Close()

	reader := csv.NewReader(rc)
	reader.ReuseRecord = true
	reader.LazyQuotes = true
	reader.FieldsPerRecord = -1

	header, err := reader.Read()
	if err != nil {
		return 0, err
	}
	idx := gleifColumnIndex(header)
	if idx.name < 0 || idx.lei < 0 {
		return 0, fmt.Errorf("gleif csv 缺 LegalName/LEI 列: %v", header[:min(8, len(header))])
	}

	inserted := 0
	seen := 0
	batch := make([]Merchant, 0, 5000)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		n, err := dir.InsertBatch(ctx, batch)
		if err != nil {
			return err
		}
		inserted += n
		batch = batch[:0]
		return nil
	}

	for {
		if err := ctx.Err(); err != nil {
			_ = flush()
			return inserted, err
		}
		rec, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			continue
		}
		lei := col(rec, idx.lei)
		name := col(rec, idx.name)
		if lei == "" || name == "" {
			continue
		}
		seen++
		batch = append(batch, Merchant{
			ExtID:    "gleif:" + lei,
			Source:   "gleif",
			Name:     name,
			Shop:     firstNonEmpty(col(rec, idx.category), "legal-entity"),
			Country:  strings.ToUpper(col(rec, idx.country)),
			City:     col(rec, idx.city),
			Homepage: "https://search.gleif.org/#/record/" + lei,
		})
		if len(batch) >= 5000 {
			if err := flush(); err != nil {
				return inserted, err
			}
			if seen%100000 == 0 {
				logIngest("GLEIF parsed=%d inserted=%d", seen, inserted)
			}
		}
		if limit > 0 && seen >= limit {
			break
		}
	}
	if err := flush(); err != nil {
		return inserted, err
	}
	logIngest("GLEIF done parsed=%d inserted=%d", seen, inserted)
	return inserted, nil
}

type gleifIdx struct {
	lei, name, country, city, category int
}

func gleifColumnIndex(header []string) gleifIdx {
	idx := gleifIdx{lei: -1, name: -1, country: -1, city: -1, category: -1}
	for i, raw := range header {
		h := strings.ToLower(strings.TrimSpace(raw))
		switch {
		case h == "lei":
			idx.lei = i
		case h == "entity.legalname":
			idx.name = i
		case h == "entity.legaladdress.country":
			idx.country = i
		case h == "entity.legaladdress.city":
			idx.city = i
		case h == "entity.entitycategory":
			idx.category = i
		}
	}
	return idx
}

func col(rec []string, i int) string {
	if i < 0 || i >= len(rec) {
		return ""
	}
	return strings.Clone(strings.TrimSpace(rec[i]))
}
