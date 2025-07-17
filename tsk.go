package main

import (
	"bufio"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	_ "modernc.org/sqlite" // pure-Go SQLite driver with FTS5 support
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"
	"unicode"
	"unsafe"

	_ "embed"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// ----------------------
// Version Variable
// ----------------------
const version = "v0.0.6"

// ----------------------
// Help Text Constant
// ----------------------
const helpText = `[gray]
	Keybindings:
	Esc        = Exit
	Enter      = Clear search
	Up/Down    = Scroll word list

	Tab        = Scroll Word Details forward
	Shift-Tab  = Scroll Word Details backward

	[teal]Control-T[gray]  = Show [teal]example sentences[gray], from Tatoeba for the selected word.
	[yellow]Control-S[gray]  = [yellow]Mark[gray]/unmark words. All marked words will be saved upon Esc to a text file.
	[green]Control-L[gray]  = [green]List[gray] marked words. 
	[cyan]Control-F[gray]  = [cyan]Reverse-find[gray] words by searching their English definitions.
	[pink]Control-H[gray]  = Show this [pink]help[gray] text again.

	[red]Control-R[gray]  = [red]Report a bug[gray] on GitHub.com. [red]Opens your web browser[gray] to

	                   [red]https://github.com/hiAndrewQuinn/tsk/issues/new[gray]

	             Provide as many details as you can. Response is on a best-effort basis.

	[white]
	`

const finnishFlag = `[gray]
                        _,-(.;)
                    _,-',###""
                _,-',###",'|
            _,-',###" ,-" :|
        _,-',###" _,#"   .'|
### _,-',###"_,######    : |
_,-',###;-'"~. #####9   :' |
,###"   |   :  ######  ,. _|
"       |  :   #####( .;###|
        |:'.   ######,6####|
        |..   ;############|
        ":_,###############|
         |##############'~ |
         |############".   |
        .###########':     |
        :##".  #####. '    |
        | :'   ######.    .|
        |.'    ######      |
        |.     ######    ':|
        ":     ######   .:.|
         |    ."#####    ._|
         |     :#####_,-'""
         |   '.,###""
        :'  .:,-'
        |_.,-'
        "
	[white]
	`

// ----------------------
// Global Debug Flag
// ----------------------
var debug bool

// ----------------------
// Embedded Data Files
// ----------------------

//go:embed words.txt
var wordsTxt string

//go:embed glosses.jsonl
var glossesJsonl string

//go:embed go-deeper.txt
var goDeeperTxt string

//go:embed example-sentences.sqlite
var embeddedDB []byte
var exampleDB *sql.DB

// Schema for the embeddedDB, at least as of 2025-05-07 :
//
// CREATE VIRTUAL TABLE sentences USING fts5(
//   finnish,
//   english
// )
// /* sentences(finnish,english) */;
// CREATE TABLE IF NOT EXISTS 'sentences_data'(id INTEGER PRIMARY KEY, block BLOB);
// CREATE TABLE IF NOT EXISTS 'sentences_idx'(segid, term, pgno, PRIMARY KEY(segid, term)) WITHOUT ROWID;
// CREATE TABLE IF NOT EXISTS 'sentences_content'(id INTEGER PRIMARY KEY, c0, c1);
// CREATE TABLE IF NOT EXISTS 'sentences_docsize'(id INTEGER PRIMARY KEY, sz BLOB);
// CREATE TABLE IF NOT EXISTS 'sentences_config'(k PRIMARY KEY, v) WITHOUT ROWID;
//
// We pretty much only use this for full-text searches for example sentences.

// ----------------------
// Constants
// ----------------------

const (
	TRIE_MAX_SEARCH_DEPTH = 50 // Maximum number of words to return

	// Informational only.
	WORD_LIST_FILE = "words.txt"
	GLOSSES_FILE   = "glosses.jsonl"

	scrollDebounce = 5000 * time.Millisecond // Only allow one scroll event in this timeframe
)

// ----------------------
// Custom Usage Function
// ----------------------

func printCustomUsage() {
	// The main header is printed regardless, so we start with the usage details.
	fmt.Fprintf(os.Stderr, "A terminal-based Finnish dictionary. Interactive TUI by default,\n\n")
	fmt.Fprintf(os.Stderr, "but can also be run as a normal CLI application.\n\n")

	fmt.Fprintf(os.Stderr, "USAGE:\n")
	fmt.Fprintf(os.Stderr, "  tsk [flags]\n")
	fmt.Fprintf(os.Stderr, "  tsk [flags] [word...]\n")
	fmt.Fprintf(os.Stderr, "  <command> | tsk [flags]\n\n")

	fmt.Fprintf(os.Stderr, "MODES OF OPERATION:\n")
	fmt.Fprintf(os.Stderr, "  Interactive TUI (default):\n")
	fmt.Fprintf(os.Stderr, "    Run without arguments or piped input to launch the interactive interface.\n")
	fmt.Fprintf(os.Stderr, "    $ tsk\n\n")

	fmt.Fprintf(os.Stderr, "  Direct CLI (by arguments):\n")
	fmt.Fprintf(os.Stderr, "    Provide one or more words as arguments to get their definitions printed to stdout.\n")
	fmt.Fprintf(os.Stderr, "    $ tsk hei maailma\n\n")

	fmt.Fprintf(os.Stderr, "  Direct CLI (by piped input):\n")
	fmt.Fprintf(os.Stderr, "    Pipe text into the program to look up all words from the input stream.\n")
	fmt.Fprintf(os.Stderr, "    $ echo \"terve taas\" | tsk\n\n")

	fmt.Fprintf(os.Stderr, "FLAGS:\n")
	// This helper function prints the default flag information.
	flag.PrintDefaults()
}

// ----------------------
// Trie Data Structure
// ----------------------

type TrieNode struct {
	children map[rune]*TrieNode
	isEnd    bool
}

func newTrieNode() *TrieNode {
	return &TrieNode{children: make(map[rune]*TrieNode)}
}

type Trie struct {
	root *TrieNode
}

func NewTrie() *Trie {
	return &Trie{root: newTrieNode()}
}

func (t *Trie) Insert(word string) {
	node := t.root
	for _, ch := range word {
		if _, ok := node.children[ch]; !ok {
			node.children[ch] = newTrieNode()
		}
		node = node.children[ch]
	}
	node.isEnd = true
}

func (node *TrieNode) collectWords(prefix string, words *[]string) {
	if len(*words) >= TRIE_MAX_SEARCH_DEPTH {
		return
	}
	if node.isEnd {
		*words = append(*words, prefix)
		if len(*words) >= TRIE_MAX_SEARCH_DEPTH {
			return
		}
	}
	for ch, child := range node.children {
		child.collectWords(prefix+string(ch), words)
		if len(*words) >= TRIE_MAX_SEARCH_DEPTH {
			return
		}
	}
}

func (t *Trie) FindWords(prefix string) []string {
	node := t.root
	for _, ch := range prefix {
		next, exists := node.children[ch]
		if !exists {
			return []string{}
		}
		node = next
	}
	var words []string
	node.collectWords(prefix, &words)
	return words
}

func (t *Trie) CountNodes() int {
	count := 0
	var traverse func(node *TrieNode)
	traverse = func(node *TrieNode) {
		count++
		for _, child := range node.children {
			traverse(child)
		}
	}
	traverse(t.root)
	return count
}

// ----------------------
// Utility to load words from embedded data
// ----------------------

func loadWords() ([]string, error) {
	scanner := bufio.NewScanner(strings.NewReader(wordsTxt))
	var words []string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		line = strings.Trim(line, "\"")
		if line != "" {
			words = append(words, line)
		}
	}
	return words, scanner.Err()
}

// ----------------------
// Utility: Strip tview color tags
// ----------------------

func stripColorTags(s string) string {
	// This regex matches any sequence like `[<color>]` or `[<color>:<bgcolor>]`
	re := regexp.MustCompile(`\[[^\]]*\]`)
	return re.ReplaceAllString(s, "")
}

// ----------------------
// Gloss Data Structures & Loader
// ----------------------

type Gloss struct {
	Word     string   `json:"word"`
	Pos      string   `json:"pos"`
	Meanings []string `json:"meanings"`
}

func loadGlosses() (map[string][]Gloss, error) {
	scanner := bufio.NewScanner(strings.NewReader(glossesJsonl))
	glosses := make(map[string][]Gloss)
	for scanner.Scan() {
		var g Gloss
		if err := json.Unmarshal(scanner.Bytes(), &g); err != nil {
			return nil, err
		}
		glosses[g.Word] = append(glosses[g.Word], g)
	}
	return glosses, scanner.Err()
}

// getDeeperGlosses is a recursive helper that looks for linkable phrases in a meaning string,
// fetches their definitions, and formats them with the appropriate indentation and color
// based on the recursion depth. It recurses one level deep to handle nested definitions.
func getDeeperGlosses(text string, glosses map[string][]Gloss, level int) string {
	// Base case: We only go two levels deep (level 1 and level 2).
	if level > 2 {
		return ""
	}

	var builder strings.Builder

	// Local helper for cleaning the target word found after a prefix.
	extractTarget := func(meaning, prefix string) string {
		target := strings.TrimRight(strings.TrimSpace(strings.TrimPrefix(meaning, prefix)), ".,:;!?")
		if idx := strings.Index(target, "("); idx != -1 {
			target = strings.TrimSpace(target[:idx])
		}
		if idx := strings.Index(target, ";"); idx != -1 {
			target = strings.TrimSpace(target[:idx])
		}
		return target
	}

	// Define formatting based on recursion level to match the original output.
	var glossFormat, meaningFormat string
	if level == 1 {
		glossFormat = "[lightgray]  ~> %s (%s)[white]\n"
		meaningFormat = "[lightgray]      - %s[white]\n"
	} else { // level == 2
		glossFormat = "[gray]         ~> %s (%s)[white]\n"
		meaningFormat = "[gray]            - %s[white]\n"
	}

	// Main logic: find prefix, extract target, look up glosses, and format.
	if prefix, found := findLongestPrefix(text); found {
		target := extractTarget(text, prefix)
		if targetGlosses, ok := glosses[target]; ok {
			for _, tg := range targetGlosses {
				builder.WriteString(fmt.Sprintf(glossFormat, tg.Word, tg.Pos))
				for _, tm := range tg.Meanings {
					builder.WriteString(fmt.Sprintf(meaningFormat, tm))
					// Recursive call for the next level deep.
					builder.WriteString(getDeeperGlosses(tm, glosses, level+1))
				}
			}
		}
	}

	return builder.String()
}

// generateGlossText creates the formatted string for a word's details.
// This is used by both the main view and the reverse-find modal.
func generateGlossText(word string, glosses map[string][]Gloss) string {
	if glossSlice, ok := glosses[word]; ok {
		var formatted string

		for i, gloss := range glossSlice {
			if debug {
				log.Printf("generateGlossText: processing gloss[%d]: %s (%s)", i, gloss.Word, gloss.Pos)
			}
			if i > 0 {
				formatted += "\n"
			}
			formatted += fmt.Sprintf("[white]%s [yellow](%s)[white]\n\n", gloss.Word, gloss.Pos)
			for _, meaning := range gloss.Meanings {
				if debug {
					log.Printf("generateGlossText: processing meaning: %s", meaning)
				}
				formatted += fmt.Sprintf("- %s\n", meaning)

				// Call the recursive helper function to get all deeper glosses.
				formatted += getDeeperGlosses(meaning, glosses, 1)
			}
		}
		return formatted
	}

	if debug {
		log.Printf("generateGlossText: no gloss available for word: %s", word)
	}
	return fmt.Sprintf("%s\n\nNo gloss available.", word)
}

// ----------------------
// Go Deeper Loader and Prefix Lookup
// ----------------------

func loadDeeperPhrases() ([]string, error) {
	scanner := bufio.NewScanner(strings.NewReader(goDeeperTxt))
	var phrases []string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			phrases = append(phrases, line)
		}
	}
	return phrases, scanner.Err()
}

var (
	deeperPrefixMap     map[string]struct{}
	deeperPrefixLengths []int
)

// initDeeperPrefixes builds a hashmap for lookups where the keys are each phrase
// from go-deeper.txt with an appended space. It also builds a slice of key lengths,
// sorted in descending order so that the longest (most precise) prefix is matched first.
func initDeeperPrefixes() error {
	phrases, err := loadDeeperPhrases()
	if err != nil {
		return err
	}
	deeperPrefixMap = make(map[string]struct{}, len(phrases))
	lengthSet := make(map[int]struct{})
	for _, phrase := range phrases {
		key := phrase + " "
		deeperPrefixMap[key] = struct{}{}
		lengthSet[len(key)] = struct{}{}
	}
	for l := range lengthSet {
		deeperPrefixLengths = append(deeperPrefixLengths, l)
	}
	// Sort lengths in descending order.
	sort.Sort(sort.Reverse(sort.IntSlice(deeperPrefixLengths)))
	return nil
}

func findLongestPrefix(s string) (string, bool) {
	if debug {
		log.Printf("findLongestPrefix: Checking for prefixes which match '%s'", s)
	}

	// Split the input string into words.
	words := strings.Fields(s)

	// Start with the full set of words and remove one word at a time.
	for i := len(words); i > 0; i-- {
		// Join the first i words with a space and add a trailing space.
		candidate := strings.Join(words[:i], " ") + " "
		if debug {
			log.Printf("findLongestPrefix: Is '%s' in deeperPrefixMap?", candidate)
		}

		if _, ok := deeperPrefixMap[candidate]; ok {
			if debug {
				log.Printf("findLongestPrefix: Yes! Returning '%s' from deeperPrefixMap.", candidate)
			}
			return candidate, true
		}
	}

	return "", false
}

// ----------------------
// Utility: Open URL in default browser
// ----------------------

func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		return fmt.Errorf("unsupported platform")
	}
	return cmd.Start()
}

// ----------------------
// Utility: Clean up SQL terms properly
//

func cleanTerm(s string) string {
	// Trim off any leading/trailing non-letters
	start, end := 0, len(s)
	for start < end && !unicode.IsLetter(rune(s[start])) {
		start++
	}
	for end > start && !unicode.IsLetter(rune(s[end-1])) {
		end--
	}
	return s[start:end]
}

// showMeaningSearchModal creates and displays a modal window for searching word meanings.
// This modal is designed to look and feel like the main application window, with a
// two-pane layout for search/results and details.
// MODIFIED: Added mainInputField to the function signature to allow interaction with the main view.
func showMeaningSearchModal(pages *tview.Pages, glosses map[string][]Gloss, app *tview.Application, mainInputField *tview.InputField) {
	if debug {
		log.Println("showMeaningSearchModal: Function called.")
	}

	// --- NEW: Help text specific to this modal ---
	const reverseFindHelpText = `[gray]
	Keybindings:

	Enter       = Search for the English term.
	Up/Down     = Scroll result list.

	[green]Enter on a result[gray] in the list to select it and return to the main view.
	[red]Enter on an empty search bar[gray] to close this window and return to the main view.
	
	Unlike the normal Finnish lookup, this mode does *not* search as you type.
	You aren't supposed to stay here for long...

	[white]
	`

	// --- NEW: Color Theme for Modal ---
	const (
		// Main background set to a dark violet.
		modalBgColor = tcell.ColorDarkViolet

		// Header/footer set to an even darker purple for contrast.
		modalHeaderFooterBg = tcell.ColorIndigo
		modalDetailsBg      = tcell.ColorMidnightBlue

		// Text remains white for readability.
		modalPrimaryColor = tcell.ColorGold

		// Accents (borders, titles) are now a lighter purple.
		modalAccentColor = tcell.ColorPlum

		// Input field background is a muted purple.
		modalFieldBgColor = tcell.ColorRebeccaPurple

		// Selection colors for the list remain high-contrast.
		modalListSelectBg   = tcell.ColorIndigo
		modalListSelectText = tcell.ColorGold
	)

	// --- Components ---

	// Left Pane: Search Input & Results List
	searchInput := tview.NewInputField().
		SetLabel("English term: ").
		SetLabelColor(modalAccentColor).            // NEW: Color
		SetFieldBackgroundColor(modalFieldBgColor). // NEW: Color
		SetFieldTextColor(modalPrimaryColor).       // NEW: Color
		SetFieldWidth(30)

	resultsList := tview.NewList().
		ShowSecondaryText(false).
		SetSelectedBackgroundColor(modalListSelectBg). // NEW: Color
		SetSelectedTextColor(modalListSelectText)      // NEW: Color

	// Right Pane: Details Display
	detailsView := tview.NewTextView().
		SetDynamicColors(true).
		SetScrollable(true).
		SetWrap(true).
		SetWordWrap(true).
		SetTextColor(modalPrimaryColor)

	detailsView.SetBorder(true).
		SetTitle("Word Details (Tab/Shift-Tab to scroll)").
		SetBorderColor(modalAccentColor). // NEW: Color
		SetTitleColor(modalAccentColor)   // NEW: Color

	// Set the initial help text for this modal.
	detailsView.SetText(reverseFindHelpText)

	detailsView.SetBackgroundColor(modalDetailsBg)

	// --- Main Content Layout (The two panes) ---
	contentFlex := tview.NewFlex().
		SetDirection(tview.FlexColumn).
		AddItem(
			tview.NewFlex().SetDirection(tview.FlexRow).
				AddItem(searchInput, 3, 1, true).
				AddItem(resultsList, 0, 4, false),
			0, 1, true,
		).
		AddItem(detailsView, 0, 2, false)
	contentFlex.SetBackgroundColor(modalBgColor) // NEW: Set overall background

	// --- Header ---
	headerLeft := tview.NewTextView().
		SetText(fmt.Sprintf("tsk (%s) - Reverse-Find by English Meaning", version)).
		SetTextAlign(tview.AlignLeft).
		SetTextColor(modalPrimaryColor) // NEW: Color
	headerLeft.SetBackgroundColor(modalHeaderFooterBg)

	headerRight := tview.NewButton("[::u]https://github.com/hiAndrewQuinn/tsk[::-]")
	headerRight.SetLabelColor(modalPrimaryColor) // NEW: Color
	headerRight.SetSelectedFunc(func() {
		if err := openBrowser("https://github.com/hiAndrewQuinn/tsk"); err != nil {
			fmt.Fprintf(os.Stderr, "Error opening browser: %v\n", err)
		}
	})

	headerFlex := tview.NewFlex().SetDirection(tview.FlexColumn)
	headerFlex.SetBackgroundColor(modalHeaderFooterBg) // NEW: Color
	headerFlex.
		AddItem(headerLeft, 0, 1, false).
		AddItem(headerRight, 40, 0, false)

	// --- Footer ---
	footerLeft := tview.NewTextView().
		SetText("Esc to close. Enter to search. Up/Down to scroll results.").
		SetTextAlign(tview.AlignLeft).
		SetTextColor(modalPrimaryColor) // NEW: Color
	footerLeft.SetBackgroundColor(modalHeaderFooterBg)

	footerRight := tview.NewButton("[::u]https://andrew-quinn.me/[::-]")
	footerRight.SetLabelColor(modalPrimaryColor) // NEW: Color
	footerRight.SetSelectedFunc(func() {
		if err := openBrowser("https://andrew-quinn.me/"); err != nil {
			fmt.Fprintf(os.Stderr, "Error opening browser: %v\n", err)
		}
	})

	footerFlex := tview.NewFlex().SetDirection(tview.FlexColumn)
	footerFlex.SetBackgroundColor(modalHeaderFooterBg) // NEW: Color
	footerFlex.
		AddItem(footerLeft, 0, 1, false).
		AddItem(footerRight, 40, 0, false)

	// --- Final Modal Layout (mimicking mainFlex) ---
	modalLayout := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(headerFlex, 1, 0, false).
		AddItem(nil, 1, 0, false). // Spacer
		AddItem(contentFlex, 0, 1, true).
		AddItem(nil, 1, 0, false). // Spacer
		AddItem(footerFlex, 1, 0, false)
	modalLayout.SetBackgroundColor(modalBgColor) // NEW: Set overall background

	// --- Logic & Event Handlers ---

	searchAction := func() {
		if debug {
			log.Println("showMeaningSearchModal: searchAction triggered.")
		}
		query := strings.ToLower(strings.TrimSpace(searchInput.GetText()))
		if debug {
			log.Printf("showMeaningSearchModal: Cleaned query: '%s'", query)
		}

		resultsList.Clear()
		detailsView.Clear().ScrollToBeginning()

		// This check is now redundant because SetDoneFunc handles the empty case,
		// but it's harmless to leave as a safeguard.
		if query == "" {
			detailsView.SetText(reverseFindHelpText)
			return
		}

		foundMap := make(map[string]struct{})
		for word, glossSlice := range glosses {
			for _, gloss := range glossSlice {
				for _, meaning := range gloss.Meanings {
					if strings.Contains(strings.ToLower(meaning), query) {
						foundMap[word] = struct{}{}
						break
					}
				}
			}
		}

		if len(foundMap) == 0 {
			detailsView.SetText(fmt.Sprintf("[red]No words found with meaning containing '[darkred:%s]'.[white]", query))
		} else {
			matches := make([]string, 0, len(foundMap))
			for word := range foundMap {
				matches = append(matches, word)
			}
			sort.Strings(matches)

			for _, match := range matches {
				resultsList.AddItem(match, "", 0, nil)
			}
			resultsList.SetCurrentItem(0)
		}
	}

	resultsList.SetChangedFunc(func(index int, mainText, secondaryText string, shortcut rune) {
		glossText := generateGlossText(mainText, glosses)
		detailsView.SetText(glossText).ScrollToBeginning()
	})

	// NEW: Add a selection handler to the list.
	// When the user presses Enter on a list item, this function is called.
	resultsList.SetSelectedFunc(func(index int, mainText, secondaryText string, shortcut rune) {
		// Set the main application's search bar text to the selected word.
		mainInputField.SetText(mainText)
		// Close the modal.
		pages.RemovePage("meaningSearch")
		// Set focus back to the main input field for a seamless transition.
		app.SetFocus(mainInputField)
	})

	// MODIFIED: Updated the DoneFunc to handle exiting on an empty search.
	searchInput.SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEnter {
			query := strings.TrimSpace(searchInput.GetText())
			if query == "" {
				// If the search bar is empty, just close the modal.
				pages.RemovePage("meaningSearch")
			} else {
				// Otherwise, perform the search.
				searchAction()
			}
		}
	})

	searchInput.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		// MODIFIED: Give focus to the list on Down/Up arrow keys to enable selection.
		switch event.Key() {
		case tcell.KeyEsc:
			pages.RemovePage("meaningSearch")
			return nil
		case tcell.KeyDown:
			app.SetFocus(resultsList)
			cur := resultsList.GetCurrentItem()
			if cur < resultsList.GetItemCount()-1 {
				resultsList.SetCurrentItem(cur + 1)
			}
			return nil
		case tcell.KeyUp:
			app.SetFocus(resultsList)
			cur := resultsList.GetCurrentItem()
			if cur > 0 {
				resultsList.SetCurrentItem(cur - 1)
			}
			return nil
		case tcell.KeyTab:
			row, col := detailsView.GetScrollOffset()
			detailsView.ScrollTo(row+1, col)
			return nil
		case tcell.KeyBacktab:
			row, col := detailsView.GetScrollOffset()
			newRow := row - 1
			if newRow < 0 {
				newRow = 0
			}
			detailsView.ScrollTo(newRow, col)
			return nil
		}
		return event
	})

	// --- FIX #1: Add the modal to the pages view to make it visible. ---
	if debug {
		log.Println("showMeaningSearchModal: Adding 'meaningSearch' page to pages container.")
	}
	pages.AddPage("meaningSearch", modalLayout, true, true)
}

// ----------------------
// Main TUI Application
// ----------------------

func main() {
	fmt.Println(fmt.Sprintf("tsk (%s) - Andrew's Pocket Finnish Dictionary\n", version))
	fmt.Println("Project @ https://github.com/hiAndrewQuinn/tsk")
	fmt.Println("Author  @ https://andrew-quinn.me/\n")

	flag.Usage = printCustomUsage

	// Initialize global debug flag.
	flag.BoolVar(&debug, "debug", false, "print debug info")
	flag.Parse()

	// If debug mode is enabled, open (or create) the debug log file in append mode.
	if debug {
		debugFile, err := os.OpenFile("debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error opening debug log: %v\n", err)
			os.Exit(1)
		}
		defer debugFile.Close()
		log.SetOutput(debugFile)
		log.Println("Debug mode enabled")
	}

	// -------------------------------
	// NEW: CLI Mode Logic
	// -------------------------------
	var searchTerms []string

	// First, check for non-flag arguments.
	if len(flag.Args()) > 0 {
		searchTerms = flag.Args()
		if debug {
			log.Printf("CLI mode activated via arguments: %v", searchTerms)
		}
	} else {
		// If no arguments, check if data is being piped via stdin.
		stat, _ := os.Stdin.Stat()
		if (stat.Mode() & os.ModeCharDevice) == 0 {
			if debug {
				log.Println("CLI mode activated via stdin pipe.")
			}
			bytes, err := ioutil.ReadAll(os.Stdin)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error reading from stdin: %v\n", err)
				os.Exit(1)
			}
			// Assume space-separated words from the piped input.
			searchTerms = strings.Fields(string(bytes))
		}
	}

	// If we have terms from either args or stdin, run in CLI mode.
	if len(searchTerms) > 0 {
		// Suppress the loading messages for piped input to keep the output clean.
		if len(flag.Args()) > 0 {
			fmt.Println("Loading word definitions...")
			fmt.Println("Initializing deeper lookup prefixes...")
		}

		glosses, err := loadGlosses()
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error loading glosses:", err)
			os.Exit(1)
		}

		if err := initDeeperPrefixes(); err != nil {
			fmt.Fprintln(os.Stderr, "Error initializing deeper prefixes:", err)
			os.Exit(1)
		}

		fmt.Println("===")

		// Loop over all provided search terms.
		for i, term := range searchTerms {
			// Check if the word exists.
			if _, ok := glosses[term]; ok {
				// Generate the gloss text, strip color tags, and print.
				glossText := generateGlossText(term, glosses)
				cleanText := stripColorTags(glossText)
				fmt.Println(cleanText)
			} else {
				fmt.Printf("'%s' not found.\n", term)
			}

			// Print a separator between results, but not after the last one.
			if i < len(searchTerms)-1 {
				fmt.Println("---")
			}
		}

		fmt.Println("===")

		// Exit successfully, skipping the TUI.
		os.Exit(0)
	}
	// -------------------------------
	// End of CLI Mode Logic
	// -------------------------------

	// Load words from embedded data.
	fmt.Println("Loading words from", WORD_LIST_FILE)
	start := time.Now()
	words, err := loadWords()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error loading words:", err)
		os.Exit(1)
	}
	fmt.Printf("Loaded %d words in %v\n", len(words), time.Since(start))

	// Build trie.
	trie := NewTrie()
	start = time.Now()
	for _, word := range words {
		trie.Insert(word)
	}
	buildDuration := time.Since(start)
	fmt.Printf("Built trie in %v\n", buildDuration)

	// Track words the user explicitly marks.
	marked := make(map[string]struct{})

	// Debug info.
	if debug {
		totalNodes := trie.CountNodes()
		nodeStructSize := unsafe.Sizeof(TrieNode{})
		const estimatedMapOverhead = 48
		estimatedPerNode := int(nodeStructSize) + estimatedMapOverhead
		estimatedMemory := totalNodes * estimatedPerNode

		log.Printf("Debug: Trie has %d nodes\n", totalNodes)
		log.Printf("Debug: Estimated per-node memory usage: %d bytes\n", estimatedPerNode)
		log.Printf("Debug: Estimated total memory usage: %d bytes (~%.2f MB)\n",
			estimatedMemory, float64(estimatedMemory)/(1024*1024))
	}

	// Load glosses.
	start = time.Now() // Re-use the 'start' variable
	glosses, err := loadGlosses()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error loading glosses:", err)
		os.Exit(1)
	}
	fmt.Printf("Loaded word glosses from %s in %v\n", GLOSSES_FILE, time.Since(start))

	// Initialize deeper lookup prefixes.
	start = time.Now() // Re-use the 'start' variable again
	if err := initDeeperPrefixes(); err != nil {
		fmt.Fprintln(os.Stderr, "Error initializing deeper prefixes:", err)
		os.Exit(1)
	}
	fmt.Printf("Initialized deeper lookup prefixes from go-deeper.txt in %v\n", time.Since(start))

	// dump embeddedDB bytes into a temporary file for SQL lookups
	tmp, err := ioutil.TempFile("", "tsksentences-*.sqlite")
	if err != nil {
		log.Fatalf("could not create temp file: %v", err)
	}
	defer tmp.Close()

	if _, err := tmp.Write(embeddedDB); err != nil {
		log.Fatalf("could not write embedded DB: %v", err)
	}

	// open it via sqlite
	exampleDB, err := sql.Open("sqlite", tmp.Name()+"?_foreign_keys=on")
	if err != nil {
		log.Fatalf("could not open example sentences DB: %v", err)
	}

	fmt.Println("Starting the TUI. Thank you for your patience!")
	app := tview.NewApplication()
	pages := tview.NewPages()

	// -------------------------------
	// Header (Top Line)
	// -------------------------------
	headerLeft := tview.NewTextView().
		SetText(fmt.Sprintf("tsk (%s) - Andrew's Pocket Finnish Dictionary", version)).
		SetTextAlign(tview.AlignLeft).
		SetTextColor(tcell.ColorBlack)
	headerLeft.SetBackgroundColor(tcell.ColorLightGray)

	headerRight := tview.NewButton("[::u]https://github.com/hiAndrewQuinn/tsk[::-]")
	headerRight.SetLabelColor(tcell.ColorWhite)
	// Set the selected style to ensure light gray background with black text.
	headerRight.SetSelectedFunc(func() {
		if err := openBrowser("https://github.com/hiAndrewQuinn/tsk"); err != nil {
			fmt.Fprintf(os.Stderr, "Error opening browser: %v\n", err)
		}
	})

	headerFlex := tview.NewFlex().SetDirection(tview.FlexColumn)
	headerFlex.SetBackgroundColor(tcell.ColorLightGray)
	headerFlex.
		AddItem(headerLeft, 0, 1, false).
		AddItem(headerRight, 40, 0, false)

	// -------------------------------
	// Left Pane: Search Input & List
	// -------------------------------
	inputField := tview.NewInputField().SetLabel("Search: ").SetFieldWidth(30)
	list := tview.NewList().ShowSecondaryText(false)

	updateList := func(text string) {
		list.Clear()
		if text == "" {
			return
		}
		matches := trie.FindWords(text)
		for _, w := range matches {
			list.AddItem(w, "", 0, nil)
		}
		list.SetCurrentItem(0)
	}

	leftFlex := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(inputField, 3, 1, true).
		AddItem(list, 0, 4, false)

	// -------------------------------
	// Right Pane: Gloss Display
	// -------------------------------
	textView := tview.NewTextView()
	textView.SetDynamicColors(true)
	textView.SetWrap(true)
	textView.SetWordWrap(true)
	textView.SetBorder(true)
	textView.SetTitle("Word Details (Tab/Shift-Tab to scroll, Ctrl-S to mark)")
	// Set initial help text in gray.
	textView.SetText(helpText)

	displayGloss := func(word string) {
		if debug {
			log.Printf("displayGloss: called for word: %s", word)
		}

		// Handle marking visuals (title and border color)
		_, isMarked := marked[word]
		if isMarked {
			if debug {
				log.Printf("displayGloss: %s is marked.", word)
			}
			textView.SetTitle("Word Details (Tab/Shift-Tab to scroll, Ctrl-S to unmark)")
			textView.SetBorderColor(tcell.ColorYellow)
			textView.SetTitleColor(tcell.ColorYellow)
		} else {
			if debug {
				log.Printf("displayGloss: %s is NOT marked.", word)
			}
			textView.SetTitle("Word Details (Tab/Shift-Tab to scroll, Ctrl-S to mark)")
			textView.SetBorderColor(tcell.ColorWhite)
			textView.SetTitleColor(tcell.ColorWhite)
		}

		// Generate the content using the new helper and set it
		glossText := generateGlossText(word, glosses)
		textView.SetText(glossText)
	}

	list.SetChangedFunc(func(idx int, mainText string, _ string, _ rune) {
		// first show the gloss as before:
		displayGloss(mainText)

		// then pick selection style:
		if _, marked := marked[mainText]; marked {
			// “reverse-video” in yellow:
			list.SetSelectedBackgroundColor(tcell.ColorYellow)
		} else {
			// back to the List’s defaults
			list.SetSelectedBackgroundColor(tcell.ColorWhite)
		}
	})

	inputField.SetChangedFunc(func(text string) {
		updateList(text)
	})

	inputField.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyDown:
			cur := list.GetCurrentItem()
			if cur < list.GetItemCount()-1 {
				list.SetCurrentItem(cur + 1)
			}
			return nil
		case tcell.KeyUp:
			cur := list.GetCurrentItem()
			if cur > 0 {
				list.SetCurrentItem(cur - 1)
			}
			return nil
		case tcell.KeyEnter:
			inputField.SetText("")
			updateList("")
			return nil
		}
		return event
	})

	const debounceDuration = 100 * time.Millisecond
	var lastScrollTime time.Time

	app.SetMouseCapture(func(event *tcell.EventMouse, action tview.MouseAction) (*tcell.EventMouse, tview.MouseAction) {
		now := time.Now()
		if now.Sub(lastScrollTime) < debounceDuration {
			return nil, 0
		}
		lastScrollTime = now
		if app.GetFocus() == list {
			switch event.Buttons() {
			case tcell.WheelUp, tcell.WheelDown:
				cur := list.GetCurrentItem()
				if event.Buttons() == tcell.WheelUp && cur > 0 {
					list.SetCurrentItem(cur - 1)
				} else if event.Buttons() == tcell.WheelDown && cur < list.GetItemCount()-1 {
					list.SetCurrentItem(cur + 1)
				}
				return nil, 0
			}
		}
		return event, action
	})

	topFlex := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(leftFlex, 0, 1, true).
		AddItem(textView, 0, 2, false)

	// -------------------------------
	// Footer (Bottom Line)
	// -------------------------------
	footerLeft := tview.NewTextView().
		SetText("Esc to exit. Enter to clear the search. Up/Down to scroll. Wiktionary entries under CC BY-SA.").
		SetTextAlign(tview.AlignLeft).
		SetTextColor(tcell.ColorBlack)
	footerLeft.SetBackgroundColor(tcell.ColorLightGray)

	footerRight := tview.NewButton("[::u]https://andrew-quinn.me/[::-]")
	footerRight.SetLabelColor(tcell.ColorWhite)
	// Set the selected style for the footer button as well.
	footerRight.SetSelectedFunc(func() {
		if err := openBrowser("https://andrew-quinn.me/"); err != nil {
			fmt.Fprintf(os.Stderr, "Error opening browser: %v\n", err)
		}
	})

	footerFlex := tview.NewFlex().SetDirection(tview.FlexColumn)
	footerFlex.SetBackgroundColor(tcell.ColorLightGray)
	footerFlex.
		AddItem(footerLeft, 0, 1, false).
		AddItem(footerRight, 40, 0, false)

	// -------------------------------
	// Global Key Capture: Tab/Shift+Tab scrolling without focus change.
	// -------------------------------
	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyCtrlR:
			if debug {
				log.Println("Ctrl-R detected, opening bug report URL.")
			}
			url := "https://github.com/hiAndrewQuinn/tsk/issues/new"
			if err := openBrowser(url); err != nil {
				log.Printf("Error opening browser for bug report: %v", err)
			}
			return nil // Consume the event so it's not processed further.

		case tcell.KeyCtrlF:
			showMeaningSearchModal(pages, glosses, app, inputField)
			return nil
		case tcell.KeyCtrlT:
			if list.GetItemCount() == 0 {
				textView.SetBorderColor(tcell.ColorTeal)
				textView.SetTitleColor(tcell.ColorTeal)
				textView.SetTitle("No word selected. Kotimaa itkee...")
				textView.SetText(finnishFlag)
				return nil
			}

			idx := list.GetCurrentItem()
			word, _ := list.GetItemText(idx)

			// 1a) if the search bar is empty, show teal “please enter something” message
			if strings.TrimSpace(word) == "" {
				textView.SetBorderColor(tcell.ColorTeal)
				textView.SetTitleColor(tcell.ColorTeal)
				textView.SetTitle("No word entered. Kotimaa itkee...")
				textView.SetText(finnishFlag)
				textView.SetText("[teal]No word entered. Please type something in the search bar.[white]")
				return nil
			}

			phrase := `"` + cleanTerm(word) + `"`

			const q = `
        SELECT finnish, english
        FROM sentences
        WHERE sentences MATCH ? 
    `
			rows, err := exampleDB.Query(q, phrase)
			if err != nil {
				textView.SetText(fmt.Sprintf("Error querying examples: %v", err))
				textView.SetBorderColor(tcell.ColorRed)
				return nil
			}
			defer rows.Close()

			// 3) build output
			var buf strings.Builder
			found := false

			buf.WriteString("[white]Example sentences are from https://tatoeba.org and under CC BY 2.0 FR.\n\n")

			for rows.Next() {
				found = true

				var fin, eng string
				if err := rows.Scan(&fin, &eng); err != nil {
					continue
				}

				// Finnish in teal (no per-word highlight)
				buf.WriteString("[teal]" + fin + "\n")

				// English in pink
				buf.WriteString("[pink]" + eng + "\n\n")
			}

			if err := rows.Err(); err != nil {
				buf.WriteString(fmt.Sprintf("\nError reading rows: %v", err))
			}

			// 3a) if nothing was found, show a special message
			if !found {
				textView.SetBorderColor(tcell.ColorTeal)
				textView.SetTitleColor(tcell.ColorTeal)
				textView.SetTitle("No examples found")
				textView.SetText("[red]No Tatoeba example sentences found.[white]")
				return nil
			}

			// 4) display results
			textView.SetTitle(fmt.Sprintf("Examples for '%s' (Tab/Shift-Tab to scroll)", word))
			textView.SetBorderColor(tcell.ColorTeal)
			textView.SetTitleColor(tcell.ColorTeal)
			textView.SetText(buf.String())

			return nil
		case tcell.KeyCtrlH:
			textView.SetTitle("Word Details (Tab/Shift-Tab to scroll, Ctrl-S to mark)")
			textView.SetBorderColor(tcell.ColorWhite)
			textView.SetTitleColor(tcell.ColorWhite)
			textView.SetText(helpText)
			return nil
		case tcell.KeyCtrlL:
			textView.SetBorderColor(tcell.ColorGreen)
			textView.SetTitleColor(tcell.ColorGreen)

			count := len(marked)
			if count == 0 {
				textView.SetTitle("Marked words list empty. Kotimaa itkee...")
				textView.SetText(finnishFlag)
			} else {
				textView.SetTitle(fmt.Sprintf("Listing marked words. (count: %d)", count))
				textView.SetBorderColor(tcell.ColorGreen)
				textView.SetTitleColor(tcell.ColorGreen)

				// build a sorted slice of the set
				var words []string
				for w := range marked {
					words = append(words, w)
				}
				sort.Strings(words)

				// render them in green
				builder := strings.Builder{}
				builder.WriteString("[green]")
				for _, w := range words {
					builder.WriteString(w)
					builder.WriteByte('\n')
				}
				builder.WriteString("[white]")

				builder.WriteByte('\n')
				builder.WriteByte('\n')
				builder.WriteString("[gray]Caution: The exported files [red]do NOT[gray] include any \"go-deeper\" words or phrases.")
				builder.WriteByte('\n')
				builder.WriteByte('\n')
				builder.WriteString("[gray]For example, marking '[yellow]omenan[gray]' [red]will NOT[gray] include any info about '[yellow]omena[gray]'.")
				builder.WriteByte('\n')
				builder.WriteByte('\n')
				builder.WriteString("If you want those go-deeper phrases in the export, please add them separately.[white]")

				textView.SetText(builder.String())
			}
			return nil
		case tcell.KeyCtrlS:
			if list.GetItemCount() == 0 {
				textView.SetText("\n  [red]You need to search for something before you can mark or unmark it.[white]")
				textView.SetTitle("Word Details (Tab/Shift-Tab to scroll, Ctrl-S to mark)")
				textView.SetBorderColor(tcell.ColorRed)
				textView.SetTitleColor(tcell.ColorRed)
				return nil
			}
			idx := list.GetCurrentItem()
			word, _ := list.GetItemText(idx)

			inputField.SetText(word)

			if _, present := marked[word]; present {
				delete(marked, word)
				if debug {
					log.Printf("Unmarking %s.", word)
				}
			} else {
				marked[word] = struct{}{}
				if debug {
					log.Printf("Marking %s.", word)
				}
			}
			updateList(inputField.GetText())
			return nil
		case tcell.KeyTab:
			// Scroll down one line in the textView.
			currentRow, currentCol := textView.GetScrollOffset()
			textView.ScrollTo(currentRow+1, currentCol)
			return nil // swallow event
		case tcell.KeyBacktab:
			// Scroll up one line in the textView.
			currentRow, currentCol := textView.GetScrollOffset()
			newRow := currentRow - 1
			if newRow < 0 {
				newRow = 0
			}
			textView.ScrollTo(newRow, currentCol)
			return nil // swallow event
		case tcell.KeyEsc:
			app.Stop()
			fmt.Println("Stopping the TUI. Thank you for exiting gracefully!")

			// 1) If nothing’s marked, just exit.
			if len(marked) == 0 {
				return nil
			}

			// 2) Build base filename with timestamp
			ts := time.Now().Format("2006-01-02-15-04-05")
			base := fmt.Sprintf("tsk-marked_%s", ts)
			jsonFile := base + ".jsonl"
			txtFile := base + ".txt"

			// --- JSONL dump ---
			fj, err := os.Create(jsonFile)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error creating %s: %v\n", jsonFile, err)
				os.Exit(1)
			}
			defer fj.Close()

			for wform := range marked {
				if glossSlice, ok := glosses[wform]; ok {
					for _, gloss := range glossSlice {
						line, err := json.Marshal(gloss)
						if err != nil {
							fmt.Fprintf(os.Stderr,
								"Error marshaling gloss for %s: %v\n",
								wform, err,
							)
							continue
						}
						if _, err := fj.Write(append(line, '\n')); err != nil {
							fmt.Fprintf(os.Stderr,
								"Error writing to %s: %v\n",
								jsonFile, err,
							)
							os.Exit(1)
						}
					}
				}
			}
			fmt.Printf("Saved %d words’ gloss entries to %s\n", len(marked), jsonFile)

			// --- TXT (one-column CSV) dump ---
			// We’ll use encoding/csv to get proper quoting, but it's just one column.
			ft, err := os.Create(txtFile)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error creating %s: %v\n", txtFile, err)
				os.Exit(1)
			}
			defer ft.Close()

			cw := csv.NewWriter(ft)
			defer cw.Flush()

			// Header
			cw.Write([]string{"Base Form"})

			// Collect & sort keys
			var words []string
			for w := range marked {
				words = append(words, w)
			}
			sort.Strings(words)

			// One row per word
			for _, w := range words {
				cw.Write([]string{w})
			}

			fmt.Printf("Saved %d marked words to %s\n", len(words), txtFile)

			return nil
		default:
			return event
		}
	})

	// -------------------------------
	// Main Layout
	// -------------------------------
	mainFlex := tview.NewFlex().
		SetDirection(tview.FlexRow).
		// Top row (header)
		AddItem(headerFlex, 1, 0, false).
		// Spacer for a black bar
		AddItem(nil, 1, 0, false).
		// Main content (search + list + details)
		AddItem(topFlex, 0, 1, true).
		// Spacer for a black bar
		AddItem(nil, 1, 0, false).
		// Bottom row (footer)
		AddItem(footerFlex, 1, 0, false)

	// --- FIX #2 & #3: Add the mainFlex as the "main" page, and remove the invalid modalLayout call.
	pages.AddPage("main", mainFlex, true, true)

	if err := app.SetRoot(pages, true).Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
