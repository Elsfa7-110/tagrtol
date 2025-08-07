package main

import (
	"bufio"
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html"
)

type Result struct {
	Type       string
	Status     string
	URL        string
	SourcePage string
}

var (
	httpClient  = &http.Client{Timeout: 7 * time.Second}
	maxWorkers  = 50
	githubToken = os.Getenv("GITHUB_TOKEN")
)

// ------------------ Link Extraction ------------------ //
func extractLinks(site string) []string {
	resp, err := httpClient.Get(site)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	links := []string{}
	tokenizer := html.NewTokenizer(resp.Body)
	for {
		tt := tokenizer.Next()
		if tt == html.ErrorToken {
			break
		}
		token := tokenizer.Token()
		if token.Data == "a" {
			for _, attr := range token.Attr {
				if attr.Key == "href" {
					u, err := url.Parse(attr.Val)
					if err == nil {
						if !u.IsAbs() {
							u = resp.Request.URL.ResolveReference(u)
						}
						links = append(links, u.String())
					}
				}
			}
		}
	}
	return links
}

func readBody(resp *http.Response) string {
	buf := new(strings.Builder)
	io.Copy(buf, resp.Body)
	return buf.String()
}

// ------------------ GitHub Support ------------------ //
func checkGitHub(link string) Result {
	parsed, _ := url.Parse(link)
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")

	if strings.Contains(link, "github.io") {
		resp, err := httpClient.Get(link)
		if err != nil {
			return Result{"github_pages", "connection_error", link, ""}
		}
		defer resp.Body.Close()
		body := readBody(resp)
		if strings.Contains(body, "There isn't a GitHub Pages site here.") {
			return Result{"github_pages", "possible_takeover", link, ""}
		}
		return Result{"github_pages", "ok", link, ""}
	}

	if strings.Contains(link, "gist.github.com") && len(parts) >= 2 {
		api := fmt.Sprintf("https://api.github.com/gists/%s", parts[len(parts)-1])
		return githubAPIRequest("gist", api, link)
	}

	if len(parts) >= 2 {
		api := fmt.Sprintf("https://api.github.com/repos/%s/%s", parts[0], parts[1])
		return githubAPIRequest("repo", api, link)
	}

	if len(parts) == 1 && parts[0] != "" {
		api := fmt.Sprintf("https://api.github.com/users/%s", parts[0])
		return githubAPIRequest("user", api, link)
	}

	return Result{"github", "invalid_url", link, ""}
}

func githubAPIRequest(entityType, api, link string) Result {
	req, _ := http.NewRequest("GET", api, nil)
	req.Header.Set("User-Agent", "Takeover-Scanner")
	if githubToken != "" {
		req.Header.Set("Authorization", "token "+githubToken)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return Result{entityType, "connection_error", link, ""}
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		return Result{entityType, "not_found", link, ""}
	} else if resp.StatusCode == 200 {
		return Result{entityType, "exists", link, ""}
	} else if resp.StatusCode == 403 || resp.StatusCode == 429 {
		return Result{entityType, "rate_limited", link, ""}
	} else {
		return Result{entityType, fmt.Sprintf("error_%d", resp.StatusCode), link, ""}
	}
}

// ------------------ Other Services ------------------ //
func checkS3(link string) Result {
	resp, err := httpClient.Get(link)
	if err != nil {
		return Result{"s3", "connection_error", link, ""}
	}
	defer resp.Body.Close()
	body := readBody(resp)
	if strings.Contains(body, "NoSuchBucket") {
		return Result{"s3", "possible_takeover", link, ""}
	}
	return Result{"s3", "ok", link, ""}
}

func checkHeroku(link string) Result {
	resp, err := httpClient.Get(link)
	if err != nil {
		return Result{"heroku", "connection_error", link, ""}
	}
	defer resp.Body.Close()
	body := readBody(resp)
	if strings.Contains(body, "no such app") {
		return Result{"heroku", "possible_takeover", link, ""}
	}
	return Result{"heroku", "ok", link, ""}
}

func checkVercel(link string) Result {
	resp, err := httpClient.Get(link)
	if err != nil {
		return Result{"vercel", "connection_error", link, ""}
	}
	defer resp.Body.Close()
	body := readBody(resp)
	if strings.Contains(body, "Vercel") && strings.Contains(body, "404") {
		return Result{"vercel", "possible_takeover", link, ""}
	}
	return Result{"vercel", "ok", link, ""}
}

func checkNetlify(link string) Result {
	resp, err := httpClient.Get(link)
	if err != nil {
		return Result{"netlify", "connection_error", link, ""}
	}
	defer resp.Body.Close()
	body := readBody(resp)
	if strings.Contains(body, "Not Found") && strings.Contains(body, "netlify") {
		return Result{"netlify", "possible_takeover", link, ""}
	}
	return Result{"netlify", "ok", link, ""}
}

func checkChromeExtension(link string) Result {
	u, err := url.Parse(link)
	if err != nil {
		return Result{"chrome_ext", "invalid_url", link, ""}
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 {
		return Result{"chrome_ext", "invalid_url", link, ""}
	}
	extID := parts[len(parts)-1]

	checkURL := "https://chrome.google.com/webstore/detail/" + extID
	resp, err := httpClient.Get(checkURL)
	if err != nil {
		return Result{"chrome_ext", "connection_error", link, ""}
	}
	defer resp.Body.Close()
	body := readBody(resp)
	if strings.Contains(body, "Item not found") || strings.Contains(body, "404") {
		return Result{"chrome_ext", "not_found", link, ""}
	}
	return Result{"chrome_ext", "exists", link, ""}
}

func checkWix(link string) Result {
	resp, err := httpClient.Get(link)
	if err != nil {
		return Result{"wix", "connection_error", link, ""}
	}
	defer resp.Body.Close()
	body := readBody(resp)
	if strings.Contains(body, "domain isn’t connected to a website") || strings.Contains(body, "Looks like this domain") {
		return Result{"wix", "possible_takeover", link, ""}
	}
	return Result{"wix", "ok", link, ""}
}

func checkTumblr(link string) Result {
	resp, err := httpClient.Get(link)
	if err != nil {
		return Result{"tumblr", "connection_error", link, ""}
	}
	defer resp.Body.Close()
	body := readBody(resp)
	if strings.Contains(body, "There's nothing here") || resp.StatusCode == 404 {
		return Result{"tumblr", "possible_takeover", link, ""}
	}
	return Result{"tumblr", "ok", link, ""}
}

func checkShopify(link string) Result {
	resp, err := httpClient.Get(link)
	if err != nil {
		return Result{"shopify", "connection_error", link, ""}
	}
	defer resp.Body.Close()
	body := readBody(resp)
	if strings.Contains(body, "store is unavailable") || strings.Contains(body, "This store is unavailable") {
		return Result{"shopify", "possible_takeover", link, ""}
	}
	return Result{"shopify", "ok", link, ""}
}

// ------------------ Dispatcher ------------------ //
func detectService(link string) Result {
	switch {
	case strings.Contains(link, "github.com"), strings.Contains(link, "github.io"), strings.Contains(link, "gist.github.com"):
		return checkGitHub(link)
	case strings.Contains(link, ".s3.amazonaws.com"):
		return checkS3(link)
	case strings.Contains(link, "herokuapp.com"):
		return checkHeroku(link)
	case strings.Contains(link, ".vercel.app"):
		return checkVercel(link)
	case strings.Contains(link, ".netlify.app"):
		return checkNetlify(link)
	case strings.Contains(link, "chrome.google.com/webstore/detail/"):
		return checkChromeExtension(link)
	case strings.Contains(link, ".wixsite.com"):
		return checkWix(link)
	case strings.Contains(link, ".tumblr.com"):
		return checkTumblr(link)
	case strings.Contains(link, ".myshopify.com"):
		return checkShopify(link)
	default:
		return Result{"unknown", "skipped", link, ""}
	}
}

// ------------------ Worker ------------------ //
func worker(wg *sync.WaitGroup, jobs <-chan string, results chan<- Result) {
	defer wg.Done()
	for site := range jobs {
		fmt.Println("[*] Scanning site:", site)
		links := extractLinks(site)
		for _, l := range links {
			r := detectService(l)
			r.SourcePage = site
			if r.Type != "unknown" {
				switch r.Status {
				case "possible_takeover":
					fmt.Printf("  \033[31m→ [%s] %s: %s (found in: %s)\033[0m\n", strings.ToUpper(r.Type), r.Status, r.URL, r.SourcePage)
				case "rate_limited":
					fmt.Printf("  \033[33m→ [%s] %s: %s\033[0m\n", strings.ToUpper(r.Type), r.Status, r.URL)
				default:
					fmt.Printf("  → [%s] %s: %s\n", strings.ToUpper(r.Type), r.Status, r.URL)
				}
				results <- r
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
}

// ------------------ Main ------------------ //
func main() {
	file, err := os.Open("sites.txt")
	if err != nil {
		fmt.Println("Error opening sites.txt:", err)
		return
	}
	defer file.Close()

	var sites []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			sites = append(sites, line)
		}
	}

	jobs := make(chan string, len(sites))
	resultsChan := make(chan Result, 100)

	var wg sync.WaitGroup
	for i := 0; i < maxWorkers; i++ {
		wg.Add(1)
		go worker(&wg, jobs, resultsChan)
	}

	for _, site := range sites {
		jobs <- site
	}
	close(jobs)

	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	var results []Result
	for r := range resultsChan {
		results = append(results, r)
	}

	csvFile, err := os.Create("results.csv")
	if err != nil {
		fmt.Println("Error creating results.csv:", err)
		return
	}
	defer csvFile.Close()

	writer := csv.NewWriter(csvFile)
	writer.Write([]string{"Type", "Status", "URL", "SourcePage"})
	for _, r := range results {
		writer.Write([]string{r.Type, r.Status, r.URL, r.SourcePage})
	}
	writer.Flush()

	fmt.Println("\n✅ Scan completed. Results saved to results.csv")

	// ✅ Save only takeovers to takeovers.txt
	takeoverFile, err := os.Create("takeovers.txt")
	if err != nil {
		fmt.Println("Error creating takeovers.txt:", err)
		return
	}
	defer takeoverFile.Close()

	for _, r := range results {
		if r.Status == "possible_takeover" {
			takeoverFile.WriteString(fmt.Sprintf("%s (found in: %s)\n", r.URL, r.SourcePage))
		}
	}

	fmt.Println("✅ Only takeovers saved to takeovers.txt")
}
