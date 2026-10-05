// Package metadataproviders implements only fixed official book APIs. It does
// not accept remote URLs, follow redirects, download covers or retry failures.
package metadataproviders

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	search "github.com/ryancheng/telegram-downloader/internal/app/metadatasearch"
	"github.com/ryancheng/telegram-downloader/internal/app/sourcesettings"
	"github.com/ryancheng/telegram-downloader/internal/credentials"
)

const maxBody = 2 * 1024 * 1024
const userAgent = "ryancheng/telegram-downloader/1.0 (https://github.com/ryancheng/telegram-downloader)"

type cacheEntry struct {
	records []search.Record
	expires time.Time
}
type sourceState struct {
	busy                               chan struct{}
	nextRead, timeSearch, blockedUntil time.Time
}
type Catalog struct {
	client           *http.Client
	mu               sync.Mutex
	cache            map[[32]byte]cacheEntry
	states           map[string]*sourceState
	intervalOverride *time.Duration
}

func New() *Catalog {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxConnsPerHost = 1
	transport.ResponseHeaderTimeout = 10 * time.Second
	return newCatalog(transport)
}
func newCatalog(transport http.RoundTripper) *Catalog {
	c := &Catalog{client: &http.Client{Transport: transport, Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, cache: map[[32]byte]cacheEntry{}, states: map[string]*sourceState{}}
	for _, id := range []string{"mangabaka", "mangaupdates", "bangumi"} {
		c.states[id] = &sourceState{busy: make(chan struct{}, 1)}
	}
	return c
}
func (*Catalog) Descriptors() []sourcesettings.Descriptor {
	result := []sourcesettings.Descriptor{}
	for _, entry := range [][2]string{{"mangabaka", "MangaBaka"}, {"mangaupdates", "MangaUpdates"}, {"bangumi", "Bangumi"}} {
		auth := []string{"none"}
		if entry[0] == "bangumi" {
			auth = append(auth, "bearer")
		}
		result = append(result, sourcesettings.Descriptor{ID: entry[0], Label: entry[1], AuthModes: auth, Filters: map[string][]string{}, FieldPreferences: map[string][]string{}, Capabilities: []string{"title_search", "details", "aliases", "creators", "classification", "custom_mapping"}})
	}
	return result
}
func (*Catalog) Info(id string) search.ProviderInfo {
	info := search.ProviderInfo{Visibility: "unknown", CustomFields: []search.Mapping{{Key: "source.type", Label: "来源作品类型", Type: "string"}}}
	switch id {
	case "mangabaka":
		info.Attribution = search.Attribution{Label: "MangaBaka", URL: "https://mangabaka.org", License: "自有字段 CC BY-NC-SA 4.0；未复制 source 下的第三方字段", LicenseURL: "https://mangabaka.org/about/data-license"}
		info.CustomFields = append(info.CustomFields, search.Mapping{Key: "source.status", Label: "来源连载状态", Type: "string"}, search.Mapping{Key: "source.content_rating", Label: "来源内容分级（非年龄推断）", Type: "string"})
	case "mangaupdates":
		info.Attribution = search.Attribution{Label: "MangaUpdates", URL: "https://www.mangaupdates.com", License: "MangaUpdates API 使用条款；保留来源署名", LicenseURL: "https://api.mangaupdates.com/"}
	case "bangumi":
		info.Attribution = search.Attribution{Label: "Bangumi", URL: "https://bgm.tv", License: "条目信息 CC BY-SA 3.0；其他素材不在此许可内", LicenseURL: "https://bgm.tv/about/copyright"}
		info.Visibility = "permission_dependent"
		info.CustomFields = append(info.CustomFields, search.Mapping{Key: "source.nsfw", Label: "来源 NSFW 标记", Type: "boolean"}, search.Mapping{Key: "source.series", Label: "是否系列条目", Type: "boolean"})
	}
	return info
}
func (c *Catalog) Test(ctx context.Context, id string, config sourcesettings.PublicSourceConfig, secret credentials.Secret) (sourcesettings.ProbeResult, error) {
	if id != config.ProviderID {
		return sourcesettings.ProbeResult{}, &search.Error{Code: "unavailable"}
	}
	_, err := c.Search(ctx, config, secret, "Naruto")
	if err != nil {
		return sourcesettings.ProbeResult{}, err
	}
	return sourcesettings.ProbeResult{Status: "passed", Scope: "public_catalog_only"}, nil
}
func (c *Catalog) cached(ctx context.Context, config sourcesettings.PublicSourceConfig, secret credentials.Secret, op, value string, load func() ([]search.Record, error)) ([]search.Record, error) {
	state, ok := c.states[config.ProviderID]
	if !ok {
		return nil, &search.Error{Code: "unavailable"}
	}
	keyBytes, _ := json.Marshal([]any{config.ProviderID, config.ConfigVersion, config.AuthMode, config.Filters, config.FieldPreferences, secret.Value(), op, value})
	key := sha256.Sum256(keyBytes)
	c.mu.Lock()
	entry, hit := c.cache[key]
	c.mu.Unlock()
	if hit && time.Now().Before(entry.expires) {
		return entry.records, nil
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case state.busy <- struct{}{}:
		defer func() { <-state.busy }()
	default:
		return nil, &search.Error{Code: "rate_limited", RetryAfter: 2}
	}
	c.mu.Lock()
	blocked := time.Until(state.blockedUntil)
	c.mu.Unlock()
	if blocked > 0 {
		return nil, &search.Error{Code: "rate_limited", RetryAfter: int(blocked.Seconds()) + 1}
	}
	result, err := load()
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.cache) >= 64 {
		var oldest [32]byte
		var stamp time.Time
		for k, v := range c.cache {
			if stamp.IsZero() || v.expires.Before(stamp) {
				oldest = k
				stamp = v.expires
			}
		}
		delete(c.cache, oldest)
	}
	c.cache[key] = cacheEntry{result, time.Now().Add(5 * time.Minute)}
	return result, nil
}
func (c *Catalog) request(ctx context.Context, config sourcesettings.PublicSourceConfig, secret credentials.Secret, method, path string, body any, output any) error {
	state := c.states[config.ProviderID]
	if state == nil {
		return &search.Error{Code: "unavailable"}
	}
	origin := ""
	switch config.ProviderID {
	case "mangabaka":
		origin = "https://api.mangabaka.org"
	case "mangaupdates":
		origin = "https://api.mangaupdates.com"
	case "bangumi":
		origin = "https://api.bgm.tv"
	}
	interval := time.Second
	if config.ProviderID == "mangabaka" {
		interval = time.Minute / 180
	}
	if c.intervalOverride != nil {
		interval = *c.intervalOverride
	}
	c.mu.Lock()
	next := state.nextRead
	isSearch := config.ProviderID == "mangabaka" && len(path) >= 17 && path[:17] == "/v1/series/search"
	if isSearch && state.timeSearch.After(next) {
		next = state.timeSearch
	}
	c.mu.Unlock()
	if wait := time.Until(next); wait > 0 {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}
	c.mu.Lock()
	state.nextRead = time.Now().Add(interval)
	if isSearch {
		gap := 2 * time.Second
		if c.intervalOverride != nil {
			gap = *c.intervalOverride
		}
		state.timeSearch = time.Now().Add(gap)
	}
	c.mu.Unlock()
	var data []byte
	if body != nil {
		data, _ = json.Marshal(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, origin+path, bytes.NewReader(data))
	if err != nil {
		return &search.Error{Code: "invalid_response"}
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if config.ProviderID == "bangumi" && config.AuthMode == "bearer" && secret.Value() != "" {
		req.Header.Set("Authorization", "Bearer "+secret.Value())
	}
	response, err := c.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var timeout interface{ Timeout() bool }
		if errors.As(err, &timeout) && timeout.Timeout() {
			return &search.Error{Code: "timeout"}
		}
		return &search.Error{Code: "unavailable"}
	}
	defer response.Body.Close()
	code := ""
	switch {
	case response.StatusCode == 401:
		code = "auth_invalid"
		if config.AuthMode == "none" {
			code = "auth_required"
		}
	case response.StatusCode == 403:
		code = "permission_denied"
	case response.StatusCode == 404:
		code = "no_results"
	case response.StatusCode == 429:
		retry := 60 * time.Second
		if seconds, e := strconv.Atoi(response.Header.Get("Retry-After")); e == nil && seconds > 0 && seconds <= 86400 {
			retry = time.Duration(seconds) * time.Second
		} else if until, e := http.ParseTime(response.Header.Get("Retry-After")); e == nil && time.Until(until) > 0 {
			retry = time.Until(until)
			if retry > 24*time.Hour {
				retry = 24 * time.Hour
			}
		}
		c.mu.Lock()
		state.blockedUntil = time.Now().Add(retry)
		c.mu.Unlock()
		return &search.Error{Code: "rate_limited", RetryAfter: int(retry.Seconds()) + 1}
	case response.StatusCode >= 500:
		code = "unavailable"
	case response.StatusCode != 200:
		code = "invalid_response"
	}
	if code != "" {
		return &search.Error{Code: code}
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxBody+1))
	if err != nil {
		return &search.Error{Code: "unavailable"}
	}
	if len(raw) > maxBody || len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, output) != nil {
		return &search.Error{Code: "invalid_response"}
	}
	return nil
}
