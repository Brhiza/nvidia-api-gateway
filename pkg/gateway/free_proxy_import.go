package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"nvidia-api-gateway/pkg/db"
	"nvidia-api-gateway/pkg/models"
)

const (
	defaultFreeProxyImportGroup       = models.DefaultProxyImportGroup
	defaultFreeProxyImportLimit       = models.DefaultProxyImportLimit
	maxFreeProxyImportLimit           = 2000
	defaultFreeProxyImportConcurrency = models.DefaultProxyImportConcurrency
	maxFreeProxyImportConcurrency     = 256
	defaultFreeProxyImportTimeout     = time.Duration(models.DefaultProxyImportTimeoutSeconds) * time.Second
	defaultFreeProxyImportRetryCount  = models.DefaultProxyImportRetryCount
	maxFreeProxyImportRetryCount      = 3
)

type freeProxyImportMode string

const (
	freeProxyImportModeAll    freeProxyImportMode = "all"
	freeProxyImportModeHTTP   freeProxyImportMode = "http"
	freeProxyImportModeSOCKS5 freeProxyImportMode = "socks5"
)

type freeProxyImportOptions struct {
	Mode                           freeProxyImportMode
	Group                          string
	Limit                          int
	Concurrency                    int
	Timeout                        time.Duration
	RetryCount                     int
	CleanupEnabled                 bool
	CleanupMaxLatencyMs            int
	CleanupDeleteFailedAutoProxies bool
}

type freeProxyImportSummary struct {
	Mode                string `json:"mode"`
	Group               string `json:"group"`
	CandidateCount      int    `json:"candidateCount"`
	TestedCount         int    `json:"testedCount"`
	AvailableCount      int    `json:"availableCount"`
	FailedCount         int    `json:"failedCount"`
	ImportedCount       int    `json:"importedCount"`
	UpdatedCount        int    `json:"updatedCount"`
	MatchedManualCount  int    `json:"matchedManualCount"`
	SourceErrorCount    int    `json:"sourceErrorCount"`
	CleanedSlowCount    int    `json:"cleanedSlowCount"`
	CleanedFailedCount  int    `json:"cleanedFailedCount"`
	CleanupDeletedCount int    `json:"cleanupDeletedCount"`
	UnboundKeyCount     int    `json:"unboundKeyCount"`
}

type freeProxyCandidate struct {
	Type    string
	Host    string
	Port    int
	Country string
}

type testedFreeProxy struct {
	Candidate freeProxyCandidate
	Record    models.ProxyTestRecord
	Success   bool
}

type freeProxySourceJob struct {
	URL       string
	Kind      string
	ProxyType string
}

type freeProxyFetchResult struct {
	Candidates []freeProxyCandidate
	Err        error
}

type freeProxyTestResult struct {
	Item testedFreeProxy
}

type freeProxyBatchGeoResponse struct {
	Status      string `json:"status"`
	Query       string `json:"query"`
	Country     string `json:"country"`
	CountryCode string `json:"countryCode"`
}

type freeProxyImportHooks struct {
	OnFetchStart      func(total int)
	OnFetchProgress   func(completed, total int)
	OnCandidatesReady func(candidateCount, sourceErrors int)
	OnTestProgress    func(tested, total, available, failed int)
	OnPersistProgress func(done, total, imported, updated, matchedManual int)
}

var freeProxyHTTPSourcesTXT = []string{}

var freeProxyHTTPSourcesJSON = []string{
	models.DefaultGoProxyPublicAPIURL,
}

var freeProxySOCKS5SourcesTXT = []string{}

var freeProxySOCKS5SourcesJSON = []string{
	models.DefaultGoProxyPublicAPIURL,
}

var freeProxySourceHTTPClient = &http.Client{Timeout: 15 * time.Second}

func normalizeFreeProxyImportOptions(req importFreeProxiesRequest) (freeProxyImportOptions, error) {
	mode := freeProxyImportMode(strings.ToLower(strings.TrimSpace(req.Mode)))
	if mode == "" {
		mode = freeProxyImportModeAll
	}
	switch mode {
	case freeProxyImportModeAll, freeProxyImportModeHTTP, freeProxyImportModeSOCKS5:
	default:
		return freeProxyImportOptions{}, fmt.Errorf("不支持的抓取模式: %s", req.Mode)
	}

	group := strings.TrimSpace(req.Group)
	if group == "" {
		group = defaultFreeProxyImportGroup
	}

	limit := req.Limit
	if limit <= 0 {
		limit = defaultFreeProxyImportLimit
	}
	if limit > maxFreeProxyImportLimit {
		limit = maxFreeProxyImportLimit
	}

	concurrency := req.Concurrency
	if concurrency <= 0 {
		concurrency = defaultFreeProxyImportConcurrency
	}
	if concurrency > maxFreeProxyImportConcurrency {
		concurrency = maxFreeProxyImportConcurrency
	}

	timeoutSeconds := req.TimeoutSeconds
	if timeoutSeconds <= 0 {
		timeoutSeconds = int(defaultFreeProxyImportTimeout / time.Second)
	}
	if timeoutSeconds > 30 {
		timeoutSeconds = 30
	}

	retryCount := req.RetryCount
	if retryCount < 0 {
		retryCount = defaultFreeProxyImportRetryCount
	}
	if retryCount > maxFreeProxyImportRetryCount {
		retryCount = maxFreeProxyImportRetryCount
	}

	cleanupMaxLatencyMs := req.CleanupMaxLatencyMs
	if cleanupMaxLatencyMs <= 0 {
		cleanupMaxLatencyMs = models.DefaultProxyImportCleanupLatency
	}
	if cleanupMaxLatencyMs > 30000 {
		cleanupMaxLatencyMs = 30000
	}

	return freeProxyImportOptions{
		Mode:                           mode,
		Group:                          group,
		Limit:                          limit,
		Concurrency:                    concurrency,
		Timeout:                        time.Duration(timeoutSeconds) * time.Second,
		RetryCount:                     retryCount,
		CleanupEnabled:                 req.CleanupEnabled,
		CleanupMaxLatencyMs:            cleanupMaxLatencyMs,
		CleanupDeleteFailedAutoProxies: req.CleanupDeleteFailedAutoProxies,
	}, nil
}

func importFreeUpstreamProxies(ctx context.Context, opts freeProxyImportOptions) (freeProxyImportSummary, error) {
	return importFreeUpstreamProxiesWithHooks(ctx, opts, freeProxyImportHooks{})
}

func importFreeUpstreamProxiesWithHooks(ctx context.Context, opts freeProxyImportOptions, hooks freeProxyImportHooks) (freeProxyImportSummary, error) {
	summary := freeProxyImportSummary{
		Mode:  string(opts.Mode),
		Group: opts.Group,
	}
	candidates, sourceErrors := fetchFreeProxyCandidates(opts, hooks)
	summary.SourceErrorCount = sourceErrors
	summary.CandidateCount = len(candidates)
	if hooks.OnCandidatesReady != nil {
		hooks.OnCandidatesReady(summary.CandidateCount, summary.SourceErrorCount)
	}
	if len(candidates) == 0 {
		return summary, nil
	}

	testedItems := testFreeProxyCandidates(ctx, candidates, opts, &summary, hooks)
	if len(testedItems) == 0 {
		return summary, nil
	}

	now := time.Now()
	if err := db.UpdateStore(func(store *db.Store) error {
		for index, item := range testedItems {
			upsertImportedFreeProxy(store, item, opts.Group, now, &summary)
			if hooks.OnPersistProgress != nil {
				hooks.OnPersistProgress(index+1, len(testedItems), summary.ImportedCount, summary.UpdatedCount, summary.MatchedManualCount)
			}
		}
		cleanupImportedAutoProxies(store, opts, now, &summary)
		return nil
	}); err != nil {
		return summary, err
	}
	return summary, nil
}

func fetchFreeProxyCandidates(opts freeProxyImportOptions, hooks freeProxyImportHooks) ([]freeProxyCandidate, int) {
	jobs := buildFreeProxySourceJobs(opts.Mode)
	if hooks.OnFetchStart != nil {
		hooks.OnFetchStart(len(jobs))
	}
	results := make(chan freeProxyFetchResult, len(jobs))
	var wg sync.WaitGroup
	for _, job := range jobs {
		job := job
		wg.Add(1)
		go func() {
			defer wg.Done()
			items, err := fetchFreeProxySource(job, opts.RetryCount)
			results <- freeProxyFetchResult{Candidates: items, Err: err}
		}()
	}
	go func() {
		wg.Wait()
		close(results)
	}()

	index := make(map[string]freeProxyCandidate)
	sourceErrors := 0
	completedSources := 0
	for result := range results {
		completedSources++
		if hooks.OnFetchProgress != nil {
			hooks.OnFetchProgress(completedSources, len(jobs))
		}
		if result.Err != nil {
			sourceErrors++
			continue
		}
		for _, candidate := range result.Candidates {
			key := freeProxyCandidateKey(candidate)
			if existing, ok := index[key]; ok {
				if existing.Country == "" && candidate.Country != "" {
					index[key] = candidate
				}
				continue
			}
			index[key] = candidate
		}
	}

	candidates := make([]freeProxyCandidate, 0, len(index))
	for _, candidate := range index {
		candidates = append(candidates, candidate)
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Type != candidates[j].Type {
			return candidates[i].Type < candidates[j].Type
		}
		leftHost := strings.ToLower(candidates[i].Host)
		rightHost := strings.ToLower(candidates[j].Host)
		if leftHost != rightHost {
			return leftHost < rightHost
		}
		return candidates[i].Port < candidates[j].Port
	})
	if opts.Limit > 0 && len(candidates) > opts.Limit {
		candidates = candidates[:opts.Limit]
	}
	return candidates, sourceErrors
}

func loadExternalProxySources() models.ExternalProxySources {
	store, err := db.ReadStore()
	if err != nil || store == nil {
		return models.DefaultExternalProxySources()
	}
	return models.NormalizeExternalProxySources(store.ExternalProxySources)
}

func appendUniqueProxySourceJobs(jobs []freeProxySourceJob, items []string, kind string, proxyType string) []freeProxySourceJob {
	seen := make(map[string]struct{}, len(jobs)+len(items))
	for _, item := range jobs {
		key := strings.TrimSpace(item.ProxyType) + "|" + strings.TrimSpace(item.Kind) + "|" + strings.TrimSpace(item.URL)
		if key != "||" {
			seen[key] = struct{}{}
		}
	}
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		key := strings.TrimSpace(proxyType) + "|" + strings.TrimSpace(kind) + "|" + trimmed
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		jobs = append(jobs, freeProxySourceJob{URL: trimmed, Kind: kind, ProxyType: proxyType})
	}
	return jobs
}

func buildFreeProxySourceJobs(mode freeProxyImportMode) []freeProxySourceJob {
	external := loadExternalProxySources()
	jobs := make([]freeProxySourceJob, 0, len(freeProxyHTTPSourcesTXT)+len(freeProxyHTTPSourcesJSON)+len(freeProxyHTTPSourcesHTML)+len(freeProxySOCKS5SourcesTXT)+len(freeProxySOCKS5SourcesJSON)+len(freeProxySOCKS5SourcesHTML)+len(external.HTTPTXT)+len(external.HTTPJSON)+len(external.HTTPHTML)+len(external.SOCKS5TXT)+len(external.SOCKS5JSON)+len(external.SOCKS5HTML))
	if mode == freeProxyImportModeAll || mode == freeProxyImportModeHTTP {
		jobs = appendUniqueProxySourceJobs(jobs, freeProxyHTTPSourcesTXT, "txt", "http")
		jobs = appendUniqueProxySourceJobs(jobs, freeProxyHTTPSourcesJSON, "json", "http")
		jobs = appendUniqueProxySourceJobs(jobs, freeProxyHTTPSourcesHTML, "html", "http")
		jobs = appendUniqueProxySourceJobs(jobs, external.HTTPTXT, "txt", "http")
		jobs = appendUniqueProxySourceJobs(jobs, external.HTTPJSON, "json", "http")
		jobs = appendUniqueProxySourceJobs(jobs, external.HTTPHTML, "html", "http")
	}
	if mode == freeProxyImportModeAll || mode == freeProxyImportModeSOCKS5 {
		jobs = appendUniqueProxySourceJobs(jobs, freeProxySOCKS5SourcesTXT, "txt", "socks5h")
		jobs = appendUniqueProxySourceJobs(jobs, freeProxySOCKS5SourcesJSON, "json", "socks5h")
		jobs = appendUniqueProxySourceJobs(jobs, freeProxySOCKS5SourcesHTML, "html", "socks5h")
		jobs = appendUniqueProxySourceJobs(jobs, external.SOCKS5TXT, "txt", "socks5h")
		jobs = appendUniqueProxySourceJobs(jobs, external.SOCKS5JSON, "json", "socks5h")
		jobs = appendUniqueProxySourceJobs(jobs, external.SOCKS5HTML, "html", "socks5h")
	}
	return jobs
}

func fetchFreeProxySource(job freeProxySourceJob, retryCount int) ([]freeProxyCandidate, error) {
	attempts := retryCount + 1
	if attempts <= 0 {
		attempts = 1
	}
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		items, err := fetchFreeProxySourceOnce(job)
		if err == nil {
			return items, nil
		}
		lastErr = err
		if attempt < attempts {
			time.Sleep(time.Duration(attempt) * 200 * time.Millisecond)
		}
	}
	return nil, lastErr
}

func fetchFreeProxySourceOnce(job freeProxySourceJob) ([]freeProxyCandidate, error) {
	req, err := http.NewRequest(http.MethodGet, job.URL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := freeProxySourceHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("source http %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	switch job.Kind {
	case "txt":
		return parseFreeProxyTXTBody(string(body), job.ProxyType), nil
	case "json":
		return parseFreeProxyJSONBody(body, job.ProxyType)
	case "html":
		return parseFreeProxyHTMLBody(string(body), job.ProxyType), nil
	default:
		return nil, fmt.Errorf("unknown source kind: %s", job.Kind)
	}
}

func parseFreeProxyTXTBody(body string, proxyType string) []freeProxyCandidate {
	items := make([]freeProxyCandidate, 0)
	for _, line := range strings.Split(body, "\n") {
		if candidate, ok := buildFreeProxyCandidate(proxyType, line, ""); ok {
			items = append(items, candidate)
		}
	}
	return items
}

func parseFreeProxyJSONBody(body []byte, proxyType string) ([]freeProxyCandidate, error) {
	var payload any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	return collectFreeProxyCandidates(payload, proxyType), nil
}

func collectFreeProxyCandidates(payload any, proxyType string) []freeProxyCandidate {
	switch value := payload.(type) {
	case []any:
		items := make([]freeProxyCandidate, 0)
		for _, item := range value {
			items = append(items, collectFreeProxyCandidates(item, proxyType)...)
		}
		return items
	case map[string]any:
		if candidate, ok := freeProxyCandidateFromMap(value, proxyType); ok {
			return []freeProxyCandidate{candidate}
		}
		for _, key := range []string{"data", "proxies", "items", "result", "list"} {
			if nested, ok := value[key]; ok {
				items := collectFreeProxyCandidates(nested, proxyType)
				if len(items) > 0 {
					return items
				}
			}
		}
	}
	return nil
}

func freeProxyCandidateFromMap(value map[string]any, proxyType string) (freeProxyCandidate, bool) {
	country := firstJSONString(value, "country", "country_code", "countryCode", "exit_location", "exitLocation")
	candidateType, hasExplicitType := inferFreeProxyCandidateType(value, proxyType)
	if hasExplicitType && !freeProxyTypeMatches(proxyType, candidateType) {
		return freeProxyCandidate{}, false
	}
	if proxy := firstJSONString(value, "proxy", "ip_port", "ipPort", "address", "addr", "url"); proxy != "" {
		return buildFreeProxyCandidate(candidateType, proxy, country)
	}
	ip := firstJSONString(value, "ip", "host", "hostname")
	port := firstJSONPort(value, "port")
	if ip != "" && port > 0 {
		return freeProxyCandidate{Type: candidateType, Host: ip, Port: port, Country: strings.ToUpper(strings.TrimSpace(country))}, true
	}
	return freeProxyCandidate{}, false
}

func inferFreeProxyCandidateType(value map[string]any, fallback string) (string, bool) {
	if protocol := normalizeFreeProxyCandidateType(firstJSONString(value, "protocol", "type", "scheme")); protocol != "" {
		return protocol, true
	}
	for _, key := range []string{"proxy", "ip_port", "ipPort", "address", "addr", "url"} {
		raw := firstJSONString(value, key)
		if !strings.Contains(raw, "://") {
			continue
		}
		parsed, err := url.Parse(raw)
		if err != nil {
			continue
		}
		if protocol := normalizeFreeProxyCandidateType(parsed.Scheme); protocol != "" {
			return protocol, true
		}
	}
	return normalizeFreeProxyCandidateType(fallback), false
}

func normalizeFreeProxyCandidateType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "http", "https":
		return "http"
	case "socks", "socks5", "socks5h":
		return "socks5h"
	default:
		return ""
	}
}

func freeProxyTypeMatches(expected, actual string) bool {
	expected = normalizeFreeProxyCandidateType(expected)
	actual = normalizeFreeProxyCandidateType(actual)
	return expected == "" || actual == "" || expected == actual
}

func firstJSONString(value map[string]any, keys ...string) string {
	for _, key := range keys {
		raw, ok := value[key]
		if !ok || raw == nil {
			continue
		}
		switch item := raw.(type) {
		case string:
			trimmed := strings.TrimSpace(item)
			if trimmed != "" {
				return trimmed
			}
		case json.Number:
			return item.String()
		case float64:
			if item > 0 {
				return strconv.FormatInt(int64(item), 10)
			}
		case int:
			if item > 0 {
				return strconv.Itoa(item)
			}
		}
	}
	return ""
}

func firstJSONPort(value map[string]any, keys ...string) int {
	for _, key := range keys {
		raw, ok := value[key]
		if !ok || raw == nil {
			continue
		}
		switch item := raw.(type) {
		case float64:
			return int(item)
		case int:
			return item
		case int64:
			return int(item)
		case string:
			port, err := strconv.Atoi(strings.TrimSpace(item))
			if err == nil {
				return port
			}
		case json.Number:
			port, err := item.Int64()
			if err == nil {
				return int(port)
			}
		}
	}
	return 0
}

func buildFreeProxyCandidate(proxyType, endpoint, country string) (freeProxyCandidate, bool) {
	host, port, ok := parseFreeProxyAddress(endpoint)
	if !ok {
		return freeProxyCandidate{}, false
	}
	return freeProxyCandidate{
		Type:    strings.ToLower(strings.TrimSpace(proxyType)),
		Host:    host,
		Port:    port,
		Country: strings.ToUpper(strings.TrimSpace(country)),
	}, true
}

func parseFreeProxyAddress(raw string) (string, int, bool) {
	value := strings.TrimSpace(strings.Trim(raw, "`| 	\r\n"))
	if value == "" || strings.HasPrefix(value, "#") || len(value) > 128 {
		return "", 0, false
	}

	if strings.Contains(value, "://") {
		parsed, err := url.Parse(value)
		if err != nil {
			return "", 0, false
		}
		host := strings.TrimSpace(parsed.Hostname())
		port, err := strconv.Atoi(parsed.Port())
		if err != nil || host == "" || port <= 0 || port > 65535 {
			return "", 0, false
		}
		return host, port, true
	}

	if parsed, err := url.Parse("//" + value); err == nil && parsed.Host != "" && strings.Contains(parsed.Host, ":") {
		host := strings.TrimSpace(parsed.Hostname())
		port, err := strconv.Atoi(parsed.Port())
		if err == nil && host != "" && port > 0 && port <= 65535 {
			return host, port, true
		}
	}

	host, portText, err := net.SplitHostPort(value)
	if err != nil {
		idx := strings.LastIndex(value, ":")
		if idx <= 0 || idx >= len(value)-1 {
			return "", 0, false
		}
		host = strings.Trim(value[:idx], "[]")
		portText = value[idx+1:]
	}
	port, err := strconv.Atoi(strings.TrimSpace(portText))
	host = strings.TrimSpace(strings.Trim(host, "[]"))
	if err != nil || host == "" || port <= 0 || port > 65535 {
		return "", 0, false
	}
	return host, port, true
}

func testFreeProxyCandidates(ctx context.Context, candidates []freeProxyCandidate, opts freeProxyImportOptions, summary *freeProxyImportSummary, hooks freeProxyImportHooks) []testedFreeProxy {
	workers := opts.Concurrency
	if workers <= 0 {
		workers = defaultFreeProxyImportConcurrency
	}
	input := make(chan freeProxyCandidate)
	output := make(chan freeProxyTestResult, len(candidates))
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for candidate := range input {
				record, success := testFreeProxyCandidate(ctx, candidate, opts.Timeout, opts.RetryCount)
				output <- freeProxyTestResult{Item: testedFreeProxy{Candidate: candidate, Record: record, Success: success}}
			}
		}()
	}
	go func() {
		for _, candidate := range candidates {
			input <- candidate
		}
		close(input)
		wg.Wait()
		close(output)
	}()

	testedItems := make([]testedFreeProxy, 0, len(candidates))
	for result := range output {
		summary.TestedCount++
		if result.Item.Success {
			summary.AvailableCount++
		} else {
			summary.FailedCount++
		}
		testedItems = append(testedItems, result.Item)
		if hooks.OnTestProgress != nil {
			hooks.OnTestProgress(summary.TestedCount, len(candidates), summary.AvailableCount, summary.FailedCount)
		}
	}
	enrichFreeProxyCountries(testedItems)
	return testedItems
}

func testFreeProxyCandidate(ctx context.Context, candidate freeProxyCandidate, timeout time.Duration, retryCount int) (models.ProxyTestRecord, bool) {
	proxyCfg := models.NormalizeUpstreamProxy(models.UpstreamProxy{
		Name:   buildAutoProxyName(candidate),
		Group:  defaultFreeProxyImportGroup,
		Source: models.ProxySourceAuto,
		Type:   candidate.Type,
		Status: models.ProxyStatusEnabled,
		Host:   candidate.Host,
		Port:   candidate.Port,
	})
	attempts := retryCount + 1
	if attempts <= 0 {
		attempts = 1
	}
	var lastRecord models.ProxyTestRecord
	for attempt := 1; attempt <= attempts; attempt++ {
		testCtx := ctx
		var cancel context.CancelFunc
		if timeout > 0 {
			testCtx, cancel = context.WithTimeout(ctx, timeout)
		}
		result := testUpstreamProxyConnectivity(testCtx, proxyCfg)
		if cancel != nil {
			cancel()
		}
		record := buildProxyTestRecordFromResult(result)
		lastRecord = record
		if record.Success {
			return record, true
		}
		if attempt < attempts {
			time.Sleep(time.Duration(attempt) * 150 * time.Millisecond)
		}
	}
	return lastRecord, lastRecord.Success
}

func upsertImportedFreeProxy(store *db.Store, item testedFreeProxy, defaultGroup string, now time.Time, summary *freeProxyImportSummary) {
	for i := range store.Proxies {
		if !sameProxyEndpoint(store.Proxies[i], item.Candidate) {
			continue
		}
		record := item.Record
		store.Proxies[i].LastTest = &record
		store.Proxies[i].TestHistory = mergeProxyTestHistory(store.Proxies[i].TestHistory, record, maxProxyTestHistory)
		store.Proxies[i].UpdatedAt = now
		store.Proxies[i] = models.NormalizeUpstreamProxy(store.Proxies[i])
		if store.Proxies[i].Source == models.ProxySourceAuto {
			if item.Success {
				store.Proxies[i].Status = models.ProxyStatusEnabled
			} else {
				store.Proxies[i].Status = models.ProxyStatusDisabled
			}
			if strings.TrimSpace(item.Candidate.Country) != "" {
				store.Proxies[i].Country = item.Candidate.Country
			}
			if strings.TrimSpace(store.Proxies[i].Group) == "" {
				store.Proxies[i].Group = defaultGroup
			}
			store.Proxies[i].Name = buildAutoProxyName(item.Candidate)
			summary.UpdatedCount++
		} else {
			summary.MatchedManualCount++
		}
		return
	}

	record := item.Record
	proxyStatus := models.ProxyStatusDisabled
	if item.Success {
		proxyStatus = models.ProxyStatusEnabled
	}
	proxyCfg := models.NormalizeUpstreamProxy(models.UpstreamProxy{
		ID:          store.NextProxyID,
		Name:        buildAutoProxyName(item.Candidate),
		Group:       defaultGroup,
		Source:      models.ProxySourceAuto,
		Type:        item.Candidate.Type,
		Status:      proxyStatus,
		Country:     item.Candidate.Country,
		Host:        item.Candidate.Host,
		Port:        item.Candidate.Port,
		LastTest:    &record,
		TestHistory: []models.ProxyTestRecord{record},
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	store.NextProxyID++
	store.Proxies = append(store.Proxies, proxyCfg)
	summary.ImportedCount++
}

func sameProxyEndpoint(proxyCfg models.UpstreamProxy, candidate freeProxyCandidate) bool {
	proxyCfg = models.NormalizeUpstreamProxy(proxyCfg)
	return normalizeProxyType(proxyCfg.Type) == normalizeProxyType(candidate.Type) && strings.EqualFold(strings.TrimSpace(proxyCfg.Host), strings.TrimSpace(candidate.Host)) && proxyCfg.Port == candidate.Port
}

func freeProxyCandidateKey(candidate freeProxyCandidate) string {
	return fmt.Sprintf("%s|%s|%d", normalizeProxyType(candidate.Type), strings.ToLower(strings.TrimSpace(candidate.Host)), candidate.Port)
}

func buildAutoProxyName(candidate freeProxyCandidate) string {
	proxyType := normalizeProxyType(candidate.Type)
	if proxyType == "socks5h" {
		proxyType = "socks5"
	}
	return fmt.Sprintf("AUTO %s://%s:%d", proxyType, candidate.Host, candidate.Port)
}

func cleanupImportedAutoProxies(store *db.Store, opts freeProxyImportOptions, now time.Time, summary *freeProxyImportSummary) {
	if !opts.CleanupEnabled {
		return
	}
	removedIDs := make(map[uint]struct{})
	kept := make([]models.UpstreamProxy, 0, len(store.Proxies))
	for _, proxyCfg := range store.Proxies {
		proxyCfg = models.NormalizeUpstreamProxy(proxyCfg)
		if proxyCfg.Source != models.ProxySourceAuto {
			kept = append(kept, proxyCfg)
			continue
		}
		removeFailed := opts.CleanupDeleteFailedAutoProxies && (proxyCfg.LastTest == nil || !proxyCfg.LastTest.Success)
		removeSlow := opts.CleanupMaxLatencyMs > 0 && proxyCfg.LastTest != nil && proxyCfg.LastTest.Success && proxyCfg.LastTest.ResponseTime > int64(opts.CleanupMaxLatencyMs)
		if !removeFailed && !removeSlow {
			kept = append(kept, proxyCfg)
			continue
		}
		removedIDs[proxyCfg.ID] = struct{}{}
		summary.CleanupDeletedCount++
		if removeFailed {
			summary.CleanedFailedCount++
		}
		if removeSlow {
			summary.CleanedSlowCount++
		}
	}
	if len(removedIDs) == 0 {
		return
	}
	store.Proxies = kept
	for i := range store.APIKeys {
		if store.APIKeys[i].ProxyID == 0 {
			continue
		}
		if _, ok := removedIDs[store.APIKeys[i].ProxyID]; !ok {
			continue
		}
		store.APIKeys[i].ProxyID = 0
		store.APIKeys[i].UpdatedAt = now
		summary.UnboundKeyCount++
	}
}

func enrichFreeProxyCountries(items []testedFreeProxy) {
	ips := make([]string, 0)
	seen := make(map[string]struct{})
	for _, item := range items {
		if strings.TrimSpace(item.Candidate.Country) != "" {
			continue
		}
		host := strings.TrimSpace(item.Candidate.Host)
		if host == "" || net.ParseIP(host) == nil {
			continue
		}
		if _, ok := seen[host]; ok {
			continue
		}
		seen[host] = struct{}{}
		ips = append(ips, host)
	}
	if len(ips) == 0 {
		return
	}
	resolved := lookupFreeProxyCountriesByBatch(ips)
	if len(resolved) == 0 {
		return
	}
	for i := range items {
		if strings.TrimSpace(items[i].Candidate.Country) != "" {
			continue
		}
		if country := strings.TrimSpace(resolved[strings.TrimSpace(items[i].Candidate.Host)]); country != "" {
			items[i].Candidate.Country = country
		}
	}
}

func lookupFreeProxyCountriesByBatch(ips []string) map[string]string {
	result := make(map[string]string, len(ips))
	for start := 0; start < len(ips); start += 100 {
		end := start + 100
		if end > len(ips) {
			end = len(ips)
		}
		body, err := json.Marshal(ips[start:end])
		if err != nil {
			continue
		}
		req, err := http.NewRequest(http.MethodPost, "http://ip-api.com/batch?fields=status,query,country,countryCode", bytes.NewReader(body))
		if err != nil {
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := freeProxySourceHTTPClient.Do(req)
		if err != nil {
			continue
		}
		payload, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		if err != nil || resp.StatusCode < 200 || resp.StatusCode >= 300 {
			continue
		}
		var rows []freeProxyBatchGeoResponse
		if err := json.Unmarshal(payload, &rows); err != nil {
			continue
		}
		for _, row := range rows {
			if strings.TrimSpace(row.Query) == "" || strings.ToLower(strings.TrimSpace(row.Status)) != "success" {
				continue
			}
			country := strings.TrimSpace(row.Country)
			if country == "" {
				country = strings.ToUpper(strings.TrimSpace(row.CountryCode))
			}
			if country != "" {
				result[strings.TrimSpace(row.Query)] = country
			}
		}
	}
	return result
}

func parseFreeProxyHTMLBody(body string, proxyType string) []freeProxyCandidate {
	items := make([]freeProxyCandidate, 0)
	for _, match := range freeProxyHTMLPairPattern.FindAllStringSubmatch(body, -1) {
		endpoint := strings.TrimSpace(match[1]) + ":" + strings.TrimSpace(match[2])
		if candidate, ok := buildFreeProxyCandidate(proxyType, endpoint, ""); ok {
			items = append(items, candidate)
		}
	}
	return items
}

var freeProxyHTTPSourcesHTML = []string{}

var freeProxySOCKS5SourcesHTML = []string{}

var freeProxyHTMLPairPattern = regexp.MustCompile(`(?s)(?:<td[^>]*>\s*|\b)((?:\d{1,3}\.){3}\d{1,3})(?:\s*</td>\s*<td[^>]*>|:|\s+)(\d{2,5})`)
