package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
	"unsafe"

	_ "embed"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// ----------------------
// Embedded Data Files
// ----------------------

//go:embed finwords.txt
var finwordsTxt string

//go:embed glosses.jsonl
var glossesJsonl string

// ----------------------
// Constants
// ----------------------

const (
	TRIE_MAX_SEARCH_DEPTH = 50 // Maximum number of words to return

	// Informational only.
	WORD_LIST_FILE = "finwords.txt"
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
	scanner := bufio.NewScanner(strings.NewReader(finwordsTxt))
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
	debugFlag := flag.Bool("debug", false, "print debug info")
	flag.Parse()

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

	// Debug info.
	if *debugFlag {
		totalNodes := trie.CountNodes()
		nodeStructSize := unsafe.Sizeof(TrieNode{})
		const estimatedMapOverhead = 48
		estimatedPerNode := int(nodeStructSize) + estimatedMapOverhead
		estimatedMemory := totalNodes * estimatedPerNode

		fmt.Fprintf(os.Stderr, "Debug: Trie has %d nodes\n", totalNodes)
		fmt.Fprintf(os.Stderr, "Debug: Estimated per-node memory usage: %d bytes\n", estimatedPerNode)
		fmt.Fprintf(os.Stderr, "Debug: Estimated total memory usage: %d bytes (~%.2f MB)\n",
			estimatedMemory, float64(estimatedMemory)/(1024*1024))
	}

	// Load glosses.
	fmt.Println("Loading glosses from", GLOSSES_FILE)
	glosses, err := loadGlosses()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error loading glosses:", err)
		os.Exit(1)
	}

	app := tview.NewApplication()

	// Global key capture: Pressing Esc exits.
	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEsc {
			app.Stop()
			return nil
		}
		return event
	})

	// -------------------------------
	// Header (Top Line)
	// -------------------------------
	// Left part: Title in reverse video using markup.
	headerLeft := tview.NewTextView()
	headerLeft.SetDynamicColors(true)
	headerLeft.SetText("[reverse]tsk - Andrew's Pocket Finnish Dictionary[::-]")
	headerLeft.SetTextAlign(tview.AlignLeft)

	// Right part: Button with underlined link.
	headerRight := tview.NewButton("[::u]https://andrew-quinn.me[::-]")
	headerRight.SetSelectedFunc(func() {
		if err := openBrowser("https://andrew-quinn.me"); err != nil {
			fmt.Fprintf(os.Stderr, "Error opening browser: %v\n", err)
		}
	})

	headerFlex := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(headerLeft, 0, 1, false).
		AddItem(headerRight, 30, 0, false)

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
	textView.SetTitle("Word Details")

	displayGloss := func(word string) {
		if glossSlice, ok := glosses[word]; ok {
			var formatted string
			for i, gloss := range glossSlice {
				if i > 0 {
					formatted += "\n"
				}
				formatted += fmt.Sprintf("%s (%s)\n\n", gloss.Word, gloss.Pos)
				for _, meaning := range gloss.Meanings {
					formatted += fmt.Sprintf("- %s\n", meaning)
				}
			}
			textView.SetText(formatted)
		} else {
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
	footer := tview.NewTextView()
	footer.SetText("Esc to exit. Enter to clear the search. Wiktionary entries under CC BY-SA.").
		SetTextAlign(tview.AlignCenter)

	// -------------------------------
	// Main Layout: Combine Header, Center, and Footer
	// -------------------------------
	mainFlex := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(headerFlex, 1, 0, false).
		AddItem(topFlex, 0, 1, true).
		AddItem(footer, 1, 0, false)

	if err := app.SetRoot(mainFlex, true).Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
