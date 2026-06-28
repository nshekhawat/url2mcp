package store

import (
	"sync"
	"time"
)

// PageContent holds the fetched and parsed content of a web page.
type PageContent struct {
	Name      string
	URL       string
	Title     string
	BodyHTML  string   // raw innerHTML of <body>
	BodyText  string   // plain text extracted from body
	Links     []string // all href values from <a> tags
	FetchedAt time.Time
}

// Store is a thread-safe in-memory registry of URL resources keyed by name.
type Store struct {
	mu   sync.RWMutex
	data map[string]PageContent
}

// New creates a new empty Store.
func New() *Store {
	return &Store{
		data: make(map[string]PageContent),
	}
}

// Set stores or overwrites a PageContent by name.
func (s *Store) Set(content PageContent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[content.Name] = content
}

// Get retrieves a PageContent by name. The bool is false if not found.
func (s *Store) Get(name string) (PageContent, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	pc, ok := s.data[name]
	return pc, ok
}

// List returns a copy of all stored PageContent entries.
func (s *Store) List() []PageContent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]PageContent, 0, len(s.data))
	for _, pc := range s.data {
		result = append(result, pc)
	}
	return result
}

// Delete removes a PageContent by name. It is safe to call for a non-existent name.
func (s *Store) Delete(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, name)
}