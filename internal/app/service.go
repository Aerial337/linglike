package app

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/aerial337/linglike/internal/dict"
	_ "github.com/aerial337/linglike/internal/dict/all"
	"github.com/aerial337/linglike/internal/render"
	"github.com/aerial337/linglike/internal/translate"
)

// LoadError records a dictionary that failed to load.
type LoadError struct {
	Path string
	Err  error
}

// Service owns the loaded dictionaries and answers queries. It is safe for
// concurrent use.
type Service struct {
	mu      sync.RWMutex
	cfg     *Config
	manager dict.Manager
	errors  []LoadError
	tr      *translate.Client
}

// NewService creates a service and loads the enabled dictionaries.
func NewService(cfg *Config) *Service {
	s := &Service{cfg: cfg, tr: &translate.Client{}}
	s.Reload()
	return s
}

// Config returns the live configuration.
func (s *Service) Config() *Config { return s.cfg }

// Reload closes and reloads all enabled dictionaries from the configuration.
func (s *Service) Reload() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.manager.Close()
	s.errors = nil
	for _, dc := range s.cfg.Dictionaries {
		if !dc.Enabled {
			continue
		}
		d, err := dict.Open(dc.Path)
		if err != nil {
			s.errors = append(s.errors, LoadError{dc.Path, err})
			continue
		}
		if dc.Name != "" {
			d = renamed{d, dc.Name}
		}
		s.manager.Dicts = append(s.manager.Dicts, d)
	}
}

type renamed struct {
	dict.Dictionary
	name string
}

func (r renamed) Name() string { return r.name }

// Errors returns the load errors from the last Reload.
func (s *Service) Errors() []LoadError {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]LoadError(nil), s.errors...)
}

// Dictionaries returns the loaded dictionaries in lookup order.
func (s *Service) Dictionaries() []dict.Dictionary {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]dict.Dictionary(nil), s.manager.Dicts...)
}

// Lookup queries all dictionaries.
func (s *Service) Lookup(word string) []dict.Result {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.manager.Lookup(word)
}

// Suggest returns headword suggestions for a prefix.
func (s *Service) Suggest(prefix string, limit int) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.manager.Suggest(prefix, limit)
}

// Translate runs machine translation with the configured languages.
func (s *Service) Translate(ctx context.Context, text string) (*translate.Result, error) {
	return s.tr.Translate(ctx, text, s.cfg.SourceLang, s.cfg.TargetLang)
}

// Query is the result of a full lookup: dictionary hits plus (optionally)
// a translation.
type Query struct {
	Text        string
	Results     []dict.Result
	Suggestions []string
	Translation *translate.Result
	TrErr       error
	TrPending   bool
}

// IsSentence reports whether text looks like running text rather than a
// single headword, in which case translation is the primary result.
func IsSentence(text string) bool {
	t := strings.TrimSpace(text)
	return len(strings.Fields(t)) > 4 || len(t) > 60
}

// Page renders a query into a result page.
func (s *Service) Page(q *Query, compact bool) *render.Page {
	p := &render.Page{Query: q.Text, Compact: compact}
	i := 0
	addTr := func() {
		if !s.cfg.TranslateEnabled {
			return
		}
		if q.TrPending {
			p.Sections = append(p.Sections, render.TranslateSection(i, s.cfg.TargetLang, nil, nil))
			i++
			return
		}
		if q.Translation != nil || q.TrErr != nil {
			p.Sections = append(p.Sections, render.TranslateSection(i, s.cfg.TargetLang, q.Translation, q.TrErr))
			i++
		}
	}
	sentence := IsSentence(q.Text)
	if sentence {
		addTr()
	}
	for _, r := range q.Results {
		p.Sections = append(p.Sections, render.DictSection(i, r))
		i++
	}
	if !sentence {
		addTr()
	}
	if len(q.Results) == 0 && len(q.Suggestions) > 0 {
		p.Sections = append(p.Sections, render.SuggestionsSection(i, q.Suggestions))
		i++
	}
	if len(p.Sections) == 0 {
		if len(s.manager.Dicts) == 0 {
			p.Sections = append(p.Sections, render.InfoSection(i, "No dictionaries", "No dictionaries are installed. Open Options > Dictionaries to add LD2, MDX, StarDict or text dictionaries."))
		}
	}
	return p
}

// Describe returns a one line description of a dictionary for lists.
func Describe(d dict.Dictionary) string {
	return fmt.Sprintf("%s (%d entries)", d.Name(), d.Count())
}

// SortedNames returns dictionary names sorted for display.
func SortedNames(ds []dict.Dictionary) []string {
	out := make([]string, len(ds))
	for i, d := range ds {
		out[i] = d.Name()
	}
	sort.Strings(out)
	return out
}
