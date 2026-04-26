package concepts

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// Store paths inside <workspace>/.gemba/concepts/. The names live as
// constants so the CLI, the SPA (when it lands), and any future
// importer all hit the same files.
const (
	StoreDirName     = "concepts"
	VocabularyFile   = "vocabulary.json"
	SuggestionsFile  = "suggestions.json"
	DecisionsLogFile = "decisions.log"
)

// StoreDir returns the absolute concepts directory under the
// workspace's .gemba/. Callers usually pass `<workspace>` resolved
// elsewhere; the helper just composes the path.
func StoreDir(workspace string) string {
	return filepath.Join(workspace, ".gemba", StoreDirName)
}

// EnsureStoreDir mkdir-p's the concepts directory. Idempotent.
func EnsureStoreDir(workspace string) (string, error) {
	dir := StoreDir(workspace)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("concepts: mkdir %s: %w", dir, err)
	}
	return dir, nil
}

// LoadVocabulary reads vocabulary.json. Returns an empty Vocabulary
// (not an error) when the file doesn't exist — a fresh workspace's
// first read is "no terms yet", which the bootstrap path handles.
func LoadVocabulary(workspace string) (*Vocabulary, error) {
	path := filepath.Join(StoreDir(workspace), VocabularyFile)
	v := &Vocabulary{}
	if err := readJSON(path, v); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return v, nil
		}
		return nil, err
	}
	return v, nil
}

// SaveVocabulary writes vocabulary.json atomically (write to a
// sibling .tmp + rename) so a crashed process never leaves a half-
// written file.
func SaveVocabulary(workspace string, v *Vocabulary) error {
	dir, err := EnsureStoreDir(workspace)
	if err != nil {
		return err
	}
	v.Sort()
	return writeJSONAtomic(filepath.Join(dir, VocabularyFile), v)
}

// LoadSuggestions / SaveSuggestions mirror the vocabulary helpers.
type SuggestionList struct {
	Suggestions []Suggestion `json:"suggestions"`
}

func LoadSuggestions(workspace string) (*SuggestionList, error) {
	path := filepath.Join(StoreDir(workspace), SuggestionsFile)
	out := &SuggestionList{}
	if err := readJSON(path, out); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return out, nil
		}
		return nil, err
	}
	return out, nil
}

func SaveSuggestions(workspace string, list *SuggestionList) error {
	dir, err := EnsureStoreDir(workspace)
	if err != nil {
		return err
	}
	return writeJSONAtomic(filepath.Join(dir, SuggestionsFile), list)
}

// AppendDecision adds one entry to the decisions JSONL log. The log
// is append-only: every approve / reject becomes a permanent line so
// the audit trail survives vocabulary edits.
func AppendDecision(workspace string, d Decision) error {
	dir, err := EnsureStoreDir(workspace)
	if err != nil {
		return err
	}
	path := filepath.Join(dir, DecisionsLogFile)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("concepts: open decisions log: %w", err)
	}
	defer f.Close()
	if d.At.IsZero() {
		d.At = time.Now().UTC()
	}
	enc := json.NewEncoder(f)
	if err := enc.Encode(d); err != nil {
		return fmt.Errorf("concepts: append decision: %w", err)
	}
	return nil
}

// ReadDecisions returns every decision in the log, in order. Used by
// the CLI's `concepts log` view; the file itself stays appendable
// for new entries.
func ReadDecisions(workspace string) ([]Decision, error) {
	path := filepath.Join(StoreDir(workspace), DecisionsLogFile)
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	out := []Decision{}
	for {
		var d Decision
		if err := dec.Decode(&d); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("concepts: parse decisions log: %w", err)
		}
		out = append(out, d)
	}
	return out, nil
}

func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, v)
}

func writeJSONAtomic(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("concepts: marshal %s: %w", path, err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("concepts: write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("concepts: rename %s: %w", path, err)
	}
	return nil
}

func stat(path string) (os.FileInfo, error) {
	return os.Stat(path)
}
