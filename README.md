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

## FAQ

**Which Go versions?** 1.20 and later.

**Is there a streaming mode?** No. Each call returns one JSON document.

**Who maintains it?** Alpha Quantum. info@alpha-quantum.com. MIT license.
