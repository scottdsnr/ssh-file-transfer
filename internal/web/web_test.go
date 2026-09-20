package web

import (
	"archive/zip"
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testHandler(t *testing.T) (*Handler, []byte) {
	t.Helper()
	dir := t.TempDir()
	body := bytes.Repeat([]byte("abcdefgh"), 1000)
	if err := os.WriteFile(filepath.Join(dir, "a.bin"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	return New("1234-cobalt-badger-orbit", []File{
		{Name: "a.bin", Source: filepath.Join(dir, "a.bin"), Size: int64(len(body))},
		{Name: "nested/b.txt", Source: filepath.Join(dir, "b.txt"), Size: 5},
	}), body
}

func get(h *Handler, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	return w
}

func TestIndexListsEveryFile(t *testing.T) {
	h, _ := testHandler(t)
	w := get(h, h.Prefix())
	if w.Code != http.StatusOK {
		t.Fatalf("index status = %d", w.Code)
	}
	page := w.Body.String()
	for _, want := range []string{"a.bin", "nested/b.txt", "all.zip"} {
		if !strings.Contains(page, want) {
			t.Errorf("index is missing %q", want)
		}
	}
}

func TestFileDownloadServesTheBytes(t *testing.T) {
	h, body := testHandler(t)
	w := get(h, h.Prefix()+"/f/0/a.bin")
	if w.Code != http.StatusOK {
		t.Fatalf("download status = %d", w.Code)
	}
	if !bytes.Equal(w.Body.Bytes(), body) {
		t.Errorf("download returned %d bytes, want %d", w.Body.Len(), len(body))
	}
	if cd := w.Header().Get("Content-Disposition"); !strings.Contains(cd, `"a.bin"`) {
		t.Errorf("Content-Disposition = %q", cd)
	}
}

// A dropped download has to be resumable, or a big file over a flaky link
// never finishes.
func TestRangeRequestResumes(t *testing.T) {
	h, body := testHandler(t)
	r := httptest.NewRequest(http.MethodGet, h.Prefix()+"/f/0/a.bin", nil)
	r.Header.Set("Range", "bytes=100-199")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusPartialContent {
		t.Fatalf("range status = %d, want 206", w.Code)
	}
	if !bytes.Equal(w.Body.Bytes(), body[100:200]) {
		t.Errorf("range returned the wrong bytes")
	}
}

func TestZipContainsEveryFileUnderItsManifestPath(t *testing.T) {
	h, body := testHandler(t)
	w := get(h, h.Prefix()+"/all.zip")
	if w.Code != http.StatusOK {
		t.Fatalf("zip status = %d", w.Code)
	}
	zr, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		content, _ := io.ReadAll(rc)
		rc.Close()
		got[f.Name] = len(content)
	}
	if got["a.bin"] != len(body) || got["nested/b.txt"] != 5 {
		t.Errorf("zip entries = %v", got)
	}
}

// The code is the only secret protecting the link, so a wrong one must look
// exactly like a hostname with nothing behind it.
func TestWrongCodeIsIndistinguishableFromNothing(t *testing.T) {
	h, _ := testHandler(t)
	for _, path := range []string{"/w/1234-wrong-badger-orbit", "/w/1234-wrong-badger-orbit/f/0/a.bin", "/", "/w/"} {
		if w := get(h, path); w.Code != http.StatusNotFound {
			t.Errorf("%s status = %d, want 404", path, w.Code)
		}
	}
}

func TestUnknownFileIndexIsNotFound(t *testing.T) {
	h, _ := testHandler(t)
	for _, path := range []string{"/f/9/a.bin", "/f/-1/a.bin", "/f/x/a.bin", "/other"} {
		if w := get(h, h.Prefix()+path); w.Code != http.StatusNotFound {
			t.Errorf("%s status = %d, want 404", path, w.Code)
		}
	}
}

func TestOnDownloadReportsFinishedFiles(t *testing.T) {
	h, _ := testHandler(t)
	var seen []string
	h.OnDownload = func(name string) { seen = append(seen, name) }
	get(h, h.Prefix()+"/f/1/b.txt")
	get(h, h.Prefix()+"/all.zip")
	if len(seen) != 2 || seen[0] != "nested/b.txt" || seen[1] != "all.zip" {
		t.Errorf("OnDownload saw %v", seen)
	}
}
