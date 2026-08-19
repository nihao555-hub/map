package engine

import (
	"context"
	"database/sql"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// DefaultCustomsDB is the local SQLite dump for public customs/trade feeds.
const DefaultCustomsDB = "store/customs.db"

var customsDBFallbacks = []string{
	DefaultCustomsDB,
	"webdata/customs.db",
	"/tmp/gmaps-webdata/customs.db",
}

// ResolveCustomsDB picks an explicit path, then ENGINE_CUSTOMS_DB, then defaults.
func ResolveCustomsDB(explicit string) string {
	if p := strings.TrimSpace(explicit); p != "" {
		return p
	}
	if p := strings.TrimSpace(os.Getenv("ENGINE_CUSTOMS_DB")); p != "" {
		return p
	}
	for _, p := range customsDBFallbacks {
		if st, err := os.Stat(p); err == nil && st.Size() > 0 {
			return p
		}
	}
	return DefaultCustomsDB
}

// CustomsCompany is one importer/exporter row from a public registry.
type CustomsCompany struct {
	ExtID    string
	Country  string
	Name     string
	Address  string
	Postcode string
	Role     string
	Source   string
	Period   string
}

// CustomsCompanyProduct links a company to HS codes in a period.
type CustomsCompanyProduct struct {
	ExtID   string
	HSCode  string
	Source  string
	Period  string
	Country string
}

// CustomsTradeLine is an aggregated customs declaration line (no company name).
type CustomsTradeLine struct {
	ReporterCountry string
	Flow            string
	Period          string
	HSCode          string
	PartnerCountry  string
	OriginCountry   string
	PortCode        string
	StatValue       int64
	NetMass         int64
	RecordType      string
	Source          string
}

// CustomsShipmentRow is one persisted bill-of-lading / manifest row.
type CustomsShipmentRow struct {
	ExtID     string
	ShipDate  string
	Shipper   string
	Consignee string
	Product   string
	HSCode    string
	Origin    string
	Vessel    string
	Source    string
	Year      int
}

// CustomsCompanyStat stores importer profile rollups.
type CustomsCompanyStat struct {
	ExtID             string
	Year              int
	TotalShipments    int
	UniqueSuppliers   int
	MatchingShipments int
	Source            string
}

// CustomsStore is a local SQLite customs dump.
type CustomsStore struct {
	db *sql.DB
}

func OpenCustomsStore(path string) (*CustomsStore, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		path = ResolveCustomsDB("")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil && filepath.Dir(path) != "." {
		return nil, err
	}
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(8000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(customsSchema); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &CustomsStore{db: db}, nil
}

func (s *CustomsStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

const customsSchema = `
CREATE TABLE IF NOT EXISTS customs_companies (
  id INTEGER PRIMARY KEY,
  ext_id TEXT NOT NULL UNIQUE,
  country TEXT,
  name TEXT NOT NULL,
  address TEXT,
  postcode TEXT,
  role TEXT,
  source TEXT NOT NULL,
  period TEXT
);
CREATE INDEX IF NOT EXISTS customs_companies_country ON customs_companies(country);
CREATE INDEX IF NOT EXISTS customs_companies_source ON customs_companies(source);
CREATE INDEX IF NOT EXISTS customs_companies_period ON customs_companies(period);

CREATE TABLE IF NOT EXISTS customs_company_products (
  id INTEGER PRIMARY KEY,
  ext_id TEXT NOT NULL,
  hs_code TEXT NOT NULL,
  country TEXT,
  source TEXT NOT NULL,
  period TEXT,
  UNIQUE(ext_id, hs_code, period, source)
);
CREATE INDEX IF NOT EXISTS customs_company_products_ext ON customs_company_products(ext_id);
CREATE INDEX IF NOT EXISTS customs_company_products_hs ON customs_company_products(hs_code);

CREATE TABLE IF NOT EXISTS customs_trade_lines (
  id INTEGER PRIMARY KEY,
  reporter_country TEXT NOT NULL,
  flow TEXT NOT NULL,
  period TEXT NOT NULL,
  hs_code TEXT,
  partner_country TEXT,
  origin_country TEXT,
  port_code TEXT,
  stat_value INTEGER,
  net_mass INTEGER,
  record_type TEXT,
  source TEXT NOT NULL,
  UNIQUE(reporter_country, flow, period, hs_code, partner_country, origin_country, port_code, record_type, source)
);
CREATE INDEX IF NOT EXISTS customs_trade_lines_period ON customs_trade_lines(period);
CREATE INDEX IF NOT EXISTS customs_trade_lines_partner ON customs_trade_lines(partner_country);

CREATE TABLE IF NOT EXISTS customs_shipments (
  id INTEGER PRIMARY KEY,
  ext_id TEXT NOT NULL,
  ship_date TEXT,
  shipper TEXT,
  consignee TEXT,
  product TEXT,
  hs_code TEXT,
  origin_country TEXT,
  vessel TEXT,
  source TEXT NOT NULL,
  year INTEGER,
  UNIQUE(source, ext_id, ship_date, hs_code, shipper, product, consignee)
);
CREATE INDEX IF NOT EXISTS customs_shipments_ext ON customs_shipments(ext_id);
CREATE INDEX IF NOT EXISTS customs_shipments_year ON customs_shipments(year);

CREATE TABLE IF NOT EXISTS customs_company_stats (
  id INTEGER PRIMARY KEY,
  ext_id TEXT NOT NULL,
  year INTEGER NOT NULL,
  total_shipments INTEGER,
  unique_suppliers INTEGER,
  matching_shipments INTEGER,
  source TEXT NOT NULL,
  UNIQUE(ext_id, year, source)
);

CREATE TABLE IF NOT EXISTS customs_comtrade_flows (
  id INTEGER PRIMARY KEY,
  reporter_code TEXT NOT NULL,
  reporter_iso TEXT,
  reporter_name TEXT,
  partner_iso TEXT,
  partner_name TEXT,
  flow TEXT NOT NULL,
  period TEXT NOT NULL,
  hs_code TEXT NOT NULL,
  primary_value REAL,
  source TEXT NOT NULL DEFAULT 'comtrade',
  UNIQUE(reporter_code, partner_iso, flow, period, hs_code, source)
);
CREATE INDEX IF NOT EXISTS customs_comtrade_period ON customs_comtrade_flows(period);

CREATE TABLE IF NOT EXISTS customs_ingest_runs (
  id INTEGER PRIMARY KEY,
  source TEXT,
  started TEXT,
  finished TEXT,
  rows INTEGER,
  ms INTEGER,
  note TEXT
);
`

func (s *CustomsStore) UpsertCompanies(ctx context.Context, rows []CustomsCompany) (int, error) {
	if s == nil || len(rows) == 0 {
		return 0, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	stmt, err := tx.PrepareContext(ctx, `
INSERT INTO customs_companies(ext_id, country, name, address, postcode, role, source, period)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(ext_id) DO UPDATE SET
  country=excluded.country,
  name=excluded.name,
  address=CASE WHEN excluded.address != '' THEN excluded.address ELSE customs_companies.address END,
  postcode=CASE WHEN excluded.postcode != '' THEN excluded.postcode ELSE customs_companies.postcode END,
  role=excluded.role,
  source=excluded.source,
  period=excluded.period`)
	if err != nil {
		_ = tx.Rollback()
		return 0, err
	}
	defer stmt.Close()
	n := 0
	for _, row := range rows {
		if strings.TrimSpace(row.ExtID) == "" || strings.TrimSpace(row.Name) == "" {
			continue
		}
		if _, err := stmt.ExecContext(ctx, row.ExtID, row.Country, row.Name, row.Address, row.Postcode, row.Role, row.Source, row.Period); err != nil {
			_ = tx.Rollback()
			return n, err
		}
		n++
	}
	return n, tx.Commit()
}

func (s *CustomsStore) UpsertCompanyProducts(ctx context.Context, rows []CustomsCompanyProduct) (int, error) {
	if s == nil || len(rows) == 0 {
		return 0, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	stmt, err := tx.PrepareContext(ctx, `
INSERT OR IGNORE INTO customs_company_products(ext_id, hs_code, country, source, period)
VALUES (?, ?, ?, ?, ?)`)
	if err != nil {
		_ = tx.Rollback()
		return 0, err
	}
	defer stmt.Close()
	n := 0
	for _, row := range rows {
		if row.ExtID == "" || row.HSCode == "" {
			continue
		}
		res, err := stmt.ExecContext(ctx, row.ExtID, row.HSCode, row.Country, row.Source, row.Period)
		if err != nil {
			_ = tx.Rollback()
			return n, err
		}
		if aff, _ := res.RowsAffected(); aff > 0 {
			n++
		}
	}
	return n, tx.Commit()
}

func (s *CustomsStore) UpsertTradeLines(ctx context.Context, rows []CustomsTradeLine) (int, error) {
	if s == nil || len(rows) == 0 {
		return 0, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	stmt, err := tx.PrepareContext(ctx, `
INSERT OR IGNORE INTO customs_trade_lines(
  reporter_country, flow, period, hs_code, partner_country, origin_country, port_code,
  stat_value, net_mass, record_type, source)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		_ = tx.Rollback()
		return 0, err
	}
	defer stmt.Close()
	n := 0
	for _, row := range rows {
		res, err := stmt.ExecContext(ctx, row.ReporterCountry, row.Flow, row.Period, row.HSCode,
			row.PartnerCountry, row.OriginCountry, row.PortCode, row.StatValue, row.NetMass, row.RecordType, row.Source)
		if err != nil {
			_ = tx.Rollback()
			return n, err
		}
		if aff, _ := res.RowsAffected(); aff > 0 {
			n++
		}
	}
	return n, tx.Commit()
}

func (s *CustomsStore) UpsertShipments(ctx context.Context, rows []CustomsShipmentRow) (int, error) {
	if s == nil || len(rows) == 0 {
		return 0, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	stmt, err := tx.PrepareContext(ctx, `
INSERT OR IGNORE INTO customs_shipments(
  ext_id, ship_date, shipper, consignee, product, hs_code, origin_country, vessel, source, year)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		_ = tx.Rollback()
		return 0, err
	}
	defer stmt.Close()
	n := 0
	for _, row := range rows {
		if row.ExtID == "" {
			continue
		}
		res, err := stmt.ExecContext(ctx, row.ExtID, row.ShipDate, row.Shipper, row.Consignee, row.Product,
			row.HSCode, row.Origin, row.Vessel, row.Source, row.Year)
		if err != nil {
			_ = tx.Rollback()
			return n, err
		}
		if aff, _ := res.RowsAffected(); aff > 0 {
			n++
		}
	}
	return n, tx.Commit()
}

func (s *CustomsStore) UpsertCompanyStats(ctx context.Context, rows []CustomsCompanyStat) (int, error) {
	if s == nil || len(rows) == 0 {
		return 0, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	stmt, err := tx.PrepareContext(ctx, `
INSERT INTO customs_company_stats(ext_id, year, total_shipments, unique_suppliers, matching_shipments, source)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(ext_id, year, source) DO UPDATE SET
  total_shipments=excluded.total_shipments,
  unique_suppliers=excluded.unique_suppliers,
  matching_shipments=excluded.matching_shipments`)
	if err != nil {
		_ = tx.Rollback()
		return 0, err
	}
	defer stmt.Close()
	n := 0
	for _, row := range rows {
		if row.ExtID == "" {
			continue
		}
		if _, err := stmt.ExecContext(ctx, row.ExtID, row.Year, row.TotalShipments, row.UniqueSuppliers, row.MatchingShipments, row.Source); err != nil {
			_ = tx.Rollback()
			return n, err
		}
		n++
	}
	return n, tx.Commit()
}

func (s *CustomsStore) UpsertComtradeFlows(ctx context.Context, rows []comtradeRecord, reporterCode, reporterISO, reporterName, flow, period, hsCode, source string) (int, error) {
	if s == nil || len(rows) == 0 {
		return 0, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	stmt, err := tx.PrepareContext(ctx, `
INSERT OR IGNORE INTO customs_comtrade_flows(
  reporter_code, reporter_iso, reporter_name, partner_iso, partner_name, flow, period, hs_code, primary_value, source)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		_ = tx.Rollback()
		return 0, err
	}
	defer stmt.Close()
	n := 0
	for _, row := range rows {
		if row.PrimaryValue <= 0 {
			continue
		}
		res, err := stmt.ExecContext(ctx, reporterCode, reporterISO, reporterName, row.PartnerISO, row.PartnerDesc,
			flow, period, hsCode, row.PrimaryValue, source)
		if err != nil {
			_ = tx.Rollback()
			return n, err
		}
		if aff, _ := res.RowsAffected(); aff > 0 {
			n++
		}
	}
	return n, tx.Commit()
}

func (s *CustomsStore) RecordRun(ctx context.Context, source string, started time.Time, rows int, note string) error {
	if s == nil {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO customs_ingest_runs(source, started, finished, rows, ms, note)
VALUES (?, ?, ?, ?, ?, ?)`,
		source, started.UTC().Format(time.RFC3339), time.Now().UTC().Format(time.RFC3339), rows,
		time.Since(started).Milliseconds(), note)
	return err
}

func (s *CustomsStore) Counts(ctx context.Context) (map[string]int, error) {
	if s == nil {
		return nil, nil
	}
	tables := []string{
		"customs_companies", "customs_company_products", "customs_trade_lines",
		"customs_shipments", "customs_company_stats", "customs_comtrade_flows",
	}
	out := map[string]int{}
	for _, t := range tables {
		var n int
		if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+t).Scan(&n); err != nil {
			return out, fmt.Errorf("%s: %w", t, err)
		}
		out[t] = n
	}
	return out, nil
}

func customsCompanyExtID(country, role, period, name, postcode string) string {
	h := fnv.New64a()
	_, _ = h.Write([]byte(strings.ToLower(strings.TrimSpace(name) + "|" + strings.TrimSpace(postcode) + "|" + period)))
	cc := strings.ToUpper(strings.TrimSpace(country))
	if cc == "" {
		cc = "XX"
	}
	r := strings.ToLower(strings.TrimSpace(role))
	if r == "" {
		r = "importer"
	}
	return fmt.Sprintf("%s:%s:%s:%s", strings.ToLower(cc), r, period, hex64(h.Sum64()))
}
