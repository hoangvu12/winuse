package tasksuite

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
)

// SubmissionsFile is the file the task server appends POSTed bodies to,
// inside the task's scratch folder — a web page's submitted form is the
// app state the Check script inspects.
const SubmissionsFile = "submissions.txt"

// maxLogBody bounds one POSTed submission line (a form's fields, well
// under a kilobyte; anything bigger is a runaway, not a form).
const maxLogBody = 64 << 10

// Server is the Task Suite's local file server: tasks whose front-matter
// says `http: yes` serve their pages from it, and web pages report their
// end state to it. It is the only way a browser task can leave a
// file-checkable end state without touching anything outside the task's
// scratch folder: the server is bound to 127.0.0.1, rooted at the
// scratch folder, and dies with the task.
type Server struct {
	listener net.Listener
	url      string
	dir      string

	mu     sync.Mutex
	closed bool
}

// StartServer starts the task server rooted at dir (the task's scratch
// folder).
func StartServer(dir string) (*Server, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("starting the task server: %w", err)
	}
	s := &Server{listener: ln, url: "http://" + ln.Addr().String(), dir: dir}
	mux := http.NewServeMux()
	mux.HandleFunc("/log", s.handleLog)
	mux.Handle("/", http.FileServer(http.Dir(dir)))
	go func() { _ = http.Serve(ln, mux) }()
	return s, nil
}

// BaseURL is the server's root, templated into the task's prompt as
// {{.BaseURL}}.
func (s *Server) BaseURL() string { return s.url }

// Close stops the server. The submissions file stays: the Check reads
// it after the server is gone.
func (s *Server) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	return s.listener.Close()
}

// handleLog appends one POSTed body as a line to the submissions file.
// A page's fetch(location.origin + '/log', {body: JSON.stringify(...)})
// lands here; the Check script parses the last line.
func (s *Server) handleLog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxLogBody+1))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if len(body) > maxLogBody {
		http.Error(w, "submission too large", http.StatusRequestEntityTooLarge)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		http.Error(w, "server closed", http.StatusServiceUnavailable)
		return
	}
	f, err := os.OpenFile(filepath.Join(s.dir, SubmissionsFile), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer f.Close()
	line := append(append([]byte{}, body...), '\n')
	if _, err := f.Write(line); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
