package notes

// The store: %LOCALAPPDATA%\winuse\notes.json, one file per Windows user,
// shared by every Agent and Harness (ADR 0024). The helper owns it. Every
// operation takes one lock (an in-process mutex and a lock on
// notes.json.lock, which also keeps out an older helper finishing its
// requests during an update), reads the file, and a write goes to a temp
// file that is then renamed over it, so a reader never sees half a file.
//
// A file that does not parse is moved aside to notes.json.bad, never
// dropped silently: the operation goes on with no Notes and returns the
// .bad path, and the command says so.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

// MaxTextLen is a Note's longest text, in characters.
const MaxTextLen = 200

// FileName is the store's file name under %LOCALAPPDATA%\winuse.
const FileName = "notes.json"

// Note is one stored Note.
type Note struct {
	Key    string    `json:"key"`
	Text   string    `json:"text"`
	Added  time.Time `json:"added"`
	Author string    `json:"author"`
}

// KeyCount is one key and how many Notes it holds.
type KeyCount struct {
	Key   string `json:"key"`
	Count int    `json:"count"`
}

// The refusals of Add: nothing is stored.
var (
	ErrEmpty     = errors.New("a Note needs a text")
	ErrMultiLine = errors.New("a Note is one line")
	ErrTooLong   = fmt.Errorf("a Note is at most %d characters", MaxTextLen)
	ErrDuplicate = errors.New("the same Note is already stored for this key")
)

// RangeError is a Remove whose number names no Note of the key.
type RangeError struct {
	Key   string
	N     int
	Count int
}

func (e *RangeError) Error() string {
	if e.Count == 0 {
		return fmt.Sprintf("%s has no Notes", e.Key)
	}
	return fmt.Sprintf("%s has %d Notes, numbered 1 to %d; there is no Note %d", e.Key, e.Count, e.Count, e.N)
}

// file is notes.json's shape. Notes are kept in the order they were
// added, oldest first, all keys in one list.
type file struct {
	Version int    `json:"version"`
	Notes   []Note `json:"notes"`
}

const fileVersion = 1

// Store is the Notes file at Path. One Store is safe for concurrent use,
// and several Stores (or processes) on one file take turns.
type Store struct {
	Path string
	Now  func() time.Time // nil: time.Now

	mu sync.Mutex
}

// NewStore is the store at path.
func NewStore(path string) *Store { return &Store{Path: path} }

// DefaultPath is %LOCALAPPDATA%\winuse\notes.json.
func DefaultPath() (string, error) {
	dir := os.Getenv("LOCALAPPDATA")
	if dir == "" {
		return "", errors.New("LOCALAPPDATA is not set, so winuse has nowhere to keep Notes")
	}
	return filepath.Join(dir, "winuse", FileName), nil
}

var (
	defaultOnce  sync.Once
	defaultStore *Store
	defaultErr   error
)

// Default is the helper's one store at DefaultPath.
func Default() (*Store, error) {
	defaultOnce.Do(func() {
		p, err := DefaultPath()
		if err != nil {
			defaultErr = err
			return
		}
		defaultStore = NewStore(p)
	})
	return defaultStore, defaultErr
}

func (s *Store) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// CleanText is a Note's text as stored: trimmed, and refused when empty,
// on more than one line, with control characters or over MaxTextLen
// characters.
func CleanText(text string) (string, error) {
	t := strings.TrimSpace(text)
	switch {
	case t == "":
		return "", ErrEmpty
	case strings.ContainsAny(t, "\r\n  "):
		return "", ErrMultiLine
	case strings.IndexFunc(t, func(r rune) bool { return unicode.IsControl(r) && r != '\t' }) >= 0:
		return "", errors.New("a Note cannot hold control characters")
	case utf8.RuneCountInString(t) > MaxTextLen:
		return "", fmt.Errorf("%w; this one has %d", ErrTooLong, utf8.RuneCountInString(t))
	}
	return strings.ReplaceAll(t, "\t", " "), nil
}

// Add stores a Note for key, unless its text is refused (ErrEmpty,
// ErrMultiLine, ErrTooLong) or key already holds the same text
// (ErrDuplicate). bad is where a corrupt file was moved, "" when it was
// not.
func (s *Store) Add(key, text, author string) (n Note, bad string, err error) {
	t, err := CleanText(text)
	if err != nil {
		return Note{}, "", err
	}
	if key == "" {
		return Note{}, "", errors.New("a Note needs a key")
	}
	err = s.update(&bad, func(f *file) (bool, error) {
		for _, old := range f.Notes {
			if old.Key == key && old.Text == t {
				return false, ErrDuplicate
			}
		}
		n = Note{Key: key, Text: t, Added: s.now().UTC().Truncate(time.Second), Author: author}
		f.Notes = append(f.Notes, n)
		return true, nil
	})
	return n, bad, err
}

// List is key's Notes, oldest first: Note n of `note list` and `note
// remove` is List's [n-1].
func (s *Store) List(key string) (notes []Note, bad string, err error) {
	err = s.view(&bad, func(f *file) {
		for _, n := range f.Notes {
			if n.Key == key {
				notes = append(notes, n)
			}
		}
	})
	return notes, bad, err
}

// All is every Note, oldest first.
func (s *Store) All() (notes []Note, bad string, err error) {
	err = s.view(&bad, func(f *file) { notes = slices.Clone(f.Notes) })
	return notes, bad, err
}

// Keys is every key that holds Notes, sorted, with its count.
func (s *Store) Keys() (keys []KeyCount, bad string, err error) {
	err = s.view(&bad, func(f *file) {
		counts := map[string]int{}
		for _, n := range f.Notes {
			counts[n.Key]++
		}
		for k, c := range counts {
			keys = append(keys, KeyCount{Key: k, Count: c})
		}
		slices.SortFunc(keys, func(a, b KeyCount) int { return strings.Compare(a.Key, b.Key) })
	})
	return keys, bad, err
}

// Remove removes key's Note number n (1 is the oldest, as List numbers
// them). A number outside 1..count is a *RangeError.
func (s *Store) Remove(key string, n int) (removed Note, bad string, err error) {
	err = s.update(&bad, func(f *file) (bool, error) {
		seen := 0
		for i, note := range f.Notes {
			if note.Key != key {
				continue
			}
			seen++
			if seen == n {
				removed = note
				f.Notes = slices.Delete(f.Notes, i, i+1)
				return true, nil
			}
		}
		count := 0
		for _, note := range f.Notes {
			if note.Key == key {
				count++
			}
		}
		return false, &RangeError{Key: key, N: n, Count: count}
	})
	return removed, bad, err
}

// Clear removes every Note of key and says how many there were.
func (s *Store) Clear(key string) (count int, bad string, err error) {
	err = s.update(&bad, func(f *file) (bool, error) {
		kept := f.Notes[:0]
		for _, n := range f.Notes {
			if n.Key == key {
				count++
				continue
			}
			kept = append(kept, n)
		}
		f.Notes = kept
		return count > 0, nil
	})
	return count, bad, err
}

// view runs read on the file's Notes under the lock.
func (s *Store) view(bad *string, read func(f *file)) error {
	return s.update(bad, func(f *file) (bool, error) {
		read(f)
		return false, nil
	})
}

// update runs change on the file's Notes under the lock, and writes them
// back when it says so.
func (s *Store) update(bad *string, change func(f *file) (write bool, err error)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return fmt.Errorf("creating the Notes folder: %w", err)
	}
	unlock, err := lockFile(s.Path + ".lock")
	if err != nil {
		return fmt.Errorf("locking %s: %w", s.Path, err)
	}
	defer unlock()
	f, moved, err := s.load()
	if err != nil {
		return err
	}
	*bad = moved
	write, err := change(&f)
	if err != nil || !write {
		return err
	}
	return s.save(f)
}

// load reads the file: no file is no Notes; a file that does not parse is
// moved to <path>.bad (its path returned) and counts as no Notes.
func (s *Store) load() (f file, moved string, err error) {
	data, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return file{Version: fileVersion}, "", nil
	}
	if err != nil {
		return file{}, "", fmt.Errorf("reading %s: %w", s.Path, err)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return file{Version: fileVersion}, "", nil
	}
	if perr := parse(data, &f); perr != nil {
		moved = s.Path + ".bad"
		if err := renameRetry(s.Path, moved); err != nil {
			return file{}, "", fmt.Errorf("%s does not parse (%v), and moving it to %s failed: %w", s.Path, perr, moved, err)
		}
		return file{Version: fileVersion}, moved, nil
	}
	return f, "", nil
}

// parse decodes the file and checks what a hand edit could break: every
// Note has a key and a text.
func parse(data []byte, f *file) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(f); err != nil {
		return err
	}
	if dec.More() {
		return errors.New("more than one JSON value")
	}
	for i, n := range f.Notes {
		if n.Key == "" || n.Text == "" {
			return fmt.Errorf("note %d has no key or text", i+1)
		}
	}
	return nil
}

// save writes the file to a temp file beside it and renames it over.
func (s *Store) save(f file) error {
	f.Version = fileVersion
	if f.Notes == nil {
		f.Notes = []Note{}
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.Path), FileName+".tmp*")
	if err != nil {
		return fmt.Errorf("writing the Notes: %w", err)
	}
	name := tmp.Name()
	_, werr := tmp.Write(append(data, '\n'))
	if serr := tmp.Sync(); werr == nil {
		werr = serr
	}
	if cerr := tmp.Close(); werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = renameRetry(name, s.Path)
	}
	if werr != nil {
		os.Remove(name)
		return fmt.Errorf("writing %s: %w", s.Path, werr)
	}
	return nil
}

// renameRetry renames, retrying briefly: on Windows a scanner or indexer
// that opened the file for a moment makes a rename fail with access
// denied.
func renameRetry(from, to string) error {
	var err error
	for range 20 {
		if err = os.Rename(from, to); err == nil {
			return nil
		}
		time.Sleep(25 * time.Millisecond)
	}
	return err
}
