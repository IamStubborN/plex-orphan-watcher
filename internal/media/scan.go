package media

import (
	"io/fs"
	"path/filepath"
	"strings"
)

var videoExtensions = map[string]struct{}{
	".avi": {}, ".m4v": {}, ".mkv": {}, ".mov": {}, ".mp4": {}, ".ts": {},
}

var ignoredDirectoryNames = map[string]struct{}{
	"behind the scenes": {},
	"bonus":             {},
	"bonus disc":        {},
	"deleted scenes":    {},
	"extra":             {},
	"extras":            {},
	"featurette":        {},
	"featurettes":       {},
	"interviews":        {},
	"nc":                {},
	"other":             {},
	"sample":            {},
	"samples":           {},
	"scenes":            {},
	"shorts":            {},
	"trailer":           {},
	"trailers":          {},
}

// HasPrimaryVideo reports whether dir contains a video outside known extras
// directories. Such a video blocks deletion even when Plex does not index it.
func HasPrimaryVideo(dir string) (bool, error) {
	found := false
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path != dir {
				if _, ignored := ignoredDirectoryNames[strings.ToLower(entry.Name())]; ignored {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if _, video := videoExtensions[strings.ToLower(filepath.Ext(entry.Name()))]; video {
			found = true
			return fs.SkipAll
		}
		return nil
	})
	return found, err
}
