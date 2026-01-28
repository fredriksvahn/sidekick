package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/earlysvahn/sidekick/internal/config"
)

type Message struct {
	Role    string    `json:"role"`
	Content string    `json:"content"`
	Time    time.Time `json:"time"`
}

type ContextHistory struct {
	System   string    `json:"system,omitempty"`
	Messages []Message `json:"messages"`
}

type ContextInfo struct {
	Name         string
	Agent        string
	Verbosity    int
	MessageCount int
	LastUsed     time.Time
}

type HistoryStore interface {
	Load(context string, limit int) ([]Message, error)
	Append(context string, msg Message) error
	LoadContext(context string) (ContextHistory, error)
	SaveContext(context string, h ContextHistory) error
	ListContexts() ([]ContextInfo, error)
}

type FileStore struct {
	baseDir string
}

func NewFileStore() *FileStore {
	return &FileStore{baseDir: filepath.Join(config.Dir(), "history")}
}

func (s *FileStore) Load(context string, limit int) ([]Message, error) {
	if limit <= 0 {
		return []Message{}, nil
	}
	h, err := s.LoadContext(context)
	if err != nil {
		return nil, err
	}
	msgs := h.Messages
	if len(msgs) > limit {
		msgs = msgs[len(msgs)-limit:]
	}
	return msgs, nil
}

func (s *FileStore) Append(context string, msg Message) error {
	h, err := s.LoadContext(context)
	if err != nil {
		return err
	}
	h.Messages = append(h.Messages, msg)
	return s.SaveContext(context, h)
}

func (s *FileStore) LoadContext(context string) (ContextHistory, error) {
	path := s.contextPath(context)
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ContextHistory{Messages: []Message{}}, nil
		}
		return ContextHistory{}, err
	}
	var h ContextHistory
	if err := json.Unmarshal(b, &h); err == nil {
		if h.Messages == nil {
			h.Messages = []Message{}
		}
		return h, nil
	}
	var msgs []Message
	if err := json.Unmarshal(b, &msgs); err != nil {
		return ContextHistory{}, err
	}
	return ContextHistory{Messages: msgs}, nil
}

func (s *FileStore) SaveContext(context string, h ContextHistory) error {
	path := s.contextPath(context)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if h.Messages == nil {
		h.Messages = []Message{}
	}
	b, err := json.MarshalIndent(h, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

func (s *FileStore) contextPath(context string) string {
	name := strings.TrimSpace(context)
	if name == "" {
		name = "misc"
	}
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.ReplaceAll(name, "\\", "_")
	return filepath.Join(s.baseDir, name+".json")
}

func (s *FileStore) ListContexts() ([]ContextInfo, error) {
	entries, err := os.ReadDir(s.baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []ContextInfo{}, nil
		}
		return nil, err
	}

	var contexts []ContextInfo
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		name := strings.TrimSuffix(entry.Name(), ".json")
		h, err := s.LoadContext(name)
		if err != nil {
			continue
		}

		if len(h.Messages) == 0 {
			continue
		}

		info := ContextInfo{
			Name:         name,
			MessageCount: len(h.Messages),
		}

		if len(h.Messages) > 0 {
			info.LastUsed = h.Messages[len(h.Messages)-1].Time
		}

		contexts = append(contexts, info)
	}

	return contexts, nil
}
