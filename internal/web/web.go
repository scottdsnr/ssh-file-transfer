// Package web serves the files of a send over plain HTTP, so a recipient with
// nothing but a browser can still fetch them. It is mounted on the same
// embedded server as the relay, which keeps the transfer serverless: the
// sending machine is the only thing hosting anything.
//
// This path cannot be end to end encrypted the way the CLI-to-CLI path is: a
// browser has no code to run the PAKE with, so the bytes are whatever the
// transport makes them. Over a LAN (--direct) nothing sits in the middle; over
// a Cloudflare quick tunnel (--tunnel) the link is HTTPS but Cloudflare
// terminates the TLS and can see the plaintext. Callers are expected to say so.
package web

import (
	"archive/zip"
	"crypto/subtle"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
)

// File is one offered file: the name the browser should save it as and the
// path it is read from on this machine.
type File struct {
	Name   string
	Source string
	Size   int64
}

// Handler serves the index page and the file bytes under a code-derived
// prefix. Everything else 404s, including a wrong code, so probing the tunnel
// hostname reveals nothing.
type Handler struct {
	code  string
	files []File

	// OnDownload is called when a browser finishes (or abandons) a download.
	OnDownload func(name string)
}

// New builds a handler offering files, reachable only under the code.
func New(code string, files []File) *Handler {
	return &Handler{code: code, files: files}
}

// Prefix is the URL path the handler answers on. It contains the whole code,
// which is what stands in for a password here.
func (h *Handler) Prefix() string { return "/w/" + h.code }

// TotalSize is the number of bytes on offer.
func (h *Handler) TotalSize() int64 {
	var n int64
	for _, f := range h.files {
		n += f.Size
	}
	return n
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rest, ok := h.strip(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}
	switch {
	case rest == "" || rest == "/":
		h.index(w, r)
	case rest == "/all.zip":
		h.zipAll(w, r)
	case strings.HasPrefix(rest, "/f/"):
		h.file(w, r, strings.TrimPrefix(rest, "/f/"))
	default:
		http.NotFound(w, r)
	}
}

// strip matches the code prefix in constant time and returns what follows it.
func (h *Handler) strip(urlPath string) (string, bool) {
	const root = "/w/"
	if !strings.HasPrefix(urlPath, root) {
		return "", false
	}
	rest := urlPath[len(root):]
	code, tail, _ := strings.Cut(rest, "/")
	if subtle.ConstantTimeCompare([]byte(code), []byte(h.code)) != 1 {
		return "", false
	}
	if tail == "" && !strings.Contains(rest, "/") {
		return "", true
	}
	return "/" + tail, true
}

// file serves one file by "<index>/<name>"; the index is what identifies it,
// and the name is there so the browser saves something sensible.
func (h *Handler) file(w http.ResponseWriter, r *http.Request, rest string) {
	idxStr, _, _ := strings.Cut(rest, "/")
	idx, err := strconv.Atoi(idxStr)
	if err != nil || idx < 0 || idx >= len(h.files) {
		http.NotFound(w, r)
		return
	}
	f := h.files[idx]

	src, err := os.Open(f.Source)
	if err != nil {
		http.Error(w, "the file is no longer readable", http.StatusGone)
		return
	}
	defer src.Close()
	st, err := src.Stat()
	if err != nil {
		http.Error(w, "the file is no longer readable", http.StatusGone)
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename="+strconv.Quote(path.Base(f.Name)))
	// ServeContent gives us range requests, so a dropped download resumes
	// instead of starting over.
	http.ServeContent(w, r, f.Name, st.ModTime(), src)
	h.downloaded(f.Name)
}

// zipAll streams every file as one archive, since a browser cannot be asked
// to rebuild a directory tree from a list of links.
func (h *Handler) zipAll(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="fsi-files.zip"`)
	// The size is unknown up front because entries are compressed as they go,
	// so the browser shows an indeterminate download.
	zw := zip.NewWriter(w)
	for _, f := range h.files {
		if err := addToZip(zw, f); err != nil {
			// The status line is long gone; closing mid-stream is the only
			// way left to tell the browser the archive is incomplete.
			return
		}
	}
	if err := zw.Close(); err != nil {
		return
	}
	h.downloaded("all.zip")
}

func addToZip(zw *zip.Writer, f File) error {
	src, err := os.Open(f.Source)
	if err != nil {
		return err
	}
	defer src.Close()
	st, err := src.Stat()
	if err != nil {
		return err
	}
	hdr, err := zip.FileInfoHeader(st)
	if err != nil {
		return err
	}
	hdr.Name = f.Name
	hdr.Method = zip.Deflate
	entry, err := zw.CreateHeader(hdr)
	if err != nil {
		return err
	}
	_, err = io.Copy(entry, src)
	return err
}

func (h *Handler) index(w http.ResponseWriter, r *http.Request) {
	type row struct {
		URL  string
		Name string
		Size string
	}
	data := struct {
		Files  []row
		Total  string
		ZipURL string
		Multi  bool
	}{Total: HumanBytes(h.TotalSize()), Multi: len(h.files) > 1}
	for i, f := range h.files {
		data.Files = append(data.Files, row{
			URL:  fmt.Sprintf("%s/f/%d/%s", h.Prefix(), i, urlName(f.Name)),
			Name: f.Name,
			Size: HumanBytes(f.Size),
		})
	}
	data.ZipURL = h.Prefix() + "/all.zip"

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	indexTmpl.Execute(w, data)
}

// urlName keeps the last path element only: the link already carries the
// index, and a nested path would make the browser guess a directory.
func urlName(name string) string {
	return url.PathEscape(path.Base(name))
}

func (h *Handler) downloaded(name string) {
	if h.OnDownload != nil {
		h.OnDownload(name)
	}
}

// HumanBytes formats a byte count the way the CLI does.
func HumanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

var indexTmpl = template.Must(template.New("index").Parse(`<!doctype html>
<html lang="en">
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Files shared with you</title>
<style>
  body { font: 16px/1.5 system-ui, sans-serif; margin: 0; background: #f6f7f9; color: #1d2026; }
  main { max-width: 34rem; margin: 3rem auto; padding: 0 1rem; }
  h1 { font-size: 1.4rem; margin: 0 0 .25rem; }
  p.sub { margin: 0 0 1.5rem; color: #5b6472; }
  ul { list-style: none; margin: 0; padding: 0; }
  li { background: #fff; border: 1px solid #e2e5ea; border-radius: .5rem; margin-bottom: .5rem; }
  a.file { display: flex; justify-content: space-between; gap: 1rem; padding: .9rem 1rem;
           text-decoration: none; color: inherit; }
  a.file:hover { background: #f0f3ff; }
  .name { word-break: break-all; }
  .size { color: #5b6472; white-space: nowrap; }
  a.zip { display: inline-block; margin-top: 1rem; padding: .6rem 1rem; background: #2f4ddf;
          color: #fff; border-radius: .5rem; text-decoration: none; }
  footer { margin-top: 2rem; color: #7a828f; font-size: .85rem; }
</style>
<main>
  <h1>Files shared with you</h1>
  <p class="sub">{{len .Files}} file(s), {{.Total}} total. The sender's machine is serving these directly, so they stay available only while that transfer is running.</p>
  <ul>
  {{range .Files}}
    <li><a class="file" href="{{.URL}}"><span class="name">{{.Name}}</span><span class="size">{{.Size}}</span></a></li>
  {{end}}
  </ul>
  {{if .Multi}}<a class="zip" href="{{.ZipURL}}">Download all as .zip</a>{{end}}
  <footer>Sent with fsi.</footer>
</main>
</html>
`))
