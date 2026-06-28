package store

import (
	"sync"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	s := New()
	if s == nil {
		t.Fatal("New() returned nil")
	}
	if s.data == nil {
		t.Fatal("Store.data not initialized")
	}
}

func TestSetAndGet(t *testing.T) {
	s := New()
	now := time.Now()
	pc := PageContent{
		Name:      "test",
		URL:       "https://example.com",
		Title:     "Example",
		BodyText:  "Hello",
		FetchedAt: now,
	}
	s.Set(pc)

	got, ok := s.Get("test")
	if !ok {
		t.Fatal("expected to find key 'test'")
	}
	if got.Name != "test" || got.URL != "https://example.com" || got.Title != "Example" {
		t.Errorf("got %+v, want %+v", got, pc)
	}
}

func TestGetNotFound(t *testing.T) {
	s := New()
	_, ok := s.Get("nonexistent")
	if ok {
		t.Fatal("expected false for missing key")
	}
}

func TestSetOverwrite(t *testing.T) {
	s := New()
	s.Set(PageContent{Name: "x", Title: "first"})
	s.Set(PageContent{Name: "x", Title: "second"})

	got, ok := s.Get("x")
	if !ok {
		t.Fatal("expected to find key 'x'")
	}
	if got.Title != "second" {
		t.Errorf("want 'second', got %q", got.Title)
	}
}

func TestList(t *testing.T) {
	s := New()
	items := []PageContent{
		{Name: "a", Title: "A"},
		{Name: "b", Title: "B"},
		{Name: "c", Title: "C"},
	}
	for _, it := range items {
		s.Set(it)
	}

	list := s.List()
	if len(list) != 3 {
		t.Fatalf("expected 3 items, got %d", len(list))
	}

	m := make(map[string]bool)
	for _, it := range list {
		m[it.Name] = true
	}
	for _, it := range items {
		if !m[it.Name] {
			t.Errorf("missing item %q in list", it.Name)
		}
	}
}

func TestListEmpty(t *testing.T) {
	s := New()
	list := s.List()
	if len(list) != 0 {
		t.Fatalf("expected empty list, got %d items", len(list))
	}
}

func TestDelete(t *testing.T) {
	s := New()
	s.Set(PageContent{Name: "delme"})
	s.Delete("delme")

	_, ok := s.Get("delme")
	if ok {
		t.Fatal("expected item to be deleted")
	}
}

func TestDeleteNonExistent(t *testing.T) {
	s := New()
	s.Delete("nope")
}

func TestConcurrency(t *testing.T) {
	s := New()
	var wg sync.WaitGroup
	n := 50

	wg.Add(n)
	for i := range n {
		go func(i int) {
			defer wg.Done()
			name := string(rune('a' + i))
			s.Set(PageContent{Name: name, Title: name})
			s.Get(name)
			s.List()
			s.Delete(name)
		}(i)
	}
	wg.Wait()
}

func TestListReturnsCopy(t *testing.T) {
	s := New()
	s.Set(PageContent{Name: "x", Title: "X"})

	list := s.List()
	list[0].Title = "Y"

	got, _ := s.Get("x")
	if got.Title != "X" {
		t.Error("List must return a copy, not a reference")
	}
}
