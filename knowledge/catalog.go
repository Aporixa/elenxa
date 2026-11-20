// Package knowledge provides a versioned, local research corpus and bounded
// deterministic retrieval. It performs no network or model calls.
package knowledge

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
)

//go:embed catalog/v1/techniques.json
var builtinRaw []byte

type Source struct {
	Name    string `json:"name"`
	URL     string `json:"url"`
	Version string `json:"version,omitempty"`
	License string `json:"license,omitempty"`
}

type Record struct {
	ID            string   `json:"id"`
	Title         string   `json:"title"`
	Summary       string   `json:"summary"`
	TargetClasses []string `json:"target_classes"`
	Tags          []string `json:"tags"`
	Procedure     []string `json:"procedure"`
	Corroboration []string `json:"corroboration"`
	Sources       []Source `json:"sources"`
}

type Catalog struct {
	SchemaVersion string   `json:"schema_version"`
	Version       string   `json:"version"`
	Records       []Record `json:"records"`
	digest        string
}

type Match struct {
	ID            string   `json:"id"`
	Title         string   `json:"title"`
	Summary       string   `json:"summary"`
	TargetClasses []string `json:"target_classes"`
	Tags          []string `json:"tags"`
	Procedure     []string `json:"procedure"`
	Corroboration []string `json:"corroboration"`
	Sources       []Source `json:"sources"`
	Score         int      `json:"score"`
}

type Result struct {
	CorpusVersion string  `json:"corpus_version"`
	CorpusDigest  string  `json:"corpus_digest"`
	Query         string  `json:"query"`
	Matches       []Match `json:"matches"`
	Truncated     bool    `json:"truncated"`
}

var (
	builtinOnce sync.Once
	builtin     Catalog
	builtinErr  error
	tokenRE     = regexp.MustCompile(`[a-z0-9][a-z0-9_-]*`)
)

func Builtin() (Catalog, error) {
	builtinOnce.Do(func() {
		dec := json.NewDecoder(strings.NewReader(string(builtinRaw)))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&builtin); err != nil {
			builtinErr = fmt.Errorf("decode built-in knowledge catalog: %w", err)
			return
		}
		if builtin.SchemaVersion == "" || builtin.Version == "" || len(builtin.Records) == 0 {
			builtinErr = fmt.Errorf("built-in knowledge catalog is missing schema_version, version, or records")
			return
		}
		seen := map[string]bool{}
		for _, record := range builtin.Records {
			if record.ID == "" || record.Title == "" || record.Summary == "" || seen[record.ID] {
				builtinErr = fmt.Errorf("built-in knowledge catalog has an invalid or duplicate record %q", record.ID)
				return
			}
			seen[record.ID] = true
		}
		hash := sha256.Sum256(builtinRaw)
		builtin.digest = "sha256:" + hex.EncodeToString(hash[:])
	})
	return builtin, builtinErr
}

// Search returns at most maxResults records and at most maxChars of encoded
// match data. Results are stable for a given corpus and query.
func (c Catalog) Search(query string, maxResults, maxChars int) (Result, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return Result{}, fmt.Errorf("query is required")
	}
	if maxResults <= 0 {
		maxResults = 3
	}
	if maxResults > 5 {
		maxResults = 5
	}
	if maxChars <= 0 {
		maxChars = 4000
	}
	if maxChars < 500 {
		maxChars = 500
	}
	if maxChars > 8000 {
		maxChars = 8000
	}
	tokens := tokenRE.FindAllString(strings.ToLower(query), -1)
	if len(tokens) == 0 {
		return Result{}, fmt.Errorf("query must contain a word or identifier")
	}
	var matches []Match
	for _, record := range c.Records {
		score := score(record, tokens)
		if score == 0 {
			continue
		}
		matches = append(matches, Match{
			ID: record.ID, Title: record.Title, Summary: record.Summary,
			TargetClasses: record.TargetClasses, Tags: record.Tags,
			Procedure: record.Procedure, Corroboration: record.Corroboration,
			Sources: record.Sources, Score: score,
		})
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].Score != matches[j].Score {
			return matches[i].Score > matches[j].Score
		}
		return matches[i].ID < matches[j].ID
	})
	result := Result{CorpusVersion: c.Version, CorpusDigest: c.digest, Query: query, Matches: []Match{}}
	for _, match := range matches {
		if len(result.Matches) >= maxResults {
			result.Truncated = true
			break
		}
		candidate := append(append([]Match(nil), result.Matches...), match)
		raw, _ := json.Marshal(candidate)
		if len(raw) > maxChars {
			result.Truncated = true
			break
		}
		result.Matches = candidate
	}
	return result, nil
}

func score(record Record, tokens []string) int {
	title := strings.ToLower(record.Title)
	id := strings.ToLower(record.ID)
	tags := strings.ToLower(strings.Join(record.Tags, " "))
	targets := strings.ToLower(strings.Join(record.TargetClasses, " "))
	body := strings.ToLower(record.Summary + " " + strings.Join(record.Procedure, " ") + " " + strings.Join(record.Corroboration, " "))
	total := 0
	for _, token := range tokens {
		switch {
		case strings.Contains(id, token):
			total += 8
		case strings.Contains(title, token):
			total += 6
		}
		if strings.Contains(tags, token) {
			total += 4
		}
		if strings.Contains(targets, token) {
			total += 3
		}
		if strings.Contains(body, token) {
			total++
		}
	}
	return total
}
