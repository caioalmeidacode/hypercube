package main

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const userAgent = "Hypercube/" + appVersion + " (minecraft launcher)"

var httpClient = &http.Client{
	Timeout: 0,
	Transport: &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		MaxIdleConnsPerHost:   16,
		IdleConnTimeout:       60 * time.Second,
		TLSHandshakeTimeout:   20 * time.Second,
		ResponseHeaderTimeout: 40 * time.Second,
	},
}

type NetError struct {
	Host    string
	Timeout bool
}

func (e *NetError) Error() string {
	if e.Timeout {
		return "tempo esgotado ao conectar em " + e.Host
	}
	return "sem conexão com " + e.Host
}

type HTTPError struct {
	Status int
	URL    string
	Body   string
}

func (e *HTTPError) Error() string {
	host := e.URL
	if u, err := url.Parse(e.URL); err == nil {
		host = u.Host
	}
	switch e.Status {
	case 401, 403:
		return fmt.Sprintf("acesso negado por %s (HTTP %d)", host, e.Status)
	case 404:
		return fmt.Sprintf("não encontrado em %s", host)
	case 429:
		return fmt.Sprintf("muitas requisições para %s, tente em instantes", host)
	}
	return fmt.Sprintf("HTTP %d em %s", e.Status, host)
}

func doRequest(method, url string, body any, headers map[string]string) ([]byte, error) {
	var rd io.Reader
	if body != nil {
		switch b := body.(type) {
		case []byte:
			rd = bytes.NewReader(b)
		case string:
			rd = strings.NewReader(b)
		default:
			j, err := json.Marshal(body)
			if err != nil {
				return nil, err
			}
			rd = bytes.NewReader(j)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, url, rd)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		if _, isStr := body.(string); !isStr {
			req.Header.Set("Content-Type", "application/json")
		}
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, &NetError{Host: req.URL.Host, Timeout: ctx.Err() != nil}
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return data, &HTTPError{Status: resp.StatusCode, URL: url, Body: string(data)}
	}
	return data, nil
}

func getJSON(url string, v any, headers ...map[string]string) error {
	var h map[string]string
	if len(headers) > 0 {
		h = headers[0]
	}
	data, err := doRequest("GET", url, nil, h)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

func postJSON(url string, body any, v any, headers ...map[string]string) error {
	var h map[string]string
	if len(headers) > 0 {
		h = headers[0]
	}
	data, err := doRequest("POST", url, body, h)
	if err != nil {
		return err
	}
	if v == nil {
		return nil
	}
	return json.Unmarshal(data, v)
}

func cachedJSON(url, cacheName string, maxAge time.Duration, v any) error {
	path := store.CacheDir("meta", cacheName)
	if st, err := os.Stat(path); err == nil && time.Since(st.ModTime()) < maxAge {
		if readJSON(path, v) == nil {
			return nil
		}
	}
	data, err := doRequest("GET", url, nil, nil)
	if err != nil {
		if readJSON(path, v) == nil {
			return nil
		}
		return err
	}
	if err := json.Unmarshal(data, v); err != nil {
		return err
	}
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, data, 0o644)
	return nil
}

func fileSHA1(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha1.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

type DL struct {
	URL  string
	Path string
	SHA1 string
	Size int64

	Optional bool
}

func (d DL) needsDownload() bool {
	st, err := os.Stat(d.Path)
	if err != nil || st.IsDir() {
		return true
	}
	if d.Size > 0 && st.Size() != d.Size {
		return true
	}
	if d.SHA1 != "" && d.Size <= 0 {
		h, err := fileSHA1(d.Path)
		return err != nil || !strings.EqualFold(h, d.SHA1)
	}
	return st.Size() == 0
}

func downloadFile(d DL, onBytes func(int64)) error {
	var lastErr error
	for attempt := 0; attempt < 4; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt*attempt) * 600 * time.Millisecond)
		}
		lastErr = downloadOnce(d, onBytes)
		if lastErr == nil {
			return nil
		}
		var he *HTTPError
		if errors.As(lastErr, &he) && he.Status == 404 {
			return lastErr
		}
	}
	return lastErr
}

func downloadOnce(d DL, onBytes func(int64)) error {
	if err := os.MkdirAll(filepath.Dir(d.Path), 0o755); err != nil {
		return err
	}
	req, err := http.NewRequest("GET", d.URL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("sem conexão com %s", req.URL.Host)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return &HTTPError{Status: resp.StatusCode, URL: d.URL}
	}
	tmp := d.Path + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	h := sha1.New()
	w := io.MultiWriter(f, h)
	buf := make([]byte, 64*1024)
	var written int64
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				f.Close()
				os.Remove(tmp)
				return werr
			}
			written += int64(n)
			if onBytes != nil {
				onBytes(int64(n))
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			f.Close()
			os.Remove(tmp)
			if onBytes != nil {
				onBytes(-written)
			}
			return rerr
		}
	}
	f.Close()
	if d.SHA1 != "" && !strings.EqualFold(hex.EncodeToString(h.Sum(nil)), d.SHA1) {
		os.Remove(tmp)
		if onBytes != nil {
			onBytes(-written)
		}
		return fmt.Errorf("checksum inválido: %s", filepath.Base(d.Path))
	}
	os.Remove(d.Path)
	return os.Rename(tmp, d.Path)
}

func downloadAll(t *Task, label string, items []DL) error {
	var pending []DL
	seen := map[string]bool{}
	for _, d := range items {
		if d.URL == "" || seen[d.Path] {
			continue
		}
		seen[d.Path] = true
		if d.needsDownload() {
			pending = append(pending, d)
		}
	}
	if len(pending) == 0 {
		return nil
	}
	var total int64
	for _, d := range pending {
		total += d.Size
	}
	if t != nil {
		if total > 0 {
			t.Stage(fmt.Sprintf("%s · %d arquivos", label, len(pending)), total)
		} else {
			t.Stage(fmt.Sprintf("%s · %d arquivos", label, len(pending)), int64(len(pending)))
		}
	}
	workers := store.Settings().Concurrency
	if workers > len(pending) {
		workers = len(pending)
	}
	jobs := make(chan DL)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for d := range jobs {
				var cb func(int64)
				if t != nil && total > 0 {
					cb = t.Add
				}
				err := downloadFile(d, cb)
				if t != nil && total <= 0 {
					t.Add(1)
				}
				if err != nil && !d.Optional {
					mu.Lock()
					if firstErr == nil {
						firstErr = fmt.Errorf("%s: %w", filepath.Base(d.Path), err)
					}
					mu.Unlock()
				}
			}
		}()
	}
	for _, d := range pending {
		mu.Lock()
		stop := firstErr != nil
		mu.Unlock()
		if stop {
			break
		}
		jobs <- d
	}
	close(jobs)
	wg.Wait()
	return firstErr
}
