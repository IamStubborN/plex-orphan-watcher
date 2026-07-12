package qbittorrent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"path/filepath"
	"strings"
	"time"
)

type Client struct {
	baseURL  string
	username string
	password string
	http     *http.Client
}

type torrent struct {
	Name        string `json:"name"`
	SavePath    string `json:"save_path"`
	ContentPath string `json:"content_path"`
}

func New(baseURL, username, password string) *Client {
	jar, _ := cookiejar.New(nil)
	return &Client{
		baseURL:  strings.TrimRight(baseURL, "/"),
		username: username,
		password: password,
		http: &http.Client{
			Jar:     jar,
			Timeout: 10 * time.Second,
		},
	}
}

func (client *Client) Managed(ctx context.Context, candidate string) (bool, error) {
	if client.username != "" || client.password != "" {
		if err := client.login(ctx); err != nil {
			return false, err
		}
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, client.baseURL+"/api/v2/torrents/info", nil)
	if err != nil {
		return false, fmt.Errorf("create qBittorrent request: %w", err)
	}
	response, err := client.http.Do(request)
	if err != nil {
		return false, fmt.Errorf("query qBittorrent: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return false, fmt.Errorf("query qBittorrent: unexpected HTTP status %s", response.Status)
	}

	var torrents []torrent
	if err := json.NewDecoder(response.Body).Decode(&torrents); err != nil {
		return false, fmt.Errorf("decode qBittorrent response: %w", err)
	}
	candidate = filepath.Clean(candidate)
	for _, item := range torrents {
		contentPath := item.ContentPath
		if contentPath == "" && item.SavePath != "" && item.Name != "" {
			contentPath = filepath.Join(item.SavePath, item.Name)
		}
		if contentPath != "" && pathsOverlap(candidate, filepath.Clean(contentPath)) {
			return true, nil
		}
	}
	return false, nil
}

func (client *Client) login(ctx context.Context) error {
	form := url.Values{"username": {client.username}, "password": {client.password}}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, client.baseURL+"/api/v2/auth/login", strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("create qBittorrent login request: %w", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := client.http.Do(request)
	if err != nil {
		return fmt.Errorf("log in to qBittorrent: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("log in to qBittorrent: unexpected HTTP status %s", response.Status)
	}
	return nil
}

func pathsOverlap(left, right string) bool {
	if left == right {
		return true
	}
	separator := string(filepath.Separator)
	return strings.HasPrefix(left, right+separator) || strings.HasPrefix(right, left+separator)
}
