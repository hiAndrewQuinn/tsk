package cmd

import (
	"github.com/spf13/cobra"

	"github.com/hiAndrewQuinn/tsk/internal/tui"
)

var tuiCmd = &cobra.Command{
	Use:   "tui",
	Short: "Launch the interactive Terminal User Interface.",
	Long:  `Starts the full-screen interactive TUI for searching words, marking them, and exploring definitions.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Instantiate the TUI application, passing in all the
		// data loaded by the root command's PersistentPreRunE.
		app := tui.NewApp(
			Version,         // Still undefined in root.go
			debug,
			wordTrie,
			glosses,
			goDeeperMatcher, // FIX: Use the correct variable name
			exampleDB,       // Still undefined in root.go
			inflectionsDB,   // Still undefined in root.go
		)

		// Run the application and return any error it encounters.
		return app.Run()
	},
}

// FIX: The init() function has been removed.
// rootCmd.AddCommand(tuiCmd) is already called in cmd/root.go.
