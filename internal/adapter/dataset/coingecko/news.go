package coingecko

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"depeg.local/platform/internal/domain/dataset"
	"depeg.local/platform/internal/identity"
)

func (s *scan) news() error {
	meta, err := s.read("coingecko_news/manifest.json", 1<<20)
	if err != nil {
		return err
	}
	var manifest struct {
		Count   int `json:"unique_article_count"`
		PerCoin map[string]struct {
			CoinID string `json:"coin_id"`
		} `json:"per_coin"`
	}
	if json.Unmarshal(meta, &manifest) != nil || manifest.Count < 1 || manifest.Count > 100000 {
		return fmt.Errorf("invalid news manifest")
	}
	for _, coin := range manifest.PerCoin {
		if !coinIDPattern.MatchString(coin.CoinID) {
			return fmt.Errorf("invalid news manifest coin ID")
		}
		s.available[coin.CoinID] = true
	}
	data, err := s.read("coingecko_news/news.jsonl", maxEntryBytes)
	if err != nil {
		return err
	}
	input := bufio.NewScanner(bytes.NewReader(data))
	input.Buffer(make([]byte, 4096), 256<<10)
	seen := map[string]bool{}
	count := 0
	batch := dataset.Batch{News: make([]dataset.News, 0, 200)}
	flush := func() error {
		if s.write != nil && len(batch.News) > 0 {
			if err := s.write(batch); err != nil {
				return err
			}
		}
		s.report.News += int64(len(batch.News))
		batch.News = make([]dataset.News, 0, 200)
		return nil
	}
	for input.Scan() {
		if err := s.ctx.Err(); err != nil {
			return err
		}
		line := strings.TrimSpace(input.Text())
		if line == "" {
			continue
		}
		count++
		if count > 100000 {
			return fmt.Errorf("maximum 100000 news records")
		}
		item, err := s.article(line)
		if err != nil {
			return fmt.Errorf("news record %d: %w", count, err)
		}
		if seen[item.ID] {
			return fmt.Errorf("duplicate news URL in input")
		}
		seen[item.ID] = true
		selected := false
		for _, coin := range item.CoinIDs {
			s.available[coin] = true
			selected = selected || s.includes(coin)
		}
		if !selected {
			continue
		}
		batch.News = append(batch.News, item)
		if len(batch.News) == 200 {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	if err := input.Err(); err != nil {
		return err
	}
	if count != manifest.Count {
		return fmt.Errorf("news count differs from manifest")
	}
	return flush()
}

func (s *scan) article(line string) (dataset.News, error) {
	var raw struct {
		Title      string          `json:"title"`
		URL        string          `json:"url"`
		Image      string          `json:"image"`
		Author     json.RawMessage `json:"author"`
		PostedAt   string          `json:"posted_at"`
		FetchedAt  string          `json:"fetched_at"`
		Type       string          `json:"type"`
		SourceName string          `json:"source_name"`
		Coins      []string        `json:"matched_coin_ids"`
	}
	if json.Unmarshal([]byte(line), &raw) != nil {
		return dataset.News{}, fmt.Errorf("invalid JSON")
	}
	link, err := url.Parse(raw.URL)
	if err != nil || link.Host == "" || (link.Scheme != "http" && link.Scheme != "https") || len(raw.URL) > 4096 || strings.TrimSpace(raw.Title) == "" || len(raw.Title) > 4096 || len(raw.Type) == 0 || len(raw.Type) > 32 || len(raw.SourceName) > 256 || len(raw.Image) > 8192 {
		return dataset.News{}, fmt.Errorf("invalid article fields")
	}
	posted, err := time.Parse(time.RFC3339Nano, raw.PostedAt)
	if err != nil || posted.Year() < 1970 || posted.After(s.now.Add(5*time.Minute)) {
		return dataset.News{}, fmt.Errorf("invalid publication time")
	}
	fetched, err := time.Parse(time.RFC3339Nano, raw.FetchedAt)
	if err != nil || fetched.Year() < 1970 || fetched.After(s.now.Add(5*time.Minute)) || posted.After(fetched.Add(5*time.Minute)) {
		return dataset.News{}, fmt.Errorf("invalid fetch time")
	}
	authors := []string{}
	if len(raw.Author) > 0 && string(raw.Author) != "null" {
		var name string
		if json.Unmarshal(raw.Author, &name) == nil {
			if name != "" {
				authors = append(authors, name)
			}
		} else if json.Unmarshal(raw.Author, &authors) != nil {
			return dataset.News{}, fmt.Errorf("author must be a string, string array or null")
		}
	}
	if authors == nil {
		authors = []string{}
	}
	if len(raw.Coins) == 0 {
		return dataset.News{}, fmt.Errorf("missing matched coin IDs")
	}
	for _, coin := range raw.Coins {
		if !coinIDPattern.MatchString(coin) {
			return dataset.News{}, fmt.Errorf("invalid matched coin ID")
		}
	}
	return dataset.News{ID: identity.Digest("coingecko:news|" + raw.URL), URL: raw.URL, Title: raw.Title, PublishedAt: posted.UTC(), FetchedAt: fetched.UTC(), Type: raw.Type, SourceName: raw.SourceName, ImageURL: raw.Image, Authors: authors, CoinIDs: raw.Coins, RawJSON: line}, nil
}
