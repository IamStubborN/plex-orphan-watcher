package media

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHasVideoFindsVideoInsideExtras(t *testing.T) {
	dir := t.TempDir()
	extra := filepath.Join(dir, "Extras", "NCOP.mkv")
	if err := os.MkdirAll(filepath.Dir(extra), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(extra, []byte("fixture"), 0o644); err != nil {
		t.Fatal(err)
	}

	found, err := HasVideo(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("video inside Extras must block directory removal")
	}
}
