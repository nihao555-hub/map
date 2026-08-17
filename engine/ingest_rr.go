package engine

import (
	"archive/zip"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const gleifLatestRR = "https://goldencopy.gleif.org/api/v2/golden-copies/publishes/rr/latest.csv"

var gleifInheritRelTypes = map[string]bool{
	"IS_ULTIMATELY_CONSOLIDATED_BY": true,
	"IS_DIRECTLY_CONSOLIDATED_BY":   true,
	"IS_INTERNATIONAL_BRANCH_OF":    true,
}

func (c *Client) ingestGLEIFRelationships(ctx context.Context, dir *Directory, zipPath string) IngestStats {
	started := time.Now()
	path := strings.TrimSpace(zipPath)
	if path == "" {
		path = filepath.Join(os.TempDir(), "merchant-ingest", "gleif-rr.csv.zip")
	}
	if st, err := os.Stat(path); err != nil || st.Size() < 1024*1024 {
		if c == nil {
			return IngestStats{Source: "gleif-rr", Took: time.Since(started), Err: "missing rr zip"}
		}
		if err := downloadCachedURL(ctx, c.httpClient(), gleifLatestRR, path, 1024*1024); err != nil {
			st := IngestStats{Source: "gleif-rr", Took: time.Since(started), Err: err.Error()}
			_ = dir.RecordRun(ctx, "gleif-rr", started, 0, st.Err)
			return st
		}
	}
	edges, err := parseGLEIFRRZip(path)
	if err != nil {
		st := IngestStats{Source: "gleif-rr", Took: time.Since(started), Err: err.Error()}
		_ = dir.RecordRun(ctx, "gleif-rr", started, 0, st.Err)
		return st
	}
	matched, profiles, err := dir.inheritParentSocials(ctx, edges)
	st := IngestStats{
		Source: "gleif-rr",
		Rows:   matched,
		Took:   time.Since(started),
		Note:   fmt.Sprintf("edges=%d profiles=%d", len(edges), profiles),
	}
	if err != nil {
		st.Err = err.Error()
	}
	_ = dir.RecordRun(ctx, "gleif-rr", started, matched, st.Note)
	logIngest("GLEIF RR inherited %d children (%d profiles, %d edges)", matched, profiles, len(edges))
	return st
}

type gleifRel struct {
	Child  string
	Parent string
}

func parseGLEIFRRZip(zipPath string) ([]gleifRel, error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	var csvFile *zip.File
	for i := range zr.File {
		if strings.HasSuffix(strings.ToLower(zr.File[i].Name), ".csv") {
			csvFile = zr.File[i]
			break
		}
	}
	if csvFile == nil {
		return nil, fmt.Errorf("gleif rr zip 里没有 csv")
	}
	rc, err := csvFile.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return parseGLEIFRR(rc)
}

func parseGLEIFRR(r io.Reader) ([]gleifRel, error) {
	reader := csv.NewReader(r)
	reader.ReuseRecord = true
	reader.LazyQuotes = true
	reader.FieldsPerRecord = -1
	header, err := reader.Read()
	if err != nil {
		return nil, err
	}
	idx := gleifRRColumnIndex(header)
	if idx.child < 0 || idx.parent < 0 || idx.kind < 0 {
		return nil, fmt.Errorf("gleif rr csv 缺关系列: %v", header[:min(8, len(header))])
	}
	out := make([]gleifRel, 0, 200000)
	seen := map[string]struct{}{}
	for {
		rec, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			continue
		}
		kind := strings.ToUpper(col(rec, idx.kind))
		if !gleifInheritRelTypes[kind] {
			continue
		}
		if idx.status >= 0 {
			st := strings.ToUpper(col(rec, idx.status))
			if st != "" && st != "ACTIVE" {
				continue
			}
		}
		child := strings.ToUpper(col(rec, idx.child))
		parent := strings.ToUpper(col(rec, idx.parent))
		if len(child) < 18 || len(parent) < 18 || child == parent {
			continue
		}
		key := child + ">" + parent
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, gleifRel{Child: child, Parent: parent})
	}
	return out, nil
}

type gleifRRIdx struct {
	child, parent, kind, status int
}

func gleifRRColumnIndex(header []string) gleifRRIdx {
	idx := gleifRRIdx{child: -1, parent: -1, kind: -1, status: -1}
	for i, raw := range header {
		h := strings.ToLower(strings.TrimSpace(raw))
		switch h {
		case "relationship.startnode.nodeid":
			idx.child = i
		case "relationship.endnode.nodeid":
			idx.parent = i
		case "relationship.relationshiptype":
			idx.kind = i
		case "relationship.relationshipstatus":
			idx.status = i
		}
	}
	return idx
}

func (d *Directory) inheritParentSocials(ctx context.Context, edges []gleifRel) (matched, profiles int, err error) {
	if d == nil || len(edges) == 0 {
		return 0, 0, nil
	}
	funds, err := d.loadGLEIFFunds(ctx)
	if err != nil {
		return 0, 0, err
	}
	byParent, err := d.loadGLEIFProfileMap(ctx)
	if err != nil {
		return 0, 0, err
	}
	have := map[string]struct{}{}
	for id := range byParent {
		have[id] = struct{}{}
	}
	var rows []Merchant
	seenChild := map[string]struct{}{}
	for _, e := range edges {
		childID := "gleif:" + e.Child
		parentID := "gleif:" + e.Parent
		if _, ok := funds[childID]; ok {
			continue
		}
		if _, ok := have[childID]; ok {
			continue
		}
		if _, ok := seenChild[childID]; ok {
			continue
		}
		donors := byParent[parentID]
		if len(donors) == 0 {
			continue
		}
		seenChild[childID] = struct{}{}
		m := Merchant{ExtID: childID, Source: "gleif"}
		for _, p := range donors {
			p.ExtID = childID
			p.Source = "gleif-rr"
			m.Profiles = append(m.Profiles, p)
			if (m.Homepage == "" || registryOnlyHomepage(m.Homepage)) && p.Platform == PlatformWebsite && isRealHomepage(p.URL) {
				m.Homepage = p.URL
			}
			if m.Homepage == "" && isRealHomepage(p.URL) && p.Platform != PlatformWebsite {
				m.Homepage = p.URL
			}
		}
		if len(m.Profiles) == 0 {
			continue
		}
		rows = append(rows, m)
	}
	return d.attachExisting(ctx, rows)
}

func (d *Directory) loadGLEIFFunds(ctx context.Context) (map[string]struct{}, error) {
	rows, err := d.db.QueryContext(ctx, `SELECT ext_id FROM merchants WHERE source='gleif' AND upper(shop)='FUND'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]struct{}{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = struct{}{}
	}
	return out, rows.Err()
}

func (d *Directory) loadGLEIFProfileMap(ctx context.Context) (map[string][]Profile, error) {
	rows, err := d.db.QueryContext(ctx, `
SELECT p.ext_id, p.platform, p.url, p.handle, p.verified, p.source
FROM merchant_profiles p
JOIN merchants m ON m.ext_id=p.ext_id
WHERE m.source='gleif'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]Profile{}
	for rows.Next() {
		var p Profile
		var verified int
		if err := rows.Scan(&p.ExtID, &p.Platform, &p.URL, &p.Handle, &verified, &p.Source); err != nil {
			return nil, err
		}
		if registryOnlyHomepage(p.URL) {
			continue
		}
		p.Verified = verified == 1
		out[p.ExtID] = append(out[p.ExtID], p)
	}
	return out, rows.Err()
}
