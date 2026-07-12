package media

import (
	"io/fs"
	"path/filepath"
	"strings"
)

var videoExtensions = map[string]struct{}{
	".avi": {}, ".m4v": {}, ".mkv": {}, ".mov": {}, ".mp4": {}, ".ts": {},
}

// HasVideo reports whether dir contains any video file, including extras.
func HasVideo(dir string) (bool, error) {
	found := false
	err := filepath.WalkDir(dir, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
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
