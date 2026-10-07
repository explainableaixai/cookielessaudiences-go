# cookielessaudiences-go

A small Go client for cookieless audience segmentation and IAB categorization. Standard library only, context-aware, one file you can read in a few minutes.

```bash
go get github.com/explainableaixai/cookielessaudiences-go
```

## Usage

```go
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	ca "github.com/explainableaixai/cookielessaudiences-go"
)

func main() {
	client := ca.New(os.Getenv("COOKIELESS_KEY"))
	page, err := client.Segment(context.Background(), "https://example.com/blog")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(page["audience_type"], ca.Labels(page))
}
```

Results are `map[string]any`, so the response can grow without breaking your build.

## Functions

| Call | Purpose |
|---|---|
| `New(key)` | client with a 120 second timeout, exported `HTTP` and `Base` fields for overrides |
| `Segment(ctx, url)` | structured audience profile |
| `SegmentLegacy(ctx, url)` | free-text shape for older integrations |
| `Categorize(ctx, url)` | IAB v3 and v2 with confidence |
| `CategorizeText(ctx, text)` | IAB for plain text |
| `Vocabularies(ctx)` | public vocabularies |
| `Labels(result)` | readable names for codes |

## Typed errors

`*APIError` carries the status and the decoded body.

```go
var apiErr *ca.APIError
if errors.As(err, &apiErr) {
	switch apiErr.Status {
	case 403:
		log.Println("key inactive or credits used up")
	case 410, 411:
		log.Println("page unreadable, skipping")
	default:
		log.Println(apiErr)
	}
}
```

## Worker pool

```go
func segmentAll(ctx context.Context, c *ca.Client, urls []string, workers int) map[string]map[string]any {
	jobs := make(chan string)
	var mu sync.Mutex
	out := map[string]map[string]any{}
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for u := range jobs {
				res, err := c.Segment(ctx, u)
				if err != nil {
					continue
				}
				mu.Lock()
				out[u] = res
				mu.Unlock()
			}
		}()
	}
	for _, u := range urls {
		jobs <- u
	}
	close(jobs)
	wg.Wait()
	return out
}
```

## Where it fits: curating inventory

Supply-side teams use the profile to decide which domains belong in a package. A Go service can run the pool above over a candidate list and keep only the sites whose `interests` and `purchase_intent` match a deal. That is the daily work behind [inventory curation](https://www.cookielessaudiences.com/use-cases/inventory-curation.php).

For publishers describing their own audience to buyers, the same profile supports [seller-defined audiences](https://www.cookielessaudiences.com/features/seller-defined-audiences.php) without exposing a single user record.

## Testing your code

Set `Base` to an `httptest.Server` URL. Every call goes through one `post` function, so a fake server that returns `{"status":200,...}` is enough.

```go
c := ca.New("test")
c.Base = srv.URL
```

## Notes

- Requests are form-encoded POSTs; the body status is authoritative.
- Do not hard-code the key. Read it from the environment.
- The module has no dependencies outside the standard library.

<!--expanded-->
## Design notes

The module is one file you can read in a few minutes, and that is deliberate. A client library is code you will debug at an awkward hour, so it should not hide anything. `Client` holds a key, an `http.Client` and a base URL. Every method builds a form, posts it, decodes a map and checks the status inside the body.

Three choices are worth knowing about.

1. **Results are `map[string]any`.** The response gains fields as vocabularies grow. A map keeps your build green when that happens, and you can add typed structs for the fields you care about.
2. **Errors are typed.** `*APIError` carries the status, a plain message and the whole decoded body.
3. **Context is first.** Every call takes a `context.Context`, so cancellation and deadlines work the way Go programmers expect.

## Typed structs for the fields you use

Decode only what you need, and keep the rest as raw JSON:

```go
type Audience struct {
	Type         string `json:"audience_type"`
	Demographics struct {
		AgeBracket []string `json:"age_bracket"`
		Income     string   `json:"income_level"`
		Confidence string   `json:"confidence"`
	} `json:"demographics"`
	Interests struct {
		Tier1 []string `json:"tier1"`
	} `json:"interests"`
}

func typed(res map[string]any) (Audience, error) {
	raw, err := json.Marshal(res)
	if err != nil {
		return Audience{}, err
	}
	var a Audience
	return a, json.Unmarshal(raw, &a)
}
```

The cost of a marshal and unmarshal round trip is tiny compared with the network call, and it keeps the client free of structs that would need to change with every vocabulary release.

## Retries without surprises

Go programmers like explicit control, so the client does not retry for you. Here is a small helper that retries only the cases where retrying makes sense:

```go
func segmentWithRetry(ctx context.Context, c *ca.Client, url string) (map[string]any, error) {
	var lastErr error
	for attempt := 0; attempt < 4; attempt++ {
		res, err := c.Segment(ctx, url)
		if err == nil {
			return res, nil
		}
		var apiErr *ca.APIError
		if errors.As(err, &apiErr) {
			switch apiErr.Status {
			case 400, 401, 403, 407, 410:
				return nil, err // retrying will not help
			}
		}
		lastErr = err
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Duration(1<<attempt) * time.Second):
		}
	}
	return nil, lastErr
}
```

Statuses 400, 401 and 407 mean the request is wrong. 403 means the key is inactive or the credits are gone, and an alert is more useful than a retry. 410 means the page lacks content. Only transport errors and 411 are worth another attempt.

## Streaming a large file through a pipeline

A common job is to read a file of URLs, segment each one, and write results as JSON lines. Go handles it well with a reader, a worker pool and a writer:

```go
func run(ctx context.Context, c *ca.Client, in io.Reader, out io.Writer, workers int) error {
	urls := make(chan string)
	results := make(chan []byte)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for u := range urls {
				res, err := c.Segment(ctx, u)
				row := map[string]any{"url": u}
				if err != nil {
					row["error"] = err.Error()
				} else {
					row["result"] = res
				}
				b, _ := json.Marshal(row)
				results <- b
			}
		}()
	}
	go func() { wg.Wait(); close(results) }()
	go func() {
		sc := bufio.NewScanner(in)
		for sc.Scan() {
			urls <- strings.TrimSpace(sc.Text())
		}
		close(urls)
	}()
	for b := range results {
		fmt.Fprintf(out, "%s\n", b)
	}
	return nil
}
```

JSON lines are friendly to everything downstream, from `jq` to warehouse loaders. Keep the worker count modest. Sixteen is plenty for a first run.

## Deployment notes

A Go binary that calls this API usually runs as a cron job, a Kubernetes job or a small service. Four habits keep it dependable.

- Read the key from the environment or a secret mount. Never bake it into an image.
- Set a deadline on every context. The default client timeout is 120 seconds.
- Export counters for successes, each error status and cache hits. A rising 403 count is an early warning of an exhausted plan.
- Log the body of every `APIError` at debug level.

The Go ecosystem has good documentation conventions, and the [official Go documentation](https://go.dev/doc/) is the best place to read about contexts, the `net/http` client and testing with `httptest`. The module's own doc comments are available through pkg.go.dev once the repository is indexed.

## How teams use the profile

Supply side teams use the profile to build packages, buy side teams use it to shortlist sites, and data teams join the coded values onto their own tables. The [guide to audience personas for advertising](https://www.cookielessaudiences.com/features/audience-personas-advertising.php) gives the vocabulary planners use in meetings, and the [inventory curation use case](https://www.cookielessaudiences.com/use-cases/inventory-curation.php) shows how a package becomes a reproducible query.

If you also maintain a resume pipeline, the published report of [64 of 64 resume parser checks](https://www.resumereaderapi.com/quality-testing.php) is a good template for how to describe the testing behind a data API. If you size markets for acquisitions, the [industry domain counter](https://www.acquisitionuniverse.com/tools/industry-counter.php) shows how many domains sit in each category.

## Versioning and support

The module follows semantic versioning and publishes tags from the repository. Use `go get github.com/explainableaixai/cookielessaudiences-go@v1.0.0` to pin a release. Open an issue on the repository for client problems, or write to info@alpha-quantum.com for questions about plans and the service itself.

## Production checklist

Before you ship a service built on this module, walk through a short list. Is the key loaded from a secret store rather than a file in the repository? Is every call bounded by a context with a deadline? Do you cap concurrency below your plan's limits? Do you count each error status separately so you can see an exhausted plan before a customer does? Do you store the vocabulary version beside every cached answer? Can you replay a day's inputs without paying twice, because answers are cached by normalized URL?

If the answer to all six is yes, the integration is in better shape than most. If one answer is no, fix that first. It is the cheapest reliability work available, and it keeps your costs predictable while the data does its job.

<!--further-->
## Further reading

The [official Go site](https://go.dev/) hosts the language documentation, the effective Go guidance and the standard library reference. For this module the most relevant pages cover `net/http`, `context`, `encoding/json` and testing with `httptest`. Reading them alongside the single source file of this module is a quick way to see how a small, idiomatic client is put together.

## FAQ

**Which Go versions?** 1.20 and later.

**Is there a streaming mode?** No. Each call returns one JSON document.

**Who maintains it?** Alpha Quantum. info@alpha-quantum.com. MIT license.
