package pathguard

import (
	"path/filepath"
	"strings"

	"github.com/IamStubborN/plex-orphan-watcher/internal/quarantine"
)

// CandidateFromEvent resolves an event path to the direct child of root that
// owns it. Paths at or outside root are rejected.
func CandidateFromEvent(root, eventPath string) (string, bool) {
	if quarantine.Contains(eventPath) {
		return "", false
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", false
	}
	eventAbs, err := filepath.Abs(eventPath)
	if err != nil {
		return "", false
	}
	rel, err := filepath.Rel(rootAbs, eventAbs)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	first, _, _ := strings.Cut(rel, string(filepath.Separator))
	if first == "" || first == "." || first == ".." {
		return "", false
	}
	return filepath.Join(rootAbs, first), true
}
