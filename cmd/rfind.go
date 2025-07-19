// Package cmd contains all the command definitions for the application.
package cmd

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

// rfindCmd represents the rfind (reverse-find) command.
var rfindCmd = &cobra.Command{
	Use:   "rfind [english-term...]",
	Short: "Find Finnish words by searching their English definitions.",
	Long: `Searches for Finnish words by looking for a matching term in their English definitions.
All arguments are joined together to form the search query. The search is case-insensitive.

Example:
  tsk rfind house
  tsk rfind "a small, flying insect"`,
	Args: cobra.MinimumNArgs(1), // Requires at least one word for the search term.
	RunE: func(cmd *cobra.Command, args []string) error {
		// The 'glosses' map is loaded by the root command's PersistentPreRunE hook.

		// Join all arguments into a single, lowercase search query.
		query := strings.ToLower(strings.Join(args, " "))

		// Use a map to store found words to automatically handle duplicates.
		foundWords := make(map[string]struct{})

		// Iterate through every word and its glosses in the dictionary.
		for word, glossSlice := range glosses {
			for _, gloss := range glossSlice {
				for _, meaning := range gloss.Meanings {
					// Check if the lowercase meaning contains the query.
					if strings.Contains(strings.ToLower(meaning), query) {
						foundWords[word] = struct{}{}
						// Once a match is found for this word, no need to check its other glosses.
						break
					}
				}
			}
		}

		if len(foundWords) == 0 {
			fmt.Printf("No Finnish words found with a definition containing: '%s'\n", query)
			return nil
		}

		// Convert map keys to a slice for sorting.
		matches := make([]string, 0, len(foundWords))
		for word := range foundWords {
			matches = append(matches, word)
		}
		sort.Strings(matches)

		// Print the sorted results.
		fmt.Printf("Found %d word(s) with definitions containing '%s':\n", len(matches), query)
		fmt.Println("---")
		for _, match := range matches {
			fmt.Println(match)
		}
		fmt.Println("---")

		return nil
	},
}

// init registers the rfind command with the root command.
func init() {
	rootCmd.AddCommand(rfindCmd)
}
