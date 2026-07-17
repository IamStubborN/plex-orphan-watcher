package model

import "time"

type ItemType string

const (
	ItemMovie   ItemType = "movie"
	ItemEpisode ItemType = "episode"
)

type Item struct {
	RatingKey            string   `json:"rating_key"`
	Type                 ItemType `json:"type"`
	SectionID            string   `json:"section_id"`
	Title                string   `json:"title"`
	ParentRatingKey      string   `json:"parent_rating_key,omitempty"`
	ParentTitle          string   `json:"parent_title,omitempty"`
	GrandparentRatingKey string   `json:"grandparent_rating_key,omitempty"`
	GrandparentTitle     string   `json:"grandparent_title,omitempty"`
	Parts                []string `json:"parts"`
}

type LibraryLocation struct {
	SectionID   string   `json:"section_id"`
	SectionType ItemType `json:"section_type"`
	Path        string   `json:"path"`
}

type Snapshot struct {
	Items     map[string]Item   `json:"items"`
	Locations []LibraryLocation `json:"locations"`
	SyncedAt  time.Time         `json:"synced_at"`
}

type PendingStatus string

const (
	PendingWaiting PendingStatus = "waiting"
	PendingDryRun  PendingStatus = "dry_run"
)

type Pending struct {
	ID         string        `json:"id"`
	Item       Item          `json:"item"`
	Trigger    string        `json:"trigger"`
	ObservedAt time.Time     `json:"observed_at"`
	DueAt      time.Time     `json:"due_at"`
	Status     PendingStatus `json:"status"`
	Plan       DeletionPlan  `json:"plan,omitempty"`
}

type ActionType string

const (
	ActionDeleteFile ActionType = "delete_file"
	ActionDeleteTree ActionType = "delete_tree"
)

type RootPolicy string

const (
	PolicyDelete RootPolicy = "delete"
	PolicyAudit  RootPolicy = "audit"
)

type Action struct {
	Type       ActionType `json:"type"`
	Path       string     `json:"path"`
	Bytes      int64      `json:"bytes"`
	RootPolicy RootPolicy `json:"root_policy"`
	Entries    []Entry    `json:"entries,omitempty"`
}

type Entry struct {
	Path            string `json:"path"`
	Size            int64  `json:"size"`
	ModTimeUnixNano int64  `json:"mod_time_unix_nano"`
	Mode            uint32 `json:"mode"`
	IsDir           bool   `json:"is_dir"`
}

type DeletionPlan struct {
	PendingID    string    `json:"pending_id"`
	RatingKey    string    `json:"rating_key"`
	ItemType     ItemType  `json:"item_type"`
	Title        string    `json:"title"`
	OldMediaPath string    `json:"old_media_path,omitempty"`
	Reason       string    `json:"reason"`
	Actions      []Action  `json:"actions"`
	CreatedAt    time.Time `json:"created_at"`
}
