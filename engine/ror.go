package engine

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// DefaultRORDump is the latest CC0 ROR registry dump (Zenodo, 2026-08-03).
	DefaultRORDump = "https://zenodo.org/records/16736416/files/v2.11-2026-08-03-ror-data.zip?download=1"
	rorUserAgent   = "google-maps-scraper-engine/1.0 (https://github.com/gosom/google-maps-scraper)"
)

// ROROrg is the subset of a ROR v2 record we need: LEI + official website.
type ROROrg struct {
	LEI     string
	Name    string
	Website string
	Country string
	RORID   string
}

type rorRecord struct {
	ID          string    `json:"id"`
	Names       []rorName `json:"names"`
	Links       []rorLink `json:"links"`
	Locations   []rorLoc  `json:"locations"`
	ExternalIDs []rorExt  `json:"external_ids"`
}

type rorName struct {
	Value string   `json:"value"`
	Types []string `json:"types"`
}

type rorLink struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

type rorLoc struct {
	Geonames struct {
		CountryCode string `json:"country_code"`
	} `json:"geonames_details"`
}

type rorExt struct {
	Type      string   `json:"type"`
	Preferred string   `json:"preferred"`
	All       []string `json:"all"`
}

func downloadRORDump(ctx context.Context, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, DefaultRORDump, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", rorUserAgent)
	req.Header.Set("Accept", "application/zip")
	client := &http.Client{Timeout: 8 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ror dump: HTTP %d", resp.StatusCode)
	}
	tmp := dest + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dest)
}

func parseRORDump(path string) ([]ROROrg, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer zr.Close()

	var zf *zip.File
	for i := range zr.File {
		name := strings.ToLower(zr.File[i].Name)
		if strings.HasSuffix(name, ".json") && !strings.Contains(name, "schema") {
			zf = zr.File[i]
			break
		}
	}
	if zf == nil {
		return nil, fmt.Errorf("ror dump: no json payload in %s", path)
	}
	rc, err := zf.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	dec := json.NewDecoder(rc)
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if d, ok := tok.(json.Delim); !ok || d != '[' {
		return nil, fmt.Errorf("ror dump: expected JSON array")
	}

	out := make([]ROROrg, 0, 4096)
	for dec.More() {
		var rec rorRecord
		if err := dec.Decode(&rec); err != nil {
			return nil, err
		}
		lei := rorLEI(rec)
		if lei == "" {
			continue
		}
		org := ROROrg{
			LEI:     lei,
			Name:    rorDisplayName(rec),
			Website: rorWebsite(rec),
			Country: rorCountry(rec),
			RORID:   rec.ID,
		}
		if org.Website == "" {
			continue
		}
		out = append(out, org)
	}
	return out, nil
}

func rorLEI(rec rorRecord) string {
	for _, ext := range rec.ExternalIDs {
		if !strings.EqualFold(ext.Type, "lei") {
			continue
		}
		if v := strings.TrimSpace(ext.Preferred); v != "" {
			return strings.ToUpper(v)
		}
		for _, v := range ext.All {
			if v = strings.TrimSpace(v); v != "" {
				return strings.ToUpper(v)
			}
		}
	}
	return ""
}

func rorDisplayName(rec rorRecord) string {
	var fallback string
	for _, n := range rec.Names {
		if n.Value == "" {
			continue
		}
		if fallback == "" {
			fallback = n.Value
		}
		for _, t := range n.Types {
			if t == "ror_display" || t == "label" {
				return n.Value
			}
		}
	}
	return fallback
}

func rorWebsite(rec rorRecord) string {
	for _, l := range rec.Links {
		if strings.EqualFold(l.Type, "website") && strings.TrimSpace(l.Value) != "" {
			return strings.TrimSpace(l.Value)
		}
	}
	return ""
}

func rorCountry(rec rorRecord) string {
	for _, loc := range rec.Locations {
		if c := strings.ToUpper(strings.TrimSpace(loc.Geonames.CountryCode)); len(c) == 2 {
			return c
		}
	}
	return ""
}
