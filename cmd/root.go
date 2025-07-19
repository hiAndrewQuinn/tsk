// File: cmd/root.go
package cmd

import (
	"database/sql"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"github.com/hiAndrewQuinn/tsk/internal/config"
	"github.com/hiAndrewQuinn/tsk/internal/data"
	"github.com/hiAndrewQuinn/tsk/internal/logger"
	"github.com/hiAndrewQuinn/tsk/internal/trie"
	"github.com/spf13/cobra"
)

// Global variables to hold loaded data, accessible by all subcommands.
var (
	// The Trie for general word completions (used by the TUI, etc.).
	// Renamed from prefixMatcher to avoid confusion.
	wordTrie *trie.Trie

	// The matcher for the "Go Deeper" feature used by GenerateGlossText.
	goDeeperMatcher *data.PrefixMatcher

	glosses map[string][]data.Gloss

	// 2. Declare the missing variables here.
	// Version is the application version, typically set at build time.
	Version       = "v0.0.6" // Default version
	exampleDB     *sql.DB
	inflectionsDB *sql.DB
)

// setupLogging configures the log output. If debug is true, it logs to
// debug.log. Otherwise, it discards all log output.
func setupLogging() error {
	if !config.Debug {
		log.SetOutput(io.Discard)
		return nil
	}

	log.SetFlags(log.Ldate | log.Ltime)

	file, err := os.OpenFile("debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("could not open debug.log: %w", err)
	}

	log.SetOutput(file)
	logger.Infof("Debug mode enabled. Logging to debug.log.")
	return nil
}

var rootCmd = &cobra.Command{
	Use:   "tsk [word...]",
	Short: "A terminal-based Finnish dictionary.",
	Long: `tsk is an interactive and command-line Finnish dictionary.

Run without arguments to launch the interactive TUI, or provide words
as arguments to get their definitions directly.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) > 0 {
			return findCmd.RunE(cmd, args)
		}

		stat, _ := os.Stdin.Stat()
		if (stat.Mode() & os.ModeCharDevice) == 0 {
			bytes, err := io.ReadAll(os.Stdin)
			if err != nil {
				return fmt.Errorf("error reading from stdin: %w", err)
			}
			terms := strings.Fields(string(bytes))
			if len(terms) > 0 {
				return findCmd.RunE(cmd, terms)
			}
		}

		return tuiCmd.RunE(cmd, args)
	},
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if err := setupLogging(); err != nil {
			return err
		}

		var err error
		glosses, err = data.LoadGlosses()
		if err != nil {
			return fmt.Errorf("failed to load glosses: %w", err)
		}

		// Load the Trie for word completion into its own variable.
		wordTrie, err = data.LoadTrie() // This now calls the function we added.
		if err != nil {
			return fmt.Errorf("failed to load word trie: %w", err)
		}

		// Load the matcher for the "Go Deeper" feature.
		goDeeperMatcher, err = data.NewPrefixMatcher()
		if err != nil {
			return fmt.Errorf("failed to load 'go deeper' prefix matcher: %w", err)
		}

		// 3. Load the databases.
		exampleDB, err = data.LoadExampleDB()
		if err != nil {
			return fmt.Errorf("failed to load example sentences db: %w", err)
		}

		inflectionsDB, err = data.LoadInflectionsDB()
		if err != nil {
			// This loader returns an error for file access issues but not if the
			// optional file simply doesn't exist. We should bubble up real errors.
			return fmt.Errorf("failed to load inflections db: %w", err)
		}

		return nil
	},
}

func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().BoolVar(&config.Debug, "debug", false, "Enable debug logging")
	rootCmd.AddCommand(tuiCmd)
}
