// One-shot: register a GitHub-backed vault and crawl it synchronously.
//
//	go run ./cmd/crawl-github <github-url>
//
// Defaults to the taiwan-md/knowledge subdir if no URL is given.
package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"

	"commonplace/internal/db"
	"commonplace/internal/external"

	_ "modernc.org/sqlite"
)

const dbPath = "/Users/shawnlee/commaplace/commonplace.db"

func main() {
	url := "https://github.com/frank890417/taiwan-md/tree/main/knowledge"
	if len(os.Args) > 1 {
		url = os.Args[1]
	}

	d, err := sql.Open("sqlite", dbPath)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer d.Close()
	if err := db.Migrate(d); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	store := &external.Store{DB: d}
	v, inserted, err := store.AddVault(context.Background(), url)
	if err != nil {
		log.Fatalf("AddVault: %v", err)
	}
	fmt.Printf("vault id=%d slug=%s kind=%s inserted=%v\n", v.ID, v.Slug, v.Kind, inserted)

	crawler := external.NewCrawler(store, map[string]external.Fetcher{
		"publish": external.NewPublishFetcher(),
		"quartz":  external.NewQuartzFetcher(),
		"github":  external.NewGitHubFetcher(),
	})
	crawler.PerFileDelay = 0 // local one-shot, hit raw.githubusercontent at full speed

	if err := crawler.CrawlByID(context.Background(), v.ID); err != nil {
		log.Fatalf("crawl: %v", err)
	}
	fmt.Println("done")
}
