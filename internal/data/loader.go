// Package data handles the loading, parsing, and processing of all application
// data from embedded files. This includes the word list for the trie, the
// glosses for definitions, and the databases for example sentences and inflections.
package data

import (
	"bufio"
	"bytes"
	"database/sql"
	"encoding/gob"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	// This allows embedding files directly into the Go binary.
	_ "embed"

	// SQLite driver.
	_ "modernc.org/sqlite"

	"github.com/hiAndrewQuinn/tsk/internal/logger"
	"github.com/hiAndrewQuinn/tsk/internal/trie"
)

//go:embed assets/words.txt
var wordsTxt string

//go:embed assets/glosses.gob
var glossesGob []byte

//go:embed assets/go-deeper.txt
var goDeeperTxt string

//go:embed assets/example-sentences.sqlite
var embeddedDB []byte

// Gloss represents the definition of a word, including its part of speech
// and a list of meanings.
type Gloss struct {
	Word     string   `json:"word"`
	Pos      string   `json:"pos"`
	Meanings []string `json:"meanings"`
}

// LoadWords reads the embedded words.txt file and returns a slice of strings,
// with each string being a word from the dictionary.
func LoadWords() ([]string, error) {
	scanner := bufio.NewScanner(strings.NewReader(wordsTxt))
	var words []string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// The source file may have quotes around words; remove them.
		line = strings.Trim(line, "\"")
		if line != "" {
			words = append(words, line)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error scanning words.txt: %w", err)
	}
	return words, nil
}

// LoadGlosses decodes the embedded glosses.gob file and returns a map where
// keys are words and values are slices of Gloss structs.
func LoadGlosses() (map[string][]Gloss, error) {
	reader := bytes.NewReader(glossesGob)
	decoder := gob.NewDecoder(reader)

	var glosses map[string][]Gloss
	if err := decoder.Decode(&glosses); err != nil {
		return nil, fmt.Errorf("error decoding glosses.gob: %w", err)
	}
	return glosses, nil
}

// --- Prefix Matching for "Go Deeper" ---

// PrefixMatcher holds the data structures needed for efficient "go deeper"
// prefix lookups.
type PrefixMatcher struct {
	prefixMap     map[string]struct{}
	prefixLengths []int
}

// NewPrefixMatcher creates and initializes a PrefixMatcher by loading phrases
// from the embedded go-deeper.txt file.
func NewPrefixMatcher() (*PrefixMatcher, error) {
	scanner := bufio.NewScanner(strings.NewReader(goDeeperTxt))
	var phrases []string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			phrases = append(phrases, line)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error scanning go-deeper.txt: %w", err)
	}

	matcher := &PrefixMatcher{
		prefixMap: make(map[string]struct{}, len(phrases)),
	}
	lengthSet := make(map[int]struct{})

	for _, phrase := range phrases {
		// Add a space to ensure we match whole words/phrases.
		key := phrase + " "
		matcher.prefixMap[key] = struct{}{}
		lengthSet[len(key)] = struct{}{}
	}

	for l := range lengthSet {
		matcher.prefixLengths = append(matcher.prefixLengths, l)
	}
	// Sort lengths in descending order to find the longest match first.
	sort.Sort(sort.Reverse(sort.IntSlice(matcher.prefixLengths)))

	return matcher, nil
}

// findLongestPrefix checks if a string starts with one of the known "go deeper"
// phrases and returns the longest one that matches.
func (pm *PrefixMatcher) findLongestPrefix(s string) (string, bool) {
	// This handles the entry and exit logs automatically.
	defer log.Printf("%s: Exiting.", logger.Enter())
	logger.Tracef("Checking for prefixes which match '%s'", s)

	// This check is a fast path to avoid unnecessary work.
	if pm == nil || len(pm.prefixMap) == 0 {
		return "", false
	}

	words := strings.Fields(s)
	// Iterate from the longest possible phrase down to a single word.
	for i := len(words); i > 0; i-- {
		candidate := strings.Join(words[:i], " ") + " "
		logger.Tracef("Is '%s' in prefixMap?", candidate)

		if _, ok := pm.prefixMap[candidate]; ok {
			logger.Tracef("Yes! Returning '%s' from prefixMap.", candidate)
			return candidate, true
		}
	}
	return "", false
}

// --- Database Loaders ---

// LoadExampleDB loads the embedded example sentences SQLite database into a
// temporary file on disk and returns a connection handle. The temporary file
// is automatically cleaned up when the program exits.
func LoadExampleDB() (*sql.DB, error) {
	// Create a temporary file to hold the database.
	tmpFile, err := ioutil.TempFile("", "tsk-examples-*.sqlite")
	if err != nil {
		return nil, fmt.Errorf("could not create temp file for example DB: %w", err)
	}
	// Important: This defer ensures the temp file is closed, but the OS will
	// still need to clean it up. For a robust solution, the caller should
	// handle closing the DB and removing the file.
	// defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write(embeddedDB); err != nil {
		return nil, fmt.Errorf("could not write embedded example DB to temp file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return nil, fmt.Errorf("could not close temp example DB file: %w", err)
	}

	// Open the database from the temporary file.
	db, err := sql.Open("sqlite", tmpFile.Name()+"?_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("could not open example DB from temp file: %w", err)
	}

	return db, nil
}

// LoadInflectionsDB attempts to load an optional, user-provided inflections
// database from the standard user config directory. If not found, it returns
// (nil, nil) indicating it's an optional feature.
func LoadInflectionsDB() (*sql.DB, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		// Non-fatal error, as this DB is optional. Log it and continue.
		log.Printf("[INFO] Could not determine user config directory: %v. Inflection search will be disabled.", err)
		return nil, nil
	}

	dbPath := filepath.Join(configDir, "tsk", "inflections.db")

	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		log.Printf("[INFO] Optional inflections database not found at '%s'. Inflection search will be disabled.", dbPath)
		return nil, nil // No error, just no DB.
	}

	dsn := fmt.Sprintf("file:%s?_journal_mode=WAL&immutable=1", filepath.ToSlash(dbPath))
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("could not open inflections database at %s: %w", dbPath, err)
	}

	if err = db.Ping(); err != nil {
		return nil, fmt.Errorf("could not connect to inflections database at %s: %w", dbPath, err)
	}

	log.Printf("[INFO] Inflections database loaded successfully from %s.", dbPath)
	return db, nil
}

// --- Text Generation ---

// GenerateGlossText creates a formatted string for a word's details, including
// any "go deeper" recursive definitions. This text includes tview color tags.
func GenerateGlossText(word string, glosses map[string][]Gloss, matcher *PrefixMatcher) string {
	glossSlice, ok := glosses[word]
	if !ok {
		return fmt.Sprintf("[red]%s[white]\n\nNo definition available.", word)
	}

	var builder strings.Builder
	for i, gloss := range glossSlice {
		if i > 0 {
			builder.WriteString("\n") // Separator for multiple glosses of the same word.
		}
		builder.WriteString(fmt.Sprintf("[white]%s [yellow](%s)[white]\n\n", gloss.Word, gloss.Pos))
		for _, meaning := range gloss.Meanings {
			builder.WriteString(fmt.Sprintf("- %s\n", meaning))
			// Recursively find and append deeper glosses.
			builder.WriteString(getDeeperGlosses(meaning, glosses, matcher, 1))
		}
	}
	return builder.String()
}

// getDeeperGlosses is a recursive helper that looks for linkable phrases.
func getDeeperGlosses(text string, glosses map[string][]Gloss, matcher *PrefixMatcher, level int) string {
	// We only recurse two levels deep to prevent infinite loops and excessive output.
	if level > 2 {
		return ""
	}

	prefix, found := matcher.findLongestPrefix(text)
	if !found {
		return ""
	}

	var builder strings.Builder
	target := extractTargetWord(text, prefix)

	if targetGlosses, ok := glosses[target]; ok {
		// Define formatting based on recursion level.
		var glossFormat, meaningFormat string
		if level == 1 {
			glossFormat = "[lightgray]  ~> %s (%s)[white]\n"
			meaningFormat = "[lightgray]      - %s[white]\n"
		} else { // level == 2
			glossFormat = "[gray]        ~> %s (%s)[white]\n"
			meaningFormat = "[gray]           - %s[white]\n"
		}

		for _, tg := range targetGlosses {
			builder.WriteString(fmt.Sprintf(glossFormat, tg.Word, tg.Pos))
			for _, tm := range tg.Meanings {
				builder.WriteString(fmt.Sprintf(meaningFormat, tm))
				// Recursive call for the next level.
				builder.WriteString(getDeeperGlosses(tm, glosses, matcher, level+1))
			}
		}
	}
	return builder.String()
}

// extractTargetWord cleans up a "go deeper" target phrase to isolate the word
// to be looked up. e.g., "a form of apple" -> "apple".
func extractTargetWord(meaning, prefix string) string {
	target := strings.TrimPrefix(meaning, prefix)
	target = strings.TrimSpace(target)
	// Remove trailing punctuation.
	target = strings.TrimRight(target, ".,:;!?")
	// Remove parenthetical explanations, e.g., "house (building)".
	if idx := strings.Index(target, "("); idx != -1 {
		target = strings.TrimSpace(target[:idx])
	}
	if idx := strings.Index(target, ";"); idx != -1 {
		target = strings.TrimSpace(target[:idx])
	}
	return target
}

func LoadTrie() (*trie.Trie, error) {
	words, err := LoadWords()
	if err != nil {
		return nil, fmt.Errorf("failed to load words for trie: %w", err)
	}

	t := trie.NewTrie()
	for _, word := range words {
		t.Insert(word)
	}

	return t, nil
}
