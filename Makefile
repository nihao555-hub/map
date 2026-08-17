APP_NAME := google_maps_scraper
VERSION := 1.17.3

default: help

# generate help info from comments: thanks to https://marmelab.com/blog/2016/02/29/auto-documented-makefile.html
help: ## help information about make commands
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'

vet: ## runs go vet
	go vet ./...

format: ## runs go fmt
	gofmt -s -w .

test-agent-skill: ## tests the bundled AI agent workflow
	node --test skills/google-maps-scraper/scripts/select-proxy-sponsors.test.mjs
	node --test skills/google-maps-scraper/scripts/status-local.test.mjs
	bash skills/google-maps-scraper/scripts/helpers_test.sh

test: test-agent-skill ## runs the unit tests
	go test -v -race -timeout 5m ./...

test-cover: ## outputs the coverage statistics
	go test -v -race -timeout 5m ./... -coverprofile coverage.out
	go tool cover -func coverage.out
	rm coverage.out

test-cover-report: ## an html report of the coverage statistics
	go test -v ./... -covermode=count -coverpkg=./... -coverprofile coverage.out
	go tool cover -html coverage.out -o coverage.html
	open coverage.html

vuln: ## runs vulnerability checks
	go tool govulncheck -C . -show verbose -format text -scan symbol ./...

lint: ## runs the linter
	go tool golangci-lint -v run ./...

cross-compile: ## cross compiles the application
	GOOS=linux GOARCH=amd64 go build -o bin/$(APP_NAME)-${VERSION}-linux-amd64
	GOOS=darwin GOARCH=amd64 go build -o bin/$(APP_NAME)-${VERSION}-darwin-amd64
	GOOS=windows GOARCH=amd64 go build -o bin/$(APP_NAME)-${VERSION}-windows-amd64.exe

build: ## builds the application (default: playwright)
	go build -o bin/$(APP_NAME) .

docker: ## builds docker image with playwright (default)
	docker build -t $(APP_NAME):$(VERSION) .

# --- SaaS targets ---

build-saas: ## builds the SaaS binary (API server, worker, admin)
	go build -o bin/gmapssaas ./cmd/gmapssaas/

docker-saas: ## builds docker image for SaaS
	docker build -f Dockerfile.saas -t gmapssaas:$(VERSION) .

SAAS_IMAGE ?= ghcr.io/gosom/google-maps-scraper-saas:latest

saas-docker-push: docker-saas ## builds and pushes the SaaS docker image
	docker tag gmapssaas:$(VERSION) $(SAAS_IMAGE)
	docker push $(SAAS_IMAGE)

provision: ## run the provisioning wizard via Docker (state persisted to ~/.gmapssaas)
	docker run --rm -it \
	  -v "$(HOME)/.gmapssaas:/root/.gmapssaas" \
	  -v /var/run/docker.sock:/var/run/docker.sock \
	  -v "$(HOME)/.ssh:/root/.ssh:ro" \
	  gmapssaas:$(VERSION) provision

saas-dev: ## start SaaS development environment (postgres + migrations + admin user + hot reload)
	@docker compose -f docker-compose.saas.yaml up -d postgres
	@echo "Waiting for postgres..."
	@until docker compose -f docker-compose.saas.yaml exec -T postgres pg_isready -U postgres > /dev/null 2>&1; do sleep 1; done
	@echo "Running migrations..."
	@sql-migrate up -config=migrations/dbconfig.yml
	@echo "Creating admin user..."
	@go run ./cmd/gmapssaas admin create-user -u admin -p '1234#abcd'
	@echo "Starting server with hot reload on :8080..."
	@air

saas-dev-stop: ## stop SaaS development environment
	@docker compose -f docker-compose.saas.yaml down

saas-dev-reset: ## reset SaaS development environment (drops all data)
	@docker compose -f docker-compose.saas.yaml down -v

saas-run-server: ## run the SaaS API server locally
	@go run ./cmd/gmapssaas serve

saas-run-worker: ## run the SaaS worker locally
	@go run ./cmd/gmapssaas worker

saas-provision: ## run infrastructure provisioning wizard
	@go run ./cmd/gmapssaas provision

saas-migrate-up: ## run all pending SaaS database migrations
	@sql-migrate up -config=migrations/dbconfig.yml

saas-migrate-down: ## rollback the last SaaS migration
	@sql-migrate down -config=migrations/dbconfig.yml -limit=1

saas-migrate-status: ## show SaaS migration status
	@sql-migrate status -config=migrations/dbconfig.yml

saas-migrate-new: ## create a new SaaS migration (usage: make saas-migrate-new name=xxx)
	@if [ -z "$(name)" ]; then echo "Error: name required. Usage: make saas-migrate-new name=xxx"; exit 1; fi
	@sql-migrate new -config=migrations/dbconfig.yml $(name)

saas-gen: ## regenerate swagger docs for the SaaS API
	@swag init -g api/doc.go -o api/docs

gen: saas-gen ## generate swagger docs

saas-psql: ## connect to SaaS development database
	PGPASSWORD=postgres psql -h localhost -p 5432 -U postgres gmapssaas

MERCHANT_DB ?= store/merchants.db

ingest-merchants: ## dump GLEIF + OSM all-shop cities into local SQLite
	go run ./cmd/ingest-merchants -db $(MERCHANT_DB) -gleif-zip /tmp/merchant-ingest/gleif-lei2.csv.zip

ingest-sea: ## dump extra Southeast Asia OSM cities + Wikidata companies
	go run ./cmd/ingest-merchants -db $(MERCHANT_DB) -sea -osm-limit 2000

ingest-lei-socials: ## attach Wikidata website/socials onto GLEIF rows by LEI
	go run ./cmd/ingest-merchants -db $(MERCHANT_DB) -lei-socials

ingest-public-socials: ## ROR dump + Wikidata P856 + same-name copy onto GLEIF
	go run ./cmd/ingest-merchants -db $(MERCHANT_DB) -public-socials -attach-only

ingest-short-video: ## 抖音/TikTok 企业号主页（TikTok-Api / f2 sidecar + 公开索引）
	go run ./cmd/harvest-dorks -db $(MERCHANT_DB) -short-video -workers 4

ingest-short-video-fast: ## 两小时：Wikidata 全量号 + 东南亚→中东→欧美
	go run ./cmd/harvest-dorks -db $(MERCHANT_DB) -short-video -fast -deadline 110m -workers 8

ingest-short-video-seed: ## 只灌 Wikidata 已标注的 TikTok/抖音号
	go run ./cmd/harvest-dorks -db $(MERCHANT_DB) -short-video -seed-only

ingest-sea-tiktok: ## 东南亚 TikTok 企业号：TikTok-Api / f2 sidecar 按词搜
	go run ./cmd/harvest-dorks -db $(MERCHANT_DB) -short-video -sidecar -regions sea -deadline 90m

ingest-social-search: ## 去 TikTok 搜索页/话题/相关账号扫企业号（常驻浏览器）
	go run ./cmd/harvest-dorks -db $(MERCHANT_DB) -short-video -social-search -regions sea,me -deadline 90m -workers 2

ingest-wayback-tiktok: ## Internet Archive CDX 里能枚举的 tiktok.com/@ 与抖音主页
	go run ./cmd/harvest-dorks -db $(MERCHANT_DB) -short-video -wayback -deadline 90m

ingest-short-video-public: ## 公开源能枚举的 TikTok/抖音主页（不是平台全库）
	go run ./cmd/harvest-dorks -db $(MERCHANT_DB) -short-video -public-all -deadline 180m

ingest-world-companies: ## Wikidata 全球带官网的企业（QLever，不灌 Facebook 全库）
	go run ./cmd/ingest-merchants -db $(MERCHANT_DB) -world-companies

ingest-public-max: ## Wikidata global socials, OSM contact:*, GLEIF parent inherit, ROR
	go run ./cmd/ingest-merchants -db $(MERCHANT_DB) -public-max \
		-rr-zip /tmp/merchant-ingest/gleif-rr.csv.zip \
		-ror-zip /tmp/merchant-ingest/ror-data.zip

ingest-rr-only: ## same-country copy + GLEIF Level 2 parent social inherit
	go run ./cmd/ingest-merchants -db $(MERCHANT_DB) -rr-only

ingest-more-socials: ## split OSM boxes, Wikidata parent, SEC websites, scrape official sites
	go run ./cmd/ingest-merchants -db $(MERCHANT_DB) -more-socials \
		-rr-zip /tmp/merchant-ingest/gleif-rr.csv.zip \
		-website-workers 16

ingest-sherlock: ## Sherlock site list on official handles + distinctive unique names
	go run ./cmd/ingest-merchants -db $(MERCHANT_DB) -sherlock \
		-sherlock-workers 12

ingest-attach-socials: ## 全库缺社媒：官网刮取 + OSM contact + 店名检索 FB/IG/LI + Sherlock 姐妹页
	go run ./cmd/ingest-merchants -db $(MERCHANT_DB) -attach-socials \
		-attach-workers 10

enrich-merchants: ## fetch OSM official sites and probe missing social homepages
	go run ./cmd/enrich-merchants -db $(MERCHANT_DB) -workers 12

upload-merchants: ## 把 store/merchants.db 快照上传到阿里云 OSS / S3
	go run ./cmd/sync-merchant-db -db $(MERCHANT_DB) -upload

restore-merchants: ## 从 OSS / S3 拉回商户库
	go run ./cmd/sync-merchant-db -db $(MERCHANT_DB) -download

clean: ## clean build artifacts
	@rm -rf bin/ tmp/
