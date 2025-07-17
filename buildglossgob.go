package main

import (
	"bufio"
	"encoding/gob"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"time"
)

// ----------------------
// Version & Constants
// ----------------------
const version = "v0.0.1"
const defaultInputFile = "glosses.jsonl"
const defaultOutputFile = "glosses.gob"

// ----------------------
// Data Structures
// ----------------------

// Gloss must be identical to the struct in tsk.go to ensure compatibility.
// It's also exported (starts with a capital letter) so the gob package can process it.
type Gloss struct {
	Word     string   `json:"word"`
	Pos      string   `json:"pos"`
	Meanings []string `json:"meanings"`
}

// ----------------------
// Custom Usage Function
// ----------------------

func printCustomUsage() {
	fmt.Fprintf(os.Stderr, "makegob (%s) - Converts tsk's glosses.jsonl to a faster glosses.gob format.\n\n", version)
	fmt.Fprintf(os.Stderr, "USAGE:\n")
	fmt.Fprintf(os.Stderr, "  makegob [flags]\n")
	fmt.Fprintf(os.Stderr, "  cat glosses.jsonl | makegob\n\n")
	fmt.Fprintf(os.Stderr, "By default, it reads '%s' and writes to '%s'.\n", defaultInputFile, defaultOutputFile)
	fmt.Fprintf(os.Stderr, "If '%s' is not found, it will attempt to read from standard input.\n\n", defaultInputFile)
	fmt.Fprintf(os.Stderr, "FLAGS:\n")
	flag.PrintDefaults()
}

// ----------------------
// Main Application
// ----------------------

func main() {
	fmt.Printf("makegob (%s) - Gloss Converter\n\n", version)

	// --- Flag setup ---
	inputFile := flag.String("in", "", "Input JSONL file. (default: glosses.jsonl or stdin)")
	outputFile := flag.String("out", defaultOutputFile, "Output Gob file.")
	flag.Usage = printCustomUsage
	flag.Parse()

	// --- Determine Input Source ---
	var reader io.Reader
	var inputSourceName string

	// Priority: 1. -in flag, 2. Stdin pipe, 3. Default file
	if *inputFile != "" {
		// User explicitly provided an input file.
		file, err := os.Open(*inputFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error opening specified input file '%s': %v\n", *inputFile, err)
			os.Exit(1)
		}
		defer file.Close()
		reader = file
		inputSourceName = *inputFile
	} else {
		// Check for piped input.
		stat, _ := os.Stdin.Stat()
		if (stat.Mode() & os.ModeCharDevice) == 0 {
			reader = os.Stdin
			inputSourceName = "standard input"
		} else {
			// Fall back to the default filename.
			file, err := os.Open(defaultInputFile)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error opening default input file '%s': %v\n", defaultInputFile, err)
				fmt.Fprintln(os.Stderr, "You can specify a file with -in or pipe data to the program.")
				os.Exit(1)
			}
			defer file.Close()
			reader = file
			inputSourceName = defaultInputFile
		}
	}

	// --- Processing ---
	fmt.Printf("Reading glosses from %s...\n", inputSourceName)
	start := time.Now()

	// Load and parse the JSONL data.
	glosses, err := loadGlossesFromJSONL(reader)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error reading or parsing glosses:", err)
		os.Exit(1)
	}
	loadDuration := time.Since(start)
	fmt.Printf(" -> Loaded and parsed %d unique word entries in %v.\n", len(glosses), loadDuration)

	// Save the data to a Gob file.
	fmt.Printf("Writing data to %s...\n", *outputFile)
	start = time.Now()
	if err := saveGlossesToGob(glosses, *outputFile); err != nil {
		fmt.Fprintln(os.Stderr, "Error writing to Gob file:", err)
		os.Exit(1)
	}
	saveDuration := time.Since(start)
	fmt.Printf(" -> Successfully wrote gloss data in %v.\n\n", saveDuration)
	fmt.Println("Conversion complete.")
}

// loadGlossesFromJSONL reads from an io.Reader, parses each JSON line,
// and organizes the data into the same map structure as tsk.go.
func loadGlossesFromJSONL(r io.Reader) (map[string][]Gloss, error) {
	scanner := bufio.NewScanner(r)
	glosses := make(map[string][]Gloss)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		var g Gloss
		if err := json.Unmarshal(scanner.Bytes(), &g); err != nil {
			return nil, fmt.Errorf("error on line %d: %w", lineNum, err)
		}
		// Append the gloss to the slice for that word.
		glosses[g.Word] = append(glosses[g.Word], g)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading input: %w", err)
	}

	return glosses, nil
}

// saveGlossesToGob takes the map of glosses and writes it to a file
// using Go's binary gob encoding.
func saveGlossesToGob(glosses map[string][]Gloss, path string) error {
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("could not create file: %w", err)
	}
	defer file.Close()

	// Use a buffered writer for better performance.
	writer := bufio.NewWriter(file)
	defer writer.Flush()

	encoder := gob.NewEncoder(writer)
	if err := encoder.Encode(glosses); err != nil {
		return fmt.Errorf("gob encoding failed: %w", err)
	}

	return nil
}
