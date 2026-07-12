package plex

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"
)

const defaultPageSize = 200

type Client struct {
	baseURL  string
	token    string
	http     *http.Client
	pageSize int
}

type mediaContainerResponse struct {
	MediaContainer struct {
		TotalSize int `json:"totalSize"`
		Size      int `json:"size"`
		Directory []struct {
			Key  string `json:"key"`
			Type string `json:"type"`
		} `json:"Directory"`
		Metadata []struct {
			Media []struct {
				Part []struct {
					File string `json:"file"`
				} `json:"Part"`
			} `json:"Media"`
		} `json:"Metadata"`
	} `json:"MediaContainer"`
}

func New(baseURL, token string) *Client {
	return &Client{
		baseURL:  strings.TrimRight(baseURL, "/"),
		token:    strings.TrimSpace(token),
		http:     &http.Client{Timeout: 15 * time.Second},
		pageSize: defaultPageSize,
	}
}

func (client *Client) CountMediaUnder(ctx context.Context, directory string) (int, error) {
	sections, err := client.tvSections(ctx)
	if err != nil {
		return 0, err
	}
	if len(sections) == 0 {
		return 0, fmt.Errorf("Plex API returned no TV library sections")
	}

	directory = path.Clean(directory)
	count := 0
	for _, section := range sections {
		sectionCount, err := client.countSection(ctx, section, directory)
		if err != nil {
			return 0, err
		}
		count += sectionCount
	}
	return count, nil
}

func (client *Client) tvSections(ctx context.Context) ([]string, error) {
	var response mediaContainerResponse
	if err := client.get(ctx, "/library/sections", nil, &response); err != nil {
		return nil, fmt.Errorf("list Plex library sections: %w", err)
	}
	sections := make([]string, 0)
	for _, section := range response.MediaContainer.Directory {
		if section.Type == "show" && section.Key != "" {
			sections = append(sections, section.Key)
		}
	}
	return sections, nil
}

func (client *Client) countSection(ctx context.Context, section, directory string) (int, error) {
	count := 0
	for start := 0; ; start += client.pageSize {
		query := url.Values{
			"type":                   {"4"},
			"X-Plex-Container-Start": {strconv.Itoa(start)},
			"X-Plex-Container-Size":  {strconv.Itoa(client.pageSize)},
		}
		var response mediaContainerResponse
		endpoint := "/library/sections/" + url.PathEscape(section) + "/all"
		if err := client.get(ctx, endpoint, query, &response); err != nil {
			return 0, fmt.Errorf("list Plex TV section %q: %w", section, err)
		}

		for _, metadata := range response.MediaContainer.Metadata {
			for _, media := range metadata.Media {
				for _, part := range media.Part {
					file := path.Clean(part.File)
					if file == directory || strings.HasPrefix(file, directory+"/") {
						count++
					}
				}
			}
		}

		returned := len(response.MediaContainer.Metadata)
		if returned == 0 || returned < client.pageSize ||
			(response.MediaContainer.TotalSize > 0 && start+returned >= response.MediaContainer.TotalSize) {
			break
		}
	}
	return count, nil
}

func (client *Client) get(ctx context.Context, endpoint string, query url.Values, target any) error {
	requestURL := client.baseURL + endpoint
	if len(query) > 0 {
		requestURL += "?" + query.Encode()
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("X-Plex-Token", client.token)
	request.Header.Set("X-Plex-Client-Identifier", "plex-orphan-watcher")
	request.Header.Set("X-Plex-Pms-Api-Version", "1.0.0")

	response, err := client.http.Do(request)
	if err != nil {
		return fmt.Errorf("request Plex API: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 1024))
		return fmt.Errorf("Plex API returned %s: %s", response.Status, strings.TrimSpace(string(body)))
	}
	if err := json.NewDecoder(response.Body).Decode(target); err != nil {
		return fmt.Errorf("decode Plex response: %w", err)
	}
	return nil
}
