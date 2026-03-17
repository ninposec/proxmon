package main

import (
	"bufio"
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/http2"
)

const chromeUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

type Result struct {
	URL        string
	StatusCode int
	Error      error
	Duration   time.Duration
}

func main() {
	inputFile := flag.String("i", "", "Input file containing URLs (required)")
	proxyAddr := flag.String("p", "", "Proxy address as ip:port (required)")
	concurrency := flag.Int("c", 20, "Number of concurrent requests")
	flag.Parse()

	if *inputFile == "" || *proxyAddr == "" {
		fmt.Println("Error: Both -i (input file) and -p (proxy) parameters are required")
		flag.Usage()
		os.Exit(1)
	}

	urls, err := readURLsFromFile(*inputFile)
	if err != nil {
		fmt.Printf("Error reading file: %v\n", err)
		os.Exit(1)
	}

	if len(urls) == 0 {
		fmt.Println("No URLs found in input file")
		os.Exit(0)
	}

	fmt.Printf("Found %d URLs to request\n", len(urls))
	fmt.Printf("Using proxy: %s | Max Concurrency: %d\n", *proxyAddr, *concurrency)
	fmt.Println(strings.Repeat("-", 50))

	client, err := createProxyClient(*proxyAddr)
	if err != nil {
		fmt.Printf("Error creating proxy client: %v\n", err)
		os.Exit(1)
	}

	results := processURLs(client, urls, *concurrency)
	printSummary(results)
}

func readURLsFromFile(filename string) ([]string, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	var urls []string
	seen := make(map[string]bool)
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "http://") || strings.HasPrefix(line, "https://") {
			if !seen[line] {
				urls = append(urls, line)
				seen[line] = true
			}
		}
	}
	return urls, scanner.Err()
}

func createProxyClient(proxyAddr string) (*http.Client, error) {
	if !strings.HasPrefix(proxyAddr, "http://") && !strings.HasPrefix(proxyAddr, "https://") {
		proxyAddr = "http://" + proxyAddr
	}

	proxyURL, err := url.Parse(proxyAddr)
	if err != nil {
		return nil, fmt.Errorf("invalid proxy address: %w", err)
	}

	// Define the base transport
	transport := &http.Transport{
		Proxy: http.ProxyURL(proxyURL),
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
			// ALPN: Tells the server we support both h2 and http/1.1
			NextProtos: []string{"h2", "http/1.1"},
		},
		MaxIdleConns:        100,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
	}

	// Upgrade the transport to support HTTP/2
	if err := http2.ConfigureTransport(transport); err != nil {
		return nil, fmt.Errorf("failed to configure http2: %w", err)
	}

	return &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
	}, nil
}

func processURLs(client *http.Client, urls []string, concurrency int) []Result {
	var wg sync.WaitGroup
	results := make([]Result, len(urls))
	
	// Semaphore channel to control concurrency
	sem := make(chan struct{}, concurrency)

	for i, urlStr := range urls {
		wg.Add(1)
		go func(index int, u string) {
			defer wg.Done()
			sem <- struct{}{}        // Acquire
			results[index] = makeRequest(client, u)
			<-sem                    // Release
		}(i, urlStr)
	}

	wg.Wait()
	return results
}

func makeRequest(client *http.Client, urlStr string) Result {
	start := time.Now()
	result := Result{URL: urlStr}

	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		result.Error = err
		result.Duration = time.Since(start)
		return result
	}

	req.Header.Set("User-Agent", chromeUserAgent)

	resp, err := client.Do(req)
	result.Duration = time.Since(start)

	if err != nil {
		result.Error = err
		fmt.Printf("  ✗ Error: %s: %v\n", urlStr, err)
		return result
	}
	defer resp.Body.Close()

	result.StatusCode = resp.StatusCode
	io.Copy(io.Discard, resp.Body)

	fmt.Printf("  ✓ %d - %s (%.2fs)\n", resp.StatusCode, urlStr, result.Duration.Seconds())
	return result
}

func printSummary(results []Result) {
	fmt.Println(strings.Repeat("-", 50))
	fmt.Println("Summary:")

	successCount, errorCount := 0, 0
	statusCodes := make(map[int]int)

	for _, result := range results {
		if result.Error != nil {
			errorCount++
		} else {
			successCount++
			statusCodes[result.StatusCode]++
		}
	}

	fmt.Printf("  Total: %d | Success: %d | Errors: %d\n", len(results), successCount, errorCount)
	if len(statusCodes) > 0 {
		fmt.Println("  Status codes breakdown:")
		for code, count := range statusCodes {
			fmt.Printf("    %d: %d\n", code, count)
		}
	}
}
