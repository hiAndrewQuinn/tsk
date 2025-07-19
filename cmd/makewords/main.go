// File: cmd/makewords/main.go
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"sort"
	"time"
)

// --- Constants & Version ---
const version = "v0.0.1"
const defaultInputFile = "glosses.jsonl"
const defaultOutputFile = "words.txt"

// GlossWord defines the minimal structure needed to extract the 'word' field.
// This is more efficient than unmarshalling the entire Gloss object.
type GlossWord struct {
	Word string `json:"word"`
}

func main() {
	// Configure logger with a custom prefix, just like the other tools.
	log.SetFlags(0)
	log.SetPrefix("makewords: ")

	log.Printf("makewords (%s) - Unique Word List Generator\n", version)

	// --- Flag setup ---
	inputFile := flag.String("in", "", "Input JSONL file. (default: glosses.jsonl or stdin)")
	outputFile := flag.String("out", defaultOutputFile, "Output text file.")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "makewords (%s) - Extracts a unique, sorted list of words from a glosses.jsonl file.\n\n", version)
		fmt.Fprintf(os.Stderr, "USAGE:\n  makewords [flags]\n")
		fmt.Fprintf(os.Stderr, "  cat glosses.jsonl | makewords\n\n")
		fmt.Fprintf(os.Stderr, "By default, it reads '%s' and writes to '%s'.\n", defaultInputFile, *outputFile)
		fmt.Fprintf(os.Stderr, "If an input file isn't specified, it reads from standard input.\n\n")
		fmt.Fprintf(os.Stderr, "FLAGS:\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	// --- Determine Input Source (identical logic to makegloss) ---
	var reader io.Reader
	var inputSourceName string

	if *inputFile != "" {
		file, err := os.Open(*inputFile)
		if err != nil {
			log.Fatalf("Error opening specified input file '%s': %v", *inputFile, err)
		}
		defer file.Close()
		reader = file
		inputSourceName = *inputFile
	} else {
		stat, _ := os.Stdin.Stat()
		if (stat.Mode() & os.ModeCharDevice) == 0 {
			reader = os.Stdin
			inputSourceName = "standard input"
		} else {
			file, err := os.Open(defaultInputFile)
			if err != nil {
				log.Fatalf("Error opening default input file '%s': %v", defaultInputFile, err)
			}
			defer file.Close()
			reader = file
			inputSourceName = defaultInputFile
		}
	}

	// --- Processing ---
	log.Printf("Reading words from %s...", inputSourceName)
	start := time.Now()

	count, err := generateWordsFile(reader, *outputFile)
	if err != nil {
		log.Fatalf("Failed to generate words file: %v", err)
	}

	duration := time.Since(start)
	log.Printf(" -> Successfully wrote %d unique words to '%s' in %v.", count, *outputFile, duration)
	log.Println("Done.")
}

// generateWordsFile reads from the reader, extracts unique words, sorts them, and writes to the output path.
func generateWordsFile(r io.Reader, outputPath string) (int, error) {
	// Use a map to efficiently store unique words.
	uniqueWords := make(map[string]struct{})
	scanner := bufio.NewScanner(r)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		var gw GlossWord
		// Unmarshal only the 'word' field from each JSON line.
		if err := json.Unmarshal(scanner.Bytes(), &gw); err != nil {
			return 0, fmt.Errorf("error on line %d: %w", lineNum, err)
		}
		if gw.Word != "" {
			uniqueWords[gw.Word] = struct{}{}
		}
	}

	if err := scanner.Err(); err != nil {
		return 0, fmt.Errorf("error reading input: %w", err)
	}

	// Convert map keys to a slice for sorting.
	words := make([]string, 0, len(uniqueWords))
	for word := range uniqueWords {
		words = append(words, word)
	}
	sort.Strings(words)

	// Write the sorted list to the output file.
	file, err := os.Create(outputPath)
	if err != nil {
		return 0, fmt.Errorf("could not create output file '%s': %w", outputPath, err)
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	for _, word := range words {
		if _, err := writer.WriteString(word + "\n"); err != nil {
			return 0, fmt.Errorf("could not write to output file: %w", err)
		}
	}
	writer.Flush()

	return len(words), nil
}
