package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var (
	baseURL   = flag.String("url", "http://localhost:8080", "WasmDB base URL")
	dbName    = flag.String("db", "bench", "table name to use")
	cleanup   = flag.Bool("cleanup", true, "delete table after benchmarks")
	benchmarks = flag.String("bench", "1,2,3,4,5,6,7", "comma-separated benchmark numbers to run")
)

// Document types matching the API.
type document struct {
	ID         string         `json:"id,omitempty"`
	Content    string         `json:"content,omitempty"`
	Attributes map[string]any `json:"attributes,omitempty"`
	CreatedAt  time.Time      `json:"created_at,omitzero"`
	UpdatedAt  time.Time      `json:"updated_at,omitzero"`
	Version    uint64         `json:"version,omitempty"`
}

type createTableRequest struct {
	Name   string `json:"name"`
	Schema *schema `json:"schema,omitempty"`
}

type schema struct {
	Fields []fieldDef `json:"fields"`
}

type fieldDef struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Required bool   `json:"required,omitempty"`
	Indexed  bool   `json:"indexed,omitempty"`
	FullText bool   `json:"full_text,omitempty"`
}

type filter struct {
	Field string `json:"field"`
	Op    string `json:"op"`
	Value any    `json:"value"`
}

type searchRequest struct {
	Filters []filter `json:"filters"`
	Limit   int      `json:"limit"`
	Offset  int      `json:"offset"`
}

type textSearchRequest struct {
	Query  string `json:"query"`
	Limit  int    `json:"limit"`
	Offset int    `json:"offset"`
}

// Synthetic data.
var (
	categories = []string{"security", "networking", "storage", "compute", "monitoring", "logging", "auth", "billing", "compliance", "analytics"}
	priorities = []string{"low", "medium", "high"}
	statuses   = []string{"open", "in_progress", "resolved", "closed"}

	contentTemplates = []string{
		"Investigate the %s issue reported in the %s subsystem. The team has observed intermittent failures during peak load conditions that affect overall system reliability.",
		"Deploy updated configuration for the %s service in the %s environment. This change addresses several known edge cases and improves performance under concurrent access.",
		"Review access control policies for %s resources. Current policy may not adequately restrict unauthorized access in multi-tenant deployments with shared %s infrastructure.",
		"Optimize %s query performance by analyzing slow log entries from the %s cluster. Index restructuring may provide significant throughput improvements.",
		"Document the %s integration workflow including setup, configuration, and troubleshooting steps for the %s pipeline. Include rollback procedures.",
		"Implement retry logic for the %s client when communicating with the %s backend. Current timeout settings may be too aggressive for high-latency environments.",
		"Audit %s certificate rotation schedule and ensure automated renewal is working correctly for all %s endpoints. Verify expiration monitoring alerts.",
		"Evaluate %s capacity planning metrics and project resource needs for the next quarter. The %s workload has grown significantly since last review.",
	}

	subjects = []string{"database", "API", "cache", "queue", "proxy", "gateway", "scheduler", "worker", "controller", "pipeline"}
	envs     = []string{"production", "staging", "development", "DR", "performance-test"}
)

func generateDoc() *document {
	cat := categories[rand.IntN(len(categories))]
	pri := priorities[rand.IntN(len(priorities))]
	status := statuses[rand.IntN(len(statuses))]

	tmpl := contentTemplates[rand.IntN(len(contentTemplates))]
	subj := subjects[rand.IntN(len(subjects))]
	env := envs[rand.IntN(len(envs))]
	content := fmt.Sprintf(tmpl, subj, env)

	return &document{
		Content: content,
		Attributes: map[string]any{
			"category": cat,
			"priority": pri,
			"status":   status,
		},
	}
}

// Stats tracks latency measurements.
type stats struct {
	latencies []time.Duration
}

func (s *stats) add(d time.Duration) {
	s.latencies = append(s.latencies, d)
}

func (s *stats) percentile(p float64) time.Duration {
	if len(s.latencies) == 0 {
		return 0
	}
	sorted := make([]time.Duration, len(s.latencies))
	copy(sorted, s.latencies)
	slices.Sort(sorted)
	idx := int(float64(len(sorted)-1) * p)
	return sorted[idx]
}

func (s *stats) count() int {
	return len(s.latencies)
}

// HTTP helpers.
func doJSON(method, url string, body any) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		bodyReader = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return http.DefaultClient.Do(req)
}

func mustDoJSON(method, url string, body any) *http.Response {
	resp, err := doJSON(method, url, body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %s %s: %v\n", method, url, err)
		os.Exit(1)
	}
	return resp
}

func readBody(resp *http.Response) []byte {
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return data
}

func shouldRun(n int) bool {
	target := fmt.Sprintf("%d", n)
	for s := range strings.SplitSeq(*benchmarks, ",") {
		if strings.TrimSpace(s) == target {
			return true
		}
	}
	return false
}

func main() {
	flag.Parse()

	// Increase default timeout for bulk operations.
	http.DefaultClient.Timeout = 5 * time.Minute

	fmt.Printf("WasmDB Benchmark Suite\n")
	fmt.Printf("Target: %s\n", *baseURL)
	fmt.Printf("Table: %s\n\n", *dbName)

	// Setup: create table with schema.
	setupDB()
	if *cleanup {
		defer cleanupDB()
	}

	// Collect doc IDs from writes for read benchmarks.
	var docIDs []string

	if shouldRun(1) {
		docIDs = append(docIDs, bench1SerialWrite()...)
	}

	if shouldRun(2) {
		docIDs = append(docIDs, bench2ConcurrentWrite()...)
	}

	if shouldRun(3) {
		bench3PointRead(docIDs)
	}

	if shouldRun(4) {
		bench4AttributeSearch()
	}

	if shouldRun(5) {
		bench5TextSearch()
	}

	if shouldRun(6) {
		bench6WriteReadConsistency()
	}

	if shouldRun(7) {
		bench7MillionRecords()
	}
}

func setupDB() {
	// Delete if exists (ignore errors).
	resp, _ := doJSON("DELETE", *baseURL+"/v1/tables/"+*dbName, nil)
	if resp != nil {
		resp.Body.Close()
	}

	resp = mustDoJSON("POST", *baseURL+"/v1/tables", createTableRequest{
		Name: *dbName,
		Schema: &schema{
			Fields: []fieldDef{
				{Name: "category", Type: "string", Required: true, Indexed: true},
				{Name: "priority", Type: "string", Required: true, Indexed: true},
				{Name: "status", Type: "string", Required: true, Indexed: true},
			},
		},
	})
	body := readBody(resp)
	if resp.StatusCode != 201 {
		fmt.Fprintf(os.Stderr, "Failed to create table: %s\n", body)
		os.Exit(1)
	}
	fmt.Printf("Created table %q\n\n", *dbName)
}

func cleanupDB() {
	resp, _ := doJSON("DELETE", *baseURL+"/v1/tables/"+*dbName, nil)
	if resp != nil {
		resp.Body.Close()
		fmt.Printf("\nCleaned up table %q\n", *dbName)
	}
}

// Benchmark 1: Serial Write Throughput
func bench1SerialWrite() []string {
	const n = 500
	fmt.Printf("=== Benchmark 1: Serial Write (%d docs) ===\n", n)

	var st stats
	var ids []string

	start := time.Now()
	for range n {
		doc := generateDoc()
		t := time.Now()
		resp := mustDoJSON("POST", *baseURL+"/v1/tables/"+*dbName+"/documents", doc)
		elapsed := time.Since(t)
		body := readBody(resp)
		if resp.StatusCode != 201 {
			fmt.Fprintf(os.Stderr, "  Write failed [%d]: %s\n", resp.StatusCode, body)
			continue
		}
		st.add(elapsed)

		var created document
		json.Unmarshal(body, &created)
		ids = append(ids, created.ID)
	}
	totalTime := time.Since(start)

	throughput := float64(st.count()) / totalTime.Seconds()
	fmt.Printf("  Throughput:  %.1f docs/sec\n", throughput)
	fmt.Printf("  Latency:     p50=%v  p95=%v  p99=%v\n", st.percentile(0.50), st.percentile(0.95), st.percentile(0.99))
	fmt.Printf("  Total:       %d docs in %v\n\n", st.count(), totalTime.Round(time.Millisecond))

	return ids
}

// Benchmark 2: Concurrent Write Throughput
func bench2ConcurrentWrite() []string {
	const n = 500
	const workers = 10
	fmt.Printf("=== Benchmark 2: Concurrent Write (%d docs, %d workers) ===\n", n, workers)

	var st stats
	var mu sync.Mutex
	var ids []string

	docs := make([]*document, n)
	for i := range n {
		docs[i] = generateDoc()
	}

	work := make(chan *document, n)
	for _, d := range docs {
		work <- d
	}
	close(work)

	var wg sync.WaitGroup
	start := time.Now()

	for range workers {
		wg.Go(func() {
			for doc := range work {
				t := time.Now()
				resp := mustDoJSON("POST", *baseURL+"/v1/tables/"+*dbName+"/documents", doc)
				elapsed := time.Since(t)
				body := readBody(resp)

				mu.Lock()
				if resp.StatusCode == 201 {
					st.add(elapsed)
					var created document
					json.Unmarshal(body, &created)
					ids = append(ids, created.ID)
				} else {
					fmt.Fprintf(os.Stderr, "  Write failed [%d]: %s\n", resp.StatusCode, body)
				}
				mu.Unlock()
			}
		})
	}

	wg.Wait()
	totalTime := time.Since(start)

	throughput := float64(st.count()) / totalTime.Seconds()
	fmt.Printf("  Throughput:  %.1f docs/sec\n", throughput)
	fmt.Printf("  Latency:     p50=%v  p95=%v  p99=%v\n", st.percentile(0.50), st.percentile(0.95), st.percentile(0.99))
	fmt.Printf("  Total:       %d docs in %v\n\n", st.count(), totalTime.Round(time.Millisecond))

	return ids
}

// Benchmark 3: Point Read Latency
func bench3PointRead(ids []string) {
	const n = 200
	fmt.Printf("=== Benchmark 3: Point Read (%d random docs) ===\n", n)

	if len(ids) == 0 {
		fmt.Printf("  SKIPPED: no document IDs available (run benchmarks 1 or 2 first)\n\n")
		return
	}

	var st stats
	for range n {
		id := ids[rand.IntN(len(ids))]
		t := time.Now()
		resp := mustDoJSON("GET", *baseURL+"/v1/databases/"+*dbName+"/documents/"+id, nil)
		elapsed := time.Since(t)
		readBody(resp)

		if resp.StatusCode == 200 {
			st.add(elapsed)
		} else {
			fmt.Fprintf(os.Stderr, "  Read failed [%d] for %s\n", resp.StatusCode, id)
		}
	}

	fmt.Printf("  Latency:     p50=%v  p95=%v  p99=%v\n", st.percentile(0.50), st.percentile(0.95), st.percentile(0.99))
	fmt.Printf("  Reads:       %d\n\n", st.count())
}

// Benchmark 4: Attribute Search Latency
func bench4AttributeSearch() {
	fmt.Printf("=== Benchmark 4: Attribute Search (small dataset) ===\n")

	// Wait for index builder to catch up.
	fmt.Printf("  Waiting 3s for index builder...\n")
	time.Sleep(3 * time.Second)

	queries := []struct {
		name    string
		filters []filter
	}{
		{
			name:    `category="security"`,
			filters: []filter{{Field: "category", Op: "eq", Value: "security"}},
		},
		{
			name:    `status="open"`,
			filters: []filter{{Field: "status", Op: "eq", Value: "open"}},
		},
		{
			name: `category="security" AND status="open"`,
			filters: []filter{
				{Field: "category", Op: "eq", Value: "security"},
				{Field: "status", Op: "eq", Value: "open"},
			},
		},
	}

	const iterations = 20

	for _, q := range queries {
		var st stats
		var resultCount int

		for range iterations {
			t := time.Now()
			resp := mustDoJSON("POST", *baseURL+"/v1/tables/"+*dbName+"/search/attributes", searchRequest{
				Filters: q.filters,
				Limit:   100,
				Offset:  0,
			})
			elapsed := time.Since(t)
			body := readBody(resp)

			if resp.StatusCode == 200 {
				st.add(elapsed)
				var results []json.RawMessage
				json.Unmarshal(body, &results)
				resultCount = len(results)
			} else {
				fmt.Fprintf(os.Stderr, "  Search failed [%d]: %s\n", resp.StatusCode, body)
			}
		}

		fmt.Printf("  Query: %s\n", q.name)
		fmt.Printf("    Results:   %d (limit 100)\n", resultCount)
		fmt.Printf("    Latency:   p50=%v  p95=%v  p99=%v\n", st.percentile(0.50), st.percentile(0.95), st.percentile(0.99))
	}
	fmt.Println()
}

// Benchmark 5: Full-Text Search Latency
func bench5TextSearch() {
	fmt.Printf("=== Benchmark 5: Full-Text Search (small dataset) ===\n")

	// Wait for index builder to catch up.
	fmt.Printf("  Waiting 3s for index builder...\n")
	time.Sleep(3 * time.Second)

	queries := []string{
		"security",
		"performance optimization",
		"certificate rotation",
		"database production",
	}

	const iterations = 20

	for _, q := range queries {
		var st stats
		var resultCount int

		for range iterations {
			t := time.Now()
			resp, err := doJSON("POST", *baseURL+"/v1/tables/"+*dbName+"/search/text", textSearchRequest{
				Query:  q,
				Limit:  10,
				Offset: 0,
			})
			elapsed := time.Since(t)
			if err != nil {
				fmt.Fprintf(os.Stderr, "  Search error: %v\n", err)
				continue
			}
			body := readBody(resp)

			if resp.StatusCode == 200 {
				st.add(elapsed)
				var result struct {
					Results []json.RawMessage `json:"results"`
					Total   int              `json:"total"`
				}
				json.Unmarshal(body, &result)
				resultCount = result.Total
			} else {
				fmt.Fprintf(os.Stderr, "  Search failed [%d]: %s\n", resp.StatusCode, body)
			}
		}

		fmt.Printf("  Query: %q\n", q)
		fmt.Printf("    Total hits: %d  (returned limit 10)\n", resultCount)
		fmt.Printf("    Latency:    p50=%v  p95=%v  p99=%v\n", st.percentile(0.50), st.percentile(0.95), st.percentile(0.99))
	}
	fmt.Println()
}

// Benchmark 6: Write + Immediate Read Consistency
func bench6WriteReadConsistency() {
	const n = 100
	fmt.Printf("=== Benchmark 6: Write + Immediate Read Consistency (%d docs) ===\n", n)

	var st stats
	mismatches := 0

	for i := range n {
		doc := generateDoc()
		doc.Content = fmt.Sprintf("consistency-check-%d-%d", i, time.Now().UnixNano())

		// Write.
		t := time.Now()
		resp := mustDoJSON("POST", *baseURL+"/v1/tables/"+*dbName+"/documents", doc)
		body := readBody(resp)
		if resp.StatusCode != 201 {
			fmt.Fprintf(os.Stderr, "  Write failed [%d]: %s\n", resp.StatusCode, body)
			continue
		}
		var created document
		json.Unmarshal(body, &created)

		// Immediate read.
		resp2 := mustDoJSON("GET", *baseURL+"/v1/databases/"+*dbName+"/documents/"+created.ID, nil)
		body2 := readBody(resp2)
		elapsed := time.Since(t)

		if resp2.StatusCode != 200 {
			fmt.Fprintf(os.Stderr, "  Read-after-write failed [%d]: %s\n", resp2.StatusCode, body2)
			mismatches++
			continue
		}

		var fetched document
		json.Unmarshal(body2, &fetched)

		if fetched.Content != doc.Content {
			mismatches++
		}

		st.add(elapsed)
	}

	fmt.Printf("  Latency:     p50=%v  p95=%v  p99=%v\n", st.percentile(0.50), st.percentile(0.95), st.percentile(0.99))
	fmt.Printf("  Correct:     %d/%d (mismatches: %d)\n\n", st.count()-mismatches, st.count(), mismatches)
}

// Benchmark 7: 1M Record Attribute Filter
func bench7MillionRecords() {
	const totalDocs = 1_000_000
	const batchSize = 500
	fmt.Printf("=== Benchmark 7: 1M Record Insert + Attribute Filter ===\n")

	// Phase 1: Bulk insert.
	fmt.Printf("  Inserting %d documents via bulk API (batch size %d)...\n", totalDocs, batchSize)

	var insertedCount atomic.Int64
	insertStart := time.Now()

	// Use concurrent bulk requests to maximize throughput.
	const bulkWorkers = 4
	batches := make(chan int, totalDocs/batchSize)
	for i := range totalDocs / batchSize {
		batches <- i
	}
	close(batches)

	var wg sync.WaitGroup
	var bulkErrors atomic.Int64

	// Progress reporter.
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				count := insertedCount.Load()
				elapsed := time.Since(insertStart)
				rate := float64(count) / elapsed.Seconds()
				pct := float64(count) / float64(totalDocs) * 100
				fmt.Printf("  Progress: %d/%d (%.1f%%) - %.0f docs/sec - %v elapsed\n",
					count, totalDocs, pct, rate, elapsed.Round(time.Second))
			}
		}
	}()

	for range bulkWorkers {
		wg.Go(func() {
			for range batches {
				docs := make([]*document, batchSize)
				for j := range batchSize {
					docs[j] = generateDoc()
				}

				resp, err := doJSON("POST", *baseURL+"/v1/tables/"+*dbName+"/documents/_bulk", docs)
				if err != nil {
					fmt.Fprintf(os.Stderr, "  Bulk request error: %v\n", err)
					bulkErrors.Add(1)
					continue
				}
				body := readBody(resp)
				if resp.StatusCode != 201 {
					fmt.Fprintf(os.Stderr, "  Bulk failed [%d]: %s\n", resp.StatusCode, body)
					bulkErrors.Add(1)
					continue
				}
				insertedCount.Add(int64(batchSize))
			}
		})
	}

	wg.Wait()
	close(done)
	insertTime := time.Since(insertStart)
	inserted := insertedCount.Load()

	fmt.Printf("  Insert:      %d docs in %v (%.0f docs/sec, %d batch errors)\n",
		inserted, insertTime.Round(time.Second), float64(inserted)/insertTime.Seconds(), bulkErrors.Load())

	// Phase 2: Wait for index builder.
	fmt.Printf("  Waiting 30s for index builder to process 1M docs...\n")
	time.Sleep(30 * time.Second)

	// Phase 3: Attribute queries.
	queries := []struct {
		name    string
		filters []filter
	}{
		{
			name:    `category="security"`,
			filters: []filter{{Field: "category", Op: "eq", Value: "security"}},
		},
		{
			name: `category="security" AND status="open"`,
			filters: []filter{
				{Field: "category", Op: "eq", Value: "security"},
				{Field: "status", Op: "eq", Value: "open"},
			},
		},
		{
			name:    `priority="high"`,
			filters: []filter{{Field: "priority", Op: "eq", Value: "high"}},
		},
	}

	const iterations = 20

	for _, q := range queries {
		var st stats
		var resultCount int

		for range iterations {
			t := time.Now()
			resp := mustDoJSON("POST", *baseURL+"/v1/tables/"+*dbName+"/search/attributes", searchRequest{
				Filters: q.filters,
				Limit:   100,
				Offset:  0,
			})
			elapsed := time.Since(t)
			body := readBody(resp)

			if resp.StatusCode == 200 {
				st.add(elapsed)
				var results []json.RawMessage
				json.Unmarshal(body, &results)
				resultCount = len(results)
			} else {
				fmt.Fprintf(os.Stderr, "  Search failed [%d]: %s\n", resp.StatusCode, body)
			}
		}

		fmt.Printf("  Query: %s\n", q.name)
		fmt.Printf("    Returned:  %d (limit 100)\n", resultCount)
		fmt.Printf("    Latency:   p50=%v  p95=%v  p99=%v\n", st.percentile(0.50), st.percentile(0.95), st.percentile(0.99))
	}
	fmt.Println()
}
