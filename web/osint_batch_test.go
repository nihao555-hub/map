package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// 印尼可达的区域/连锁品牌（非 Google/Stripe 等全球巨头；已在本机 curl 验证 200）
var uncommonSMBFixtures = []struct {
	Title    string
	Website  string
	Category string
	Address  string
}{
	{"Excelso Coffee", "https://www.excelso-coffee.com", "Coffee", "Indonesia"},
	{"Kopi Kenangan", "https://kopikenangan.com", "Coffee", "Indonesia"},
	{"Tanamera Coffee", "https://www.tanameracoffee.com", "Coffee", "Indonesia"},
	{"Common Grounds", "https://www.commongrounds.co.id", "Cafe", "Indonesia"},
	{"JCO", "https://www.jco-online.com", "Bakery", "Indonesia"},
	{"BreadTalk ID", "https://www.breadtalk.co.id", "Bakery", "Indonesia"},
	{"Eiger Adventure", "https://www.eigeradventure.com", "Outdoor", "Indonesia"},
	{"Consina", "https://www.consina.com", "Outdoor", "Indonesia"},
	{"Sarinah", "https://www.sarinah.co.id", "Retail", "Jakarta, Indonesia"},
	{"Gramedia", "https://www.gramedia.com", "Bookstore", "Indonesia"},
	{"Sari Ayu", "https://www.sariayu.com", "Cosmetics", "Indonesia"},
	{"Sociolla", "https://www.sociolla.com", "Beauty", "Indonesia"},
	{"Orami", "https://www.orami.co.id", "Parenting retail", "Indonesia"},
	{"ACE Hardware ID", "https://www.acehardware.co.id", "Hardware", "Indonesia"},
	{"Informa", "https://www.informa.co.id", "Furniture", "Indonesia"},
	{"Ruparupa", "https://www.ruparupa.com", "Home retail", "Indonesia"},
	{"Erafone", "https://www.erafone.com", "Electronics", "Indonesia"},
	{"Guardian ID", "https://www.guardianindonesia.co.id", "Pharmacy", "Indonesia"},
	{"Alfamart", "https://www.alfamart.co.id", "Convenience", "Indonesia"},
	{"Indomaret", "https://www.indomaret.co.id", "Convenience", "Indonesia"},
	{"Alfagift", "https://www.alfagift.id", "Grocery", "Indonesia"},
	{"Jiwa Group", "https://www.jiwagroup.com", "F&B", "Indonesia"},
	{"Fore Coffee", "https://www.fore.coffee", "Coffee", "Indonesia"},
	{"Sido Muncul", "https://www.sidomuncul.co.id", "Herbal", "Indonesia"},
	{"Mayora", "https://www.mayoraindah.co.id", "FMCG", "Indonesia"},
}

func TestProbeAllOSINTTools(t *testing.T) {
	st := ProbeOSINTTools()
	b, _ := json.MarshalIndent(st, "", "  ")
	t.Logf("osint tools:\n%s", b)
	if !st.TheHarvester || !st.SpiderFoot {
		t.Fatalf("core tools missing: %+v", st)
	}
}

func reachableWebsite(url string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	client := &http.Client{
		Timeout: 12 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 8 {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml;q=0.9,*/*;q=0.8")
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode > 0 && resp.StatusCode < 500
}

// TestIndonesiaSitesReachable 先保证印尼目标站可访问（凑满 20 家）。
func TestIndonesiaSitesReachable(t *testing.T) {
	var okList []string
	for _, f := range uncommonSMBFixtures {
		if reachableWebsite(f.Website) {
			okList = append(okList, f.Title+" "+f.Website)
			t.Logf("OK %s", f.Website)
		} else {
			t.Logf("FAIL %s", f.Website)
		}
	}
	t.Logf("reachable %d/%d", len(okList), len(uncommonSMBFixtures))
	if len(okList) < 20 {
		t.Fatalf("印尼可达站点不足 20：仅 %d，请检查出口/DNS", len(okList))
	}
	_ = os.WriteFile(filepath.Join(os.TempDir(), "id-reachable.txt"), []byte(strings.Join(okList, "\n")+"\n"), 0o644)
}

func TestBatchIntelUncommonSMBs(t *testing.T) {
	if testing.Short() {
		t.Skip("short")
	}
	st := ProbeOSINTTools()
	if !st.TheHarvester || !st.SpiderFoot {
		t.Skip("core osint missing")
	}

	type row struct {
		Title       string   `json:"title"`
		Website     string   `json:"website"`
		OK          bool     `json:"ok"`
		Status      string   `json:"status"`
		Emails      int      `json:"emails"`
		Phones      int      `json:"phones"`
		Socials     int      `json:"socials"`
		Makers      int      `json:"decision_makers"`
		NamedPeople int      `json:"named_people"`
		NamedNames  []string `json:"named_names,omitempty"`
		Org         int      `json:"org_units"`
		Registry    bool     `json:"registry"`
		RegistrySrc string   `json:"registry_source,omitempty"`
		Sources     int      `json:"sources"`
		HasHunter   bool     `json:"has_hunter"`
		HasWikidata bool     `json:"has_wikidata"`
		HasKatana   bool     `json:"has_katana"`
		HasGLEIF    bool     `json:"has_gleif"`
		HasAHU      bool     `json:"has_ahu"`
		Confidence  string   `json:"confidence"`
		Provider    string   `json:"provider"`
		Err         string   `json:"err,omitempty"`
		Seconds     float64  `json:"seconds"`
	}

	dir := t.TempDir()
	svc := &Service{dataFolder: dir}

	var targets []struct {
		Title, Website, Category, Address string
	}
	for _, f := range uncommonSMBFixtures {
		if reachableWebsite(f.Website) {
			targets = append(targets, f)
		} else {
			t.Logf("skip unreachable: %s %s", f.Title, f.Website)
		}
		if len(targets) >= 20 {
			break
		}
	}
	if len(targets) < 20 {
		t.Fatalf("only %d reachable ID fixtures, need 20", len(targets))
	}
	t.Logf("testing %d Indonesian brands", len(targets))

	var (
		mu   sync.Mutex
		rows []row
		wg   sync.WaitGroup
	)
	sem := make(chan struct{}, 4)
	for i, f := range targets {
		f := f
		i := i
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			place := Place{
				Title:    f.Title,
				Website:  f.Website,
				Category: f.Category,
				Address:  f.Address,
				PlaceID:  fmt.Sprintf("id_batch_%02d", i+1),
			}
			start := time.Now()
			ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
			intel, err := svc.BuildPlaceIntel(ctx, "batch-indonesia", place)
			cancel()
			r := row{Title: f.Title, Website: f.Website, Seconds: time.Since(start).Seconds()}
			if err != nil {
				r.Err = err.Error()
			} else {
				r.OK = true
				r.Status = intel.Status
				r.Emails = len(intel.ExtraEmails)
				r.Phones = len(intel.Phones)
				r.Socials = len(intel.Socials)
				r.Makers = len(intel.DecisionMakers)
				r.NamedPeople = countNamedPeople(intel.DecisionMakers)
				for _, d := range intel.DecisionMakers {
					if looksLikeRealPerson(d) {
						r.NamedNames = append(r.NamedNames, d.Name+"|"+d.Source)
					}
				}
				r.Org = len(intel.OrgStructure)
				r.Registry = intel.CompanyRegistry != nil
				if intel.CompanyRegistry != nil {
					r.RegistrySrc = intel.CompanyRegistry.Source
				}
				r.Sources = len(intel.Sources)
				blob := strings.ToLower(strings.Join(intel.Sources, " ") + " " + intel.Provider)
				r.HasHunter = strings.Contains(blob, "hunter")
				r.HasWikidata = strings.Contains(blob, "wikidata")
				r.HasKatana = strings.Contains(blob, "katana")
				r.HasGLEIF = strings.Contains(blob, "gleif")
				r.HasAHU = strings.Contains(blob, "ahu")
				r.Confidence = intel.Confidence
				r.Provider = intel.Provider
			}
			mu.Lock()
			rows = append(rows, r)
			mu.Unlock()
			t.Logf("[%02d] %s ok=%v conf=%s emails=%d socials=%d makers=%d named=%d org=%d registry=%v sources=%d (%.0fs) provider=%s names=%v err=%s",
				i+1, f.Title, r.OK, r.Confidence, r.Emails, r.Socials, r.Makers, r.NamedPeople, r.Org, r.Registry, r.Sources, r.Seconds, r.Provider, r.NamedNames, r.Err)
		}()
	}
	wg.Wait()

	outPath := filepath.Join(os.TempDir(), "intel-batch-indonesia.json")
	b, _ := json.MarshalIndent(rows, "", "  ")
	_ = os.WriteFile(outPath, b, 0o644)
	t.Logf("wrote %s", outPath)

	okN, emailN, makerN, namedN, socialN, regN := 0, 0, 0, 0, 0, 0
	wikiN, katanaN, gleifN, hunterN, ahuN := 0, 0, 0, 0, 0
	var secs []float64
	conf := map[string]int{}
	for _, r := range rows {
		if r.OK {
			okN++
		}
		if r.Emails > 0 {
			emailN++
		}
		if r.Makers > 0 {
			makerN++
		}
		if r.NamedPeople > 0 {
			namedN++
		}
		if r.Socials > 0 {
			socialN++
		}
		if r.Registry {
			regN++
		}
		if r.HasWikidata {
			wikiN++
		}
		if r.HasKatana {
			katanaN++
		}
		if r.HasGLEIF {
			gleifN++
		}
		if r.HasHunter {
			hunterN++
		}
		if r.HasAHU {
			ahuN++
		}
		secs = append(secs, r.Seconds)
		conf[r.Confidence]++
	}
	avg, p50, p95 := durationStats(secs)
	summary := fmt.Sprintf(
		"n=%d ok=%d email>0=%d makers>0=%d named_people>0=%d socials>0=%d registry=%d wiki=%d katana=%d gleif=%d hunter=%d ahu=%d conf=%v avg=%.1fs p50=%.1fs p95=%.1fs",
		len(rows), okN, emailN, makerN, namedN, socialN, regN, wikiN, katanaN, gleifN, hunterN, ahuN, conf, avg, p50, p95,
	)
	t.Log(summary)
	_ = os.WriteFile(filepath.Join(os.TempDir(), "intel-batch-indonesia-summary.txt"), []byte(summary+"\n"), 0o644)

	if okN < 15 {
		t.Fatalf("too many failures: %s", summary)
	}
	if emailN+socialN == 0 {
		t.Fatalf("no emails/socials across batch: %s", summary)
	}
}

func durationStats(secs []float64) (avg, p50, p95 float64) {
	if len(secs) == 0 {
		return 0, 0, 0
	}
	cp := append([]float64{}, secs...)
	sort.Float64s(cp)
	sum := 0.0
	for _, s := range cp {
		sum += s
	}
	avg = sum / float64(len(cp))
	p50 = cp[len(cp)/2]
	p95 = cp[int(float64(len(cp)-1)*0.95)]
	return avg, p50, p95
}

func TestBatchIntelSkipsGiants(t *testing.T) {
	for _, f := range uncommonSMBFixtures {
		u := strings.ToLower(f.Website)
		for _, bad := range []string{"google.com", "stripe.com", "facebook.com", "amazon.com", "microsoft.com", "apple.com"} {
			if strings.Contains(u, bad) {
				t.Fatalf("fixture includes giant domain %s", f.Website)
			}
		}
	}
}
