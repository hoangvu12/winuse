package tasksuite

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestServerServesFilesAndLogs(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "form.html"), []byte("hello form"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := StartServer(dir)
	if err != nil {
		t.Fatalf("StartServer: %v", err)
	}
	defer s.Close()
	if !strings.HasPrefix(s.BaseURL(), "http://127.0.0.1:") {
		t.Errorf("base url = %q, want a loopback address", s.BaseURL())
	}

	// Files are served from the scratch folder.
	resp, err := http.Get(s.BaseURL() + "/form.html")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(body) != "hello form" {
		t.Errorf("get form.html: %d %q", resp.StatusCode, body)
	}

	// Missing files are 404s, not errors that hang a page.
	resp, err = http.Get(s.BaseURL() + "/missing.html")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("missing file status = %d", resp.StatusCode)
	}
}

func TestServerLogAppendsLines(t *testing.T) {
	dir := t.TempDir()
	s, err := StartServer(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	post := func(body string) *http.Response {
		resp, err := http.Post(s.BaseURL()+"/log", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatalf("post: %v", err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		return resp
	}
	if resp := post(`{"name":"a"}`); resp.StatusCode != http.StatusNoContent {
		t.Errorf("log post status = %d", resp.StatusCode)
	}
	post(`{"name":"b"}`)

	raw, err := os.ReadFile(filepath.Join(dir, SubmissionsFile))
	if err != nil {
		t.Fatalf("reading the submissions file: %v", err)
	}
	if string(raw) != "{\"name\":\"a\"}\n{\"name\":\"b\"}\n" {
		t.Errorf("submissions = %q", raw)
	}

	// /log is POST-only.
	resp, err := http.Get(s.BaseURL() + "/log")
	if err != nil {
		t.Fatalf("get log: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("GET /log status = %d", resp.StatusCode)
	}
}

func TestServerCloseIsIdempotent(t *testing.T) {
	s, err := StartServer(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Errorf("first close: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Errorf("second close: %v", err)
	}
}
