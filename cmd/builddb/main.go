package main

import (
	"database/sql"
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	// Import the same SQLite driver your project already uses.
	_ "modernc.org/sqlite"
)

// --- Constants & Version ---
const version = "v0.0.1"
const defaultInputFile = "data/example-sentences.tsv"
const defaultOutputFile = "example-sentences.sqlite"

// ftsSchema contains the SQL to create the virtual FTS5 table.
const ftsSchema = `
CREATE VIRTUAL TABLE sentences USING fts5(
	finnish,
	english,
	tokenize = "unicode61 remove_diacritics 0"
);`

// --- Main Application ---

func main() {
	// Configure logger for clean, prefixed output, just like makegloss.
	log.SetFlags(0)
	log.SetPrefix("builddb: ")

	log.Printf("builddb (%s) - FTS5 Database Builder\n", version)

	// --- Flag setup ---
	inputFile := flag.String("in", defaultInputFile, "Input TSV file.")
	outputFile := flag.String("out", defaultOutputFile, "Output SQLite database file.")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "builddb (%s) - Converts a TSV of sentences to an SQLite FTS5 database.\n\n", version)
		fmt.Fprintf(os.Stderr, "USAGE:\n  builddb [flags]\n\n")
		fmt.Fprintf(os.Stderr, "By default, it reads '%s' and writes to '%s'.\n\n", defaultInputFile, defaultOutputFile)
		fmt.Fprintf(os.Stderr, "FLAGS:\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	// --- Processing ---
	log.Printf("Starting build process...")
	start := time.Now()

	err := buildDatabase(*inputFile, *outputFile)
	if err != nil {
		// Using log.Fatalf will print the error and exit with status 1.
		log.Fatalf("Build failed: %v", err)
	}

	duration := time.Since(start)
	log.Printf("Successfully built database in %v.", duration)
	log.Println("Done.")
}

// buildDatabase orchestrates the entire database creation process.
func buildDatabase(inputPath, outputPath string) error {
	// 1. Remove any old database file, matching the shell script's behavior.
	log.Printf("Removing existing database (if any) at '%s'...", outputPath)
	if err := os.Remove(outputPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("could not remove old database: %w", err)
	}

	// 2. Open the TSV file for reading.
	file, err := os.Open(inputPath)
	if err != nil {
		return fmt.Errorf("could not open input file '%s': %w", inputPath, err)
	}
	defer file.Close()

	// 3. Create the new SQLite database file and connect.
	log.Printf("Creating new database at '%s'...", outputPath)
	db, err := sql.Open("sqlite", outputPath)
	if err != nil {
		return fmt.Errorf("could not create sqlite database: %w", err)
	}
	defer db.Close()

	// 4. Create the FTS5 virtual table.
	log.Println("Creating FTS5 schema...")
	if _, err := db.Exec(ftsSchema); err != nil {
		return fmt.Errorf("could not create fts5 table: %w", err)
	}

	// 5. Import the data within a single transaction for performance.
	log.Printf("Importing records from '%s'...", inputPath)
	count, err := importTSV(db, file)
	if err != nil {
		return fmt.Errorf("could not import tsv data: %w", err)
	}

	log.Printf(" -> Imported %d rows.", count)
	return nil
}

// importTSV reads from the provided reader (the TSV file) and inserts records
// into the database using a single transaction.
func importTSV(db *sql.DB, r io.Reader) (int, error) {
	// The `database/sql` package does not support the `.import` command,
	// so we read the TSV and execute INSERT statements manually.
	reader := csv.NewReader(r)
	reader.Comma = '\t' // Set the delimiter to a tab.
	reader.FieldsPerRecord = 2 // Ensure we have exactly two columns.

	// Using a transaction is massively faster for bulk inserts.
	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	// Defer a rollback. If the transaction is successfully committed,
	// this will do nothing. If it fails, it will be rolled back.
	defer tx.Rollback()

	// Prepare the insert statement for re-use.
	stmt, err := tx.Prepare("INSERT INTO sentences(finnish, english) VALUES(?, ?)")
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	rowCount := 0
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break // End of file
		}
		if err != nil {
			return 0, fmt.Errorf("error reading tsv line %d: %w", rowCount+1, err)
		}

		// Execute the prepared statement with the data from the current row.
		if _, err := stmt.Exec(record[0], record[1]); err != nil {
			return 0, fmt.Errorf("could not insert row %d: %w", rowCount+1, err)
		}
		rowCount++
	}

	// Commit the transaction to save all the changes.
	if err := tx.Commit(); err != nil {
		return 0, err
	}

	return rowCount, nil
}
