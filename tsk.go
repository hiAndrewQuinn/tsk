package main

import (
	"bufio"
	"encoding/json"
	"encoding/csv"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"time"
	"unsafe"

	_ "embed"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// ----------------------
// Version Variable
// ----------------------
const version = "v0.0.5"

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

	  [yellow]Control-S[gray]  = [yellow]Mark[gray]/unmark words. All marked words will be saved upon Esc to a text file.
	  [green]Control-L[gray]  = [green]List[gray] marked words. 
	  [pink]Control-H[gray]  = Show this [pink]help[gray] text again.
	[white]
	`

const finnishFlag = `[green]
	                            ..
                        _,-(.;)
                    _,-',gtP""
                _,-',gtP",'|
            _,-',gtP" ,-" :|
        _,-',gtP" _,a"   .'|
itz _,-',gtP"_,aS####    : |
_,-',gtP;-'"~. i#H##9   :' |
,gtP"   |   :  jH###j  ,. _|
"       |  :   QH##H( .;sQ#|
        |:'.   #####k,6####|
        |..   ;##H#H#######|
        ":_,J#############H|
         |H#H####H#####F'~ |
         |H#####HH###f".   |
        .J###PFH#H##':     |
        :#F".  #H###. '    |
        | :'   H####l.    .|
        |.'    #####h      |
        |.     V####C    ':|
        ":     t####Q   .:.|
         |    ."###H#    ._|
         |     :####H_,-'""
         |   '.,#JF""
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
// Main TUI Application
// ----------------------

func main() {
	fmt.Println(fmt.Sprintf("tsk (%s) - Andrew's Pocket Finnish Dictionary\n", version))
	fmt.Println("Project @ https://github.com/hiAndrewQuinn/tsk")
	fmt.Println("Author  @ https://andrew-quinn.me/\n")

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
	fmt.Println("Loading word definitions from", GLOSSES_FILE)
	glosses, err := loadGlosses()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error loading glosses:", err)
		os.Exit(1)
	}

	// Initialize deeper lookup prefixes.
	fmt.Println("Initializing deeper lookup prefixes from go-deeper.txt")
	if err := initDeeperPrefixes(); err != nil {
		fmt.Fprintln(os.Stderr, "Error initializing deeper prefixes:", err)
		os.Exit(1)
	}

	fmt.Println("Starting the TUI. Thank you for your patience!")
	app := tview.NewApplication()

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

		if glossSlice, ok := glosses[word]; ok {
			var formatted string

			for i, gloss := range glossSlice {
				if debug {
					log.Printf("displayGloss: processing gloss[%d]: %s (%s)", i, gloss.Word, gloss.Pos)
				}
				if i > 0 {
					formatted += "\n"
				}
				formatted += fmt.Sprintf("%s (%s)\n\n", gloss.Word, gloss.Pos)
				for _, meaning := range gloss.Meanings {
					if debug {
						log.Printf("displayGloss: processing meaning: %s", meaning)
					}
					formatted += fmt.Sprintf("- %s\n", meaning)

					// First-level deep lookup using hashmap-based prefix matching.
					if prefix, found := findLongestPrefix(meaning); found {
						if debug {
							log.Printf("displayGloss: first-level deep lookup: found prefix '%s' in meaning '%s'", prefix, meaning)
						}

						target := strings.TrimRight(strings.TrimSpace(strings.TrimPrefix(meaning, prefix)), ".,:;!?")
						if idx := strings.Index(target, "("); idx != -1 {
							target = strings.TrimSpace(target[:idx])
							if debug {
								log.Printf("displayGloss: Removing the parentheses. We will just search for '%s'.", target)
							}
						}
						if idx := strings.Index(target, ";"); idx != -1 {
							target = strings.TrimSpace(target[:idx])
							if debug {
								log.Printf("displayGloss: Removing the semicolon. We will just search for '%s'.", target)
							}
						}
						if debug {
							log.Printf("displayGloss: first-level deep lookup: target after trimming: '%s'", target)
						}
						if targetGlosses, ok := glosses[target]; ok {
							if debug {
								log.Printf("displayGloss: first-level deep lookup: found glosses for target '%s'", target)
							}
							for _, tg := range targetGlosses {
								if debug {
									log.Printf("displayGloss: processing first-level target gloss: %s (%s)", tg.Word, tg.Pos)
								}
								formatted += fmt.Sprintf("[lightgray]  ~> %s (%s)[white]\n", tg.Word, tg.Pos)
								for _, tm := range tg.Meanings {
									if debug {
										log.Printf("displayGloss: processing first-level target meaning: %s", tm)
									}
									formatted += fmt.Sprintf("[lightgray]     - %s[white]\n", tm)

									// Second-level deep lookup.
									if prefix2, found2 := findLongestPrefix(tm); found2 {
										if debug {
											log.Printf("displayGloss: second-level deep lookup: found prefix '%s' in meaning '%s'", prefix2, tm)
										}
										target2 := strings.TrimRight(strings.TrimSpace(strings.TrimPrefix(tm, prefix2)), ".,:;!?")
										if idx := strings.Index(target2, "("); idx != -1 {
											target2 = strings.TrimSpace(target2[:idx])
											if debug {
												log.Printf("displayGloss: Removing the parentheses. We will just search for '%s'.", target2)
											}
										}
										if idx := strings.Index(target2, ";"); idx != -1 {
											target2 = strings.TrimSpace(target2[:idx])
											if debug {
												log.Printf("displayGloss: Removing the semicolon. We will just search for '%s'.", target2)
											}
										}
										if debug {
											log.Printf("displayGloss: second-level deep lookup: target after trimming: '%s'", target2)
										}
										if targetGlosses2, ok := glosses[target2]; ok {
											if debug {
												log.Printf("displayGloss: second-level deep lookup: found glosses for target '%s'", target2)
											}
											for _, tg2 := range targetGlosses2 {
												if debug {
													log.Printf("displayGloss: processing second-level target gloss: %s (%s)", tg2.Word, tg2.Pos)
												}
												formatted += fmt.Sprintf("[gray]       ~> %s (%s)[white]\n", tg2.Word, tg2.Pos)
												for _, tm2 := range tg2.Meanings {
													if debug {
														log.Printf("displayGloss: processing second-level target meaning: %s", tm2)
													}
													formatted += fmt.Sprintf("[gray]          - %s[white]\n", tm2)
												}
											}
										} else {
											if debug {
												log.Printf("displayGloss: second-level deep lookup: no glosses found for target '%s'", target2)
											}
										}
									}
								}
							}
						} else {
							if debug {
								log.Printf("displayGloss: first-level deep lookup: no glosses found for target '%s'", target)
							}
						}
					}
				}
			}
			textView.SetText(formatted)
		} else {
			if debug {
				log.Printf("displayGloss: no gloss available for word: %s", word)
			}
			textView.SetText(fmt.Sprintf("%s\n\nNo gloss available.", word))
		}
	}

	list.SetChangedFunc(func(index int, mainText string, secondaryText string, shortcut rune) {
		displayGloss(mainText)
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
				log.Printf("Unmarking %s.", word)
			} else {
				marked[word] = struct{}{}
				log.Printf("Marking %s.", word)
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
			txtFile  := base + ".txt"

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

	if err := app.SetRoot(mainFlex, true).Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
