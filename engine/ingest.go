package engine

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"
)

// IngestStats is one source's full-dump timing.
type IngestStats struct {
	Source string        `json:"source"`
	Rows   int           `json:"rows"`
	Took   time.Duration `json:"took"`
	Note   string        `json:"note,omitempty"`
	Err    string        `json:"error,omitempty"`
}

func (s IngestStats) String() string {
	if s.Err != "" {
		return fmt.Sprintf("%s: FAIL %s (%s)", s.Source, s.Err, s.Took.Round(time.Millisecond))
	}
	return fmt.Sprintf("%s: %d rows in %s %s", s.Source, s.Rows, s.Took.Round(time.Millisecond), s.Note)
}

// IngestOptions controls a full merchant dump (all shop types, not one category).
type IngestOptions struct {
	DBPath            string
	GLEIFZip          string
	GLEIFLimit        int
	SkipGLEIF         bool
	SkipOSM           bool
	SkipWikidata      bool
	WikidataLEI       bool
	PublicSocials     bool
	AttachOnly        bool
	MaxPublic         bool
	RROnly            bool
	MoreSocials       bool
	RORZip            string
	RRZip             string
	Overpass          bool
	OSMBoxes          []ingestBox
	OSMLimitPerCity   int
	WebsiteLimit      int
	WebsiteWorkers    int
	WorldCompanies    bool
	Sherlock          bool
	SherlockLimit     int
	SherlockNameLimit int
	SherlockWorkers   int
	AttachSocials     bool
	AttachLimit       int
	AttachWorkers     int
}

// DefaultIngestOptions dumps GLEIF Golden Copy plus OSM shops in major cities.
func DefaultIngestOptions() IngestOptions {
	return IngestOptions{
		DBPath:          ResolveMerchantDB(""),
		Overpass:        true,
		OSMBoxes:        allIngestShopBoxes(),
		OSMLimitPerCity: 1500,
	}
}

// IngestMerchants writes every connected public source into the local directory.
func (c *Client) IngestMerchants(ctx context.Context, opt IngestOptions) ([]IngestStats, error) {
	if opt.DBPath == "" {
		opt.DBPath = ResolveMerchantDB("")
	}
	if len(opt.OSMBoxes) == 0 {
		opt.OSMBoxes = allIngestShopBoxes()
	}
	if opt.OSMLimitPerCity <= 0 {
		opt.OSMLimitPerCity = 1500
	}
	dir, err := OpenDirectory(opt.DBPath)
	if err != nil {
		return nil, err
	}
	defer dir.Close()

	needBulk := !opt.SkipGLEIF || (opt.Overpass && !opt.SkipOSM) ||
		(!opt.SkipWikidata && c != nil && strings.TrimSpace(c.WikidataURL) != "") ||
		(opt.PublicSocials && !opt.AttachOnly && c != nil && strings.TrimSpace(c.WikidataURL) != "")
	if needBulk {
		if err := dir.beginBulk(ctx); err != nil {
			return nil, err
		}
	}

	var stats []IngestStats
	if !opt.SkipGLEIF {
		stats = append(stats, c.ingestGLEIF(ctx, dir, opt))
	}
	if opt.Overpass && !opt.SkipOSM {
		stats = append(stats, c.ingestOSMAllShops(ctx, dir, opt))
	}
	if !opt.SkipWikidata && c != nil && strings.TrimSpace(c.WikidataURL) != "" {
		stats = append(stats, c.ingestWikidataSEA(ctx, dir))
	}
	if opt.PublicSocials && !opt.AttachOnly && c != nil && strings.TrimSpace(c.WikidataURL) != "" {
		stats = append(stats, c.ingestWikidataMarkets(ctx, dir))
	}

	if needBulk {
		if err := dir.endBulk(ctx); err != nil {
			return stats, fmt.Errorf("rebuild fts: %w", err)
		}
	}
	if opt.WikidataLEI && c != nil && strings.TrimSpace(c.WikidataURL) != "" {
		stats = append(stats, c.ingestWikidataLEI(ctx, dir))
	}
	if opt.PublicSocials {
		stats = append(stats, c.ingestPublicSocials(ctx, dir, opt)...)
	}
	if opt.RROnly {
		stats = append(stats, dir.attachUniqueNameSocials(ctx))
		stats = append(stats, c.ingestGLEIFRelationships(ctx, dir, opt.RRZip))
		return stats, nil
	}
	if opt.WorldCompanies {
		stats = append(stats, c.ingestWikidataWorldCompanies(ctx, dir))
	}
	if opt.MaxPublic {
		stats = append(stats, c.ingestMaxPublic(ctx, dir, opt)...)
	}
	if opt.MoreSocials {
		stats = append(stats, c.ingestMoreSocials(ctx, dir, opt)...)
	}
	if opt.Sherlock {
		stats = append(stats, c.ingestSherlock(ctx, dir, opt))
	}
	if opt.AttachSocials {
		stats = append(stats, c.ingestAttachSocials(ctx, dir, opt))
	}
	return stats, nil
}

func (c *Client) ingestMaxPublic(ctx context.Context, dir *Directory, opt IngestOptions) []IngestStats {
	var stats []IngestStats
	if err := dir.beginBulk(ctx); err != nil {
		return []IngestStats{{Source: "public-max", Err: err.Error()}}
	}
	// LEI-keyed Wikidata first: highest-precision GLEIF join.
	if c != nil && strings.TrimSpace(c.WikidataURL) != "" {
		stats = append(stats, c.ingestWikidataLEI(ctx, dir))
	}
	// ROR websites + Wikidata P856 + unique same-name copy.
	stats = append(stats, c.ingestPublicSocials(ctx, dir, IngestOptions{
		PublicSocials: true,
		AttachOnly:    true,
		RORZip:        opt.RORZip,
	})...)
	// Companies with an official website (P856), class-filtered. Unfiltered
	// P856 is noisy (museums, people); this keeps business/enterprise only.
	if c != nil && strings.TrimSpace(c.WikidataURL) != "" {
		stats = append(stats, c.ingestWikidataWorldCompanies(ctx, dir))
	}
	// Global social identifiers (no unfiltered P856 — that dump is noisy).
	if c != nil && strings.TrimSpace(c.WikidataURL) != "" {
		stats = append(stats, c.ingestWikidataGlobalSocials(ctx, dir))
	}
	if !opt.SkipOSM {
		stats = append(stats, c.ingestOSMContacts(ctx, dir))
	}
	if err := dir.endBulk(ctx); err != nil {
		stats = append(stats, IngestStats{Source: "public-max-fts", Err: err.Error()})
	}
	stats = append(stats, c.ingestMoreSocials(ctx, dir, IngestOptions{
		SkipOSM:        true,
		RORZip:         opt.RORZip,
		RRZip:          opt.RRZip,
		WebsiteLimit:   opt.WebsiteLimit,
		WebsiteWorkers: opt.WebsiteWorkers,
	})...)
	return stats
}

func (c *Client) ingestMoreSocials(ctx context.Context, dir *Directory, opt IngestOptions) []IngestStats {
	var stats []IngestStats
	if !opt.SkipOSM {
		stats = append(stats, c.ingestOSMContactGaps(ctx, dir))
	}
	if c != nil && strings.TrimSpace(c.WikidataURL) != "" {
		stats = append(stats, c.ingestWikidataParentSocials(ctx, dir))
	}
	stats = append(stats, c.ingestSECTickers(ctx, dir))
	stats = append(stats, c.ingestWebsiteSocials(ctx, dir, opt))
	stats = append(stats, c.ingestSherlock(ctx, dir, opt))
	stats = append(stats, dir.attachUniqueNameSocials(ctx))
	stats = append(stats, c.ingestGLEIFRelationships(ctx, dir, opt.RRZip))
	return stats
}

func logIngest(format string, args ...any) {
	fmt.Fprintf(os.Stderr, time.Now().Format("15:04:05")+" "+format+"\n", args...)
}
