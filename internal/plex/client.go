package plex

import (
	"bufio"
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

	"github.com/IamStubborN/plex-orphan-watcher/internal/model"
)

const defaultPageSize = 200

type Client struct {
	baseURL    string
	token      string
	http       *http.Client
	streamHTTP *http.Client
	pageSize   int
}

type DeletionEvent struct {
	RatingKey string
}

type flexibleID string

func (id *flexibleID) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*id = ""
		return nil
	}
	var value string
	if len(data) > 0 && data[0] == '"' {
		if err := json.Unmarshal(data, &value); err != nil {
			return err
		}
	} else {
		value = string(data)
	}
	*id = flexibleID(strings.TrimSpace(value))
	return nil
}

type sectionResponse struct {
	MediaContainer struct {
		Directory []struct {
			Key      string `json:"key"`
			Type     string `json:"type"`
			Location []struct {
				Path string `json:"path"`
			} `json:"Location"`
		} `json:"Directory"`
	} `json:"MediaContainer"`
}

type itemsResponse struct {
	MediaContainer struct {
		TotalSize int `json:"totalSize"`
		Metadata  []struct {
			RatingKey            string `json:"ratingKey"`
			Type                 string `json:"type"`
			Title                string `json:"title"`
			ParentRatingKey      string `json:"parentRatingKey"`
			ParentTitle          string `json:"parentTitle"`
			GrandparentRatingKey string `json:"grandparentRatingKey"`
			GrandparentTitle     string `json:"grandparentTitle"`
			Media                []struct {
				Part []struct {
					File string `json:"file"`
				} `json:"Part"`
			} `json:"Media"`
		} `json:"Metadata"`
	} `json:"MediaContainer"`
}

func New(baseURL, token string) *Client {
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		token:      strings.TrimSpace(token),
		http:       &http.Client{Timeout: 30 * time.Second},
		streamHTTP: &http.Client{},
		pageSize:   defaultPageSize,
	}
}

func (client *Client) Snapshot(ctx context.Context) (model.Snapshot, error) {
	var sections sectionResponse
	if err := client.get(ctx, "/library/sections", nil, &sections); err != nil {
		return model.Snapshot{}, fmt.Errorf("list Plex library sections: %w", err)
	}
	snapshot := model.Snapshot{Items: make(map[string]model.Item), SyncedAt: time.Now().UTC()}
	for _, section := range sections.MediaContainer.Directory {
		itemType, plexType, ok := sectionKind(section.Type)
		if !ok {
			continue
		}
		for _, location := range section.Location {
			if location.Path != "" {
				snapshot.Locations = append(snapshot.Locations, model.LibraryLocation{
					SectionID: section.Key, SectionType: itemType, Path: path.Clean(location.Path),
				})
			}
		}
		if err := client.loadSection(ctx, section.Key, itemType, plexType, snapshot.Items); err != nil {
			return model.Snapshot{}, err
		}
	}
	return snapshot, nil
}

func sectionKind(sectionType string) (model.ItemType, string, bool) {
	switch sectionType {
	case "movie":
		return model.ItemMovie, "1", true
	case "show":
		return model.ItemEpisode, "4", true
	default:
		return "", "", false
	}
}

func (client *Client) loadSection(ctx context.Context, section string, itemType model.ItemType, plexType string, target map[string]model.Item) error {
	for start := 0; ; {
		query := url.Values{
			"type":                   {plexType},
			"X-Plex-Container-Start": {strconv.Itoa(start)},
			"X-Plex-Container-Size":  {strconv.Itoa(client.pageSize)},
		}
		var response itemsResponse
		endpoint := "/library/sections/" + url.PathEscape(section) + "/all"
		if err := client.get(ctx, endpoint, query, &response); err != nil {
			return fmt.Errorf("list Plex section %q: %w", section, err)
		}
		for _, metadata := range response.MediaContainer.Metadata {
			if metadata.RatingKey == "" {
				continue
			}
			item := model.Item{
				RatingKey: metadata.RatingKey, Type: itemType, SectionID: section, Title: metadata.Title,
				ParentRatingKey: metadata.ParentRatingKey, ParentTitle: metadata.ParentTitle,
				GrandparentRatingKey: metadata.GrandparentRatingKey, GrandparentTitle: metadata.GrandparentTitle,
			}
			for _, media := range metadata.Media {
				for _, part := range media.Part {
					if part.File != "" {
						item.Parts = append(item.Parts, path.Clean(part.File))
					}
				}
			}
			target[item.RatingKey] = item
		}
		returned := len(response.MediaContainer.Metadata)
		if returned == 0 || (response.MediaContainer.TotalSize > 0 && start+returned >= response.MediaContainer.TotalSize) {
			return nil
		}
		start += returned
	}
}

func (client *Client) StreamEvents(ctx context.Context, handler func(DeletionEvent)) error {
	request, err := client.request(ctx, http.MethodGet, "/:/eventsource/notifications", nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "text/event-stream")
	response, err := client.streamHTTP.Do(request)
	if err != nil {
		return fmt.Errorf("connect Plex EventSource: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 1024))
		return fmt.Errorf("plex EventSource returned %s: %s", response.Status, strings.TrimSpace(string(body)))
	}

	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		var notification struct {
			NotificationContainer struct {
				Type     string `json:"type"`
				Timeline []struct {
					State      int        `json:"state"`
					RatingKey  flexibleID `json:"ratingKey"`
					ItemID     flexibleID `json:"itemID"`
					Identifier string     `json:"identifier"`
				} `json:"TimelineEntry"`
			} `json:"NotificationContainer"`
		}
		if err := json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &notification); err != nil {
			return fmt.Errorf("decode Plex EventSource notification: %w", err)
		}
		if notification.NotificationContainer.Type != "timeline" {
			continue
		}
		for _, entry := range notification.NotificationContainer.Timeline {
			if entry.State != 9 || entry.Identifier != "com.plexapp.plugins.library" {
				continue
			}
			ratingKey := string(entry.RatingKey)
			if ratingKey == "" {
				ratingKey = string(entry.ItemID)
			}
			if ratingKey != "" {
				handler(DeletionEvent{RatingKey: ratingKey})
			}
		}
	}
	if err := scanner.Err(); err != nil && ctx.Err() == nil {
		return fmt.Errorf("read Plex EventSource: %w", err)
	}
	return nil
}

func (client *Client) Ready(ctx context.Context) error {
	var identity map[string]any
	return client.get(ctx, "/identity", nil, &identity)
}

func (client *Client) get(ctx context.Context, endpoint string, query url.Values, target any) error {
	request, err := client.request(ctx, http.MethodGet, endpoint, query)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	response, err := client.http.Do(request)
	if err != nil {
		return fmt.Errorf("request Plex API: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 1024))
		return fmt.Errorf("plex API returned %s: %s", response.Status, strings.TrimSpace(string(body)))
	}
	if err := json.NewDecoder(response.Body).Decode(target); err != nil {
		return fmt.Errorf("decode Plex response: %w", err)
	}
	return nil
}

func (client *Client) request(ctx context.Context, method, endpoint string, query url.Values) (*http.Request, error) {
	requestURL := client.baseURL + endpoint
	if len(query) > 0 {
		requestURL += "?" + query.Encode()
	}
	request, err := http.NewRequestWithContext(ctx, method, requestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create Plex request: %w", err)
	}
	request.Header.Set("X-Plex-Token", client.token)
	request.Header.Set("X-Plex-Client-Identifier", "plex-orphan-watcher")
	request.Header.Set("X-Plex-Pms-Api-Version", "1.0.0")
	return request, nil
}
