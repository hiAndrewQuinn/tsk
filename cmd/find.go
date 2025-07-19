// File: cmd/find.go
// Package cmd contains all the command definitions for the application.
package cmd

import (
	"fmt"
	"regexp"
	// "strings" // This unused import is now removed.

	"github.com/spf13/cobra"

	// Import your internal data package
	"github.com/hiAndrewQuinn/tsk/internal/data"
)

// findCmd represents the find command for non-interactive word lookups.
var findCmd = &cobra.Command{
	Use:   "find [word...]",
	Short: "Look up definitions for one or more Finnish words.",
	Long: `Looks up and prints the definitions for the Finnish words provided as arguments.
This is the non-interactive, command-line mode for word lookup.
Example:
  tsk find omena
  tsk find koira kissa`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("---")
		for i, term := range args {
			// NOTE: We now check the glosses map directly. `GenerateGlossText` handles the "not found" case.
			// Generate the rich text for the gloss.
			// It now correctly passes `goDeeperMatcher` instead of the trie.
			glossText := data.GenerateGlossText(term, glosses, goDeeperMatcher)

			// Strip the tview color tags for clean terminal output.
			cleanText := stripColorTags(glossText)
			fmt.Println(cleanText)


			// Print a separator between results, but not after the last one.
			if i < len(args)-1 {
				fmt.Println("---")
			}
		}
		// The original code had a check here, but GenerateGlossText handles the "not found"
		// case, making the else block unnecessary. This simplifies the logic.
		fmt.Println("---")
		return nil
	},
}

// init registers the find command with the root command.
func init() {
	rootCmd.AddCommand(findCmd)
}

// stripColorTags removes tview's color/style tags from a string.
func stripColorTags(s string) string {
	re := regexp.MustCompile(`\[[^:]*:[^:]*:[^\]]*\]|\[[^\]]*\]`)
	return re.ReplaceAllString(s, "")
}
