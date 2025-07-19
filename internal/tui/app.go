// Package tui provides the terminal user interface for the application.
// It encapsulates all tview components and logic for the interactive mode.
package tui

import (
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	// Import your internal packages.
	// The paths must match your module structure.
	"github.com/hiAndrewQuinn/tsk/internal/data"
	"github.com/hiAndrewQuinn/tsk/internal/trie"
)

// App encapsulates all the components and state of the TUI.
type App struct {
	// tview components
	app         *tview.Application
	pages       *tview.Pages
	inputField  *tview.InputField
	wordList    *tview.List
	detailsView *tview.TextView

	// Data and state
	version       string
	debug         bool
	trie          *trie.Trie
	glosses       map[string][]data.Gloss
	prefixMatcher *data.PrefixMatcher
	exampleDB     *sql.DB
	inflectionsDB *sql.DB
	markedWords   map[string]struct{}
}

// NewApp creates, initializes, and returns a new TUI application instance.
// It takes all the pre-loaded data as dependencies.
func NewApp(version string, debug bool, wordTrie *trie.Trie, glossData map[string][]data.Gloss, matcher *data.PrefixMatcher, exDB, inflDB *sql.DB) *App {
	a := &App{
		app:           tview.NewApplication(),
		pages:         tview.NewPages(),
		version:       version,
		debug:         debug,
		trie:          wordTrie,
		glosses:       glossData,
		prefixMatcher: matcher,
		exampleDB:     exDB,
		inflectionsDB: inflDB,
		markedWords:   make(map[string]struct{}),
	}

	a.setupUI()
	a.setupGlobalInputCapture()

	return a
}

// Run starts the TUI application event loop.
func (a *App) Run() error {
	fmt.Println("Starting the TUI. Thank you for your patience!")
	if err := a.app.SetRoot(a.pages, true).Run(); err != nil {
		return err
	}

	// This code runs after the TUI has been stopped.
	fmt.Println("Stopping the TUI. Thank you for exiting gracefully!")
	return a.saveMarkedWords()
}

// setupUI initializes all tview widgets and lays them out.
func (a *App) setupUI() {
	// --- Header ---
	header := a.createHeader()

	// --- Left Pane: Search Input & List ---
	a.inputField = tview.NewInputField().SetLabel("Search: ").SetFieldWidth(30)
	a.wordList = tview.NewList().ShowSecondaryText(false)
	leftFlex := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(a.inputField, 3, 1, true).
		AddItem(a.wordList, 0, 4, false)

	// --- Right Pane: Details Display ---
	// FIX: Initialize the TextView first, then set the border.
	a.detailsView = tview.NewTextView().
		SetDynamicColors(true).
		SetWrap(true).
		SetWordWrap(true)
	a.detailsView.SetBorder(true)
	a.showHelp() // Show help text on startup

	// --- Main Layout ---
	topFlex := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(leftFlex, 0, 1, true).
		AddItem(a.detailsView, 0, 2, false)

	// --- Footer ---
	footer := a.createFooter()

	// --- Final Assembly ---
	mainFlex := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(header, 1, 0, false).
		AddItem(nil, 1, 0, false). // Spacer
		AddItem(topFlex, 0, 1, true).
		AddItem(nil, 1, 0, false). // Spacer
		AddItem(footer, 1, 0, false)

	a.pages.AddPage("main", mainFlex, true, true)

	// --- Setup Widget Handlers ---
	a.setupWidgetHandlers()
}

// setupWidgetHandlers configures the event handlers for the interactive widgets.
func (a *App) setupWidgetHandlers() {
	// When the selected item in the word list changes
	a.wordList.SetChangedFunc(func(idx int, mainText string, _ string, _ rune) {
		a.displayGloss(mainText)
		if _, marked := a.markedWords[mainText]; marked {
			a.wordList.SetSelectedBackgroundColor(tcell.ColorYellow)
		} else {
			a.wordList.SetSelectedBackgroundColor(tcell.ColorWhite)
		}
	})

	// When the text in the input field changes
	a.inputField.SetChangedFunc(func(text string) {
		a.updateWordList(text)
	})

	// Capture input for the search field (up/down arrows, enter)
	a.inputField.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyDown:
			cur := a.wordList.GetCurrentItem()
			if cur < a.wordList.GetItemCount()-1 {
				a.wordList.SetCurrentItem(cur + 1)
			}
			return nil
		case tcell.KeyUp:
			cur := a.wordList.GetCurrentItem()
			if cur > 0 {
				a.wordList.SetCurrentItem(cur - 1)
			}
			return nil
		case tcell.KeyEnter:
			a.inputField.SetText("")
			a.updateWordList("")
			return nil
		}
		return event
	})

	// Debounced mouse scroll handler
	var lastScrollTime time.Time
	const debounceDuration = 100 * time.Millisecond
	a.app.SetMouseCapture(func(event *tcell.EventMouse, action tview.MouseAction) (*tcell.EventMouse, tview.MouseAction) {
		if a.app.GetFocus() == a.wordList {
			now := time.Now()
			if now.Sub(lastScrollTime) < debounceDuration {
				return nil, 0
			}
			lastScrollTime = now
			switch event.Buttons() {
			case tcell.WheelUp:
				cur := a.wordList.GetCurrentItem()
				if cur > 0 {
					a.wordList.SetCurrentItem(cur - 1)
				}
				return nil, 0
			case tcell.WheelDown:
				cur := a.wordList.GetCurrentItem()
				if cur < a.wordList.GetItemCount()-1 {
					a.wordList.SetCurrentItem(cur + 1)
				}
				return nil, 0
			}
		}
		return event, action
	})
}

// setupGlobalInputCapture sets up the application-wide key bindings.
func (a *App) setupGlobalInputCapture() {
	a.app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		// If a modal is visible, don't process global hotkeys.
		if a.pages.HasPage("meaningSearch") || a.pages.HasPage("inflectionSearch") {
			return event
		}

		switch event.Key() {
		case tcell.KeyEsc:
			a.app.Stop()
			return nil
		case tcell.KeyCtrlR:
			openBrowser("https://github.com/hiAndrewQuinn/tsk/issues/new")
			return nil
		case tcell.KeyCtrlF:
			a.showMeaningSearchModal()
			return nil
		case tcell.KeyCtrlE:
			a.showInflectionSearchModal()
			return nil
		case tcell.KeyCtrlT:
			a.showExampleSentences()
			return nil
		case tcell.KeyCtrlH:
			a.showHelp()
			return nil
		case tcell.KeyCtrlL:
			a.listMarkedWords()
			return nil
		case tcell.KeyCtrlS:
			a.toggleMarkedWord()
			return nil
		case tcell.KeyTab:
			row, col := a.detailsView.GetScrollOffset()
			a.detailsView.ScrollTo(row+1, col)
			return nil
		case tcell.KeyBacktab:
			row, col := a.detailsView.GetScrollOffset()
			if row > 0 {
				a.detailsView.ScrollTo(row-1, col)
			}
			return nil
		}
		return event
	})
}

// updateWordList clears and repopulates the word list based on the search text.
func (a *App) updateWordList(text string) {
	a.wordList.Clear()
	if text != "" {
		matches := a.trie.FindWords(text)
		for _, w := range matches {
			a.wordList.AddItem(w, "", 0, nil)
		}
	}
	a.wordList.SetCurrentItem(0)
}

// displayGloss shows the definition for the given word in the details view.
func (a *App) displayGloss(word string) {
	if a.debug {
		log.Printf("displayGloss: called for word: %s", word)
	}

	_, isMarked := a.markedWords[word]
	if isMarked {
		a.detailsView.SetTitle("Word Details (Tab/Shift-Tab to scroll, Ctrl-S to unmark)")
		a.detailsView.SetBorderColor(tcell.ColorYellow)
		a.detailsView.SetTitleColor(tcell.ColorYellow)
	} else {
		a.detailsView.SetTitle("Word Details (Tab/Shift-Tab to scroll, Ctrl-S to mark)")
		a.detailsView.SetBorderColor(tcell.ColorWhite)
		a.detailsView.SetTitleColor(tcell.ColorWhite)
	}

	glossText := data.GenerateGlossText(word, a.glosses, a.prefixMatcher)
	a.detailsView.SetText(glossText).ScrollToBeginning()
}

// saveMarkedWords writes the list of marked words to timestamped .jsonl and .txt files.
func (a *App) saveMarkedWords() error {
	if len(a.markedWords) == 0 {
		return nil // Nothing to save.
	}

	ts := time.Now().Format("2006-01-02-15-04-05")
	base := fmt.Sprintf("tsk-marked_%s", ts)
	jsonFile := base + ".jsonl"
	txtFile := base + ".txt"

	// --- JSONL dump ---
	fj, err := os.Create(jsonFile)
	if err != nil {
		return fmt.Errorf("error creating %s: %w", jsonFile, err)
	}
	defer fj.Close()

	for wform := range a.markedWords {
		if glossSlice, ok := a.glosses[wform]; ok {
			for _, gloss := range glossSlice {
				line, err := json.Marshal(gloss)
				if err != nil {
					log.Printf("Error marshaling gloss for %s: %v\n", wform, err)
					continue
				}
				if _, err := fj.Write(append(line, '\n')); err != nil {
					return fmt.Errorf("error writing to %s: %w", jsonFile, err)
				}
			}
		}
	}
	fmt.Printf("Saved %d words’ gloss entries to %s\n", len(a.markedWords), jsonFile)

	// --- TXT (one-column CSV) dump ---
	ft, err := os.Create(txtFile)
	if err != nil {
		return fmt.Errorf("error creating %s: %w", txtFile, err)
	}
	defer ft.Close()

	cw := csv.NewWriter(ft)
	defer cw.Flush()

	cw.Write([]string{"Base Form"}) // Header
	sortedWords := make([]string, 0, len(a.markedWords))
	for w := range a.markedWords {
		sortedWords = append(sortedWords, w)
	}
	sort.Strings(sortedWords)

	for _, w := range sortedWords {
		cw.Write([]string{w})
	}

	fmt.Printf("Saved %d marked words to %s\n", len(sortedWords), txtFile)
	return nil
}

// ----------------------
// UI Creation Methods
// ----------------------

func (a *App) createHeader() *tview.Flex {
	headerLeft := tview.NewTextView().
		SetText(fmt.Sprintf("tsk (%s) - Andrew's Pocket Finnish Dictionary", a.version)).
		SetTextAlign(tview.AlignLeft).
		SetTextColor(tcell.ColorBlack)
	headerLeft.SetBackgroundColor(tcell.ColorLightGray)

	headerRight := tview.NewButton("[::u]https://github.com/hiAndrewQuinn/tsk[::-]")
	headerRight.SetLabelColor(tcell.ColorWhite)
	headerRight.SetSelectedFunc(func() {
		openBrowser("https://github.com/hiAndrewQuinn/tsk")
	})

	headerFlex := tview.NewFlex().SetDirection(tview.FlexColumn)
	headerFlex.SetBackgroundColor(tcell.ColorLightGray)
	headerFlex.
		AddItem(headerLeft, 0, 1, false).
		AddItem(headerRight, 40, 0, false)
	return headerFlex
}

func (a *App) createFooter() *tview.Flex {
	footerLeft := tview.NewTextView().
		SetText("Esc to exit. Enter to clear search. Up/Down to scroll. Wiktionary entries under CC BY-SA.").
		SetTextAlign(tview.AlignLeft).
		SetTextColor(tcell.ColorBlack)
	footerLeft.SetBackgroundColor(tcell.ColorLightGray)

	footerRight := tview.NewButton("[::u]https://andrew-quinn.me/[::-]")
	footerRight.SetLabelColor(tcell.ColorWhite)
	footerRight.SetSelectedFunc(func() {
		openBrowser("https://andrew-quinn.me/")
	})

	footerFlex := tview.NewFlex().SetDirection(tview.FlexColumn)
	footerFlex.SetBackgroundColor(tcell.ColorLightGray)
	footerFlex.
		AddItem(footerLeft, 0, 1, false).
		AddItem(footerRight, 40, 0, false)
	return footerFlex
}

// ----------------------
// Action/Handler Methods
// ----------------------

func (a *App) showHelp() {
	a.detailsView.SetTitle("Word Details (Tab/Shift-Tab to scroll, Ctrl-S to mark)")
	a.detailsView.SetBorderColor(tcell.ColorWhite)
	a.detailsView.SetTitleColor(tcell.ColorWhite)
	a.detailsView.SetText(helpText)
}

func (a *App) toggleMarkedWord() {
	if a.wordList.GetItemCount() == 0 {
		a.detailsView.SetText("\n  [red]You need to search for something before you can mark or unmark it.[white]")
		a.detailsView.SetTitle("Word Details (Tab/Shift-Tab to scroll, Ctrl-S to mark)")
		a.detailsView.SetBorderColor(tcell.ColorRed)
		a.detailsView.SetTitleColor(tcell.ColorRed)
		return
	}
	idx := a.wordList.GetCurrentItem()
	word, _ := a.wordList.GetItemText(idx)

	a.inputField.SetText(word)

	if _, present := a.markedWords[word]; present {
		delete(a.markedWords, word)
		if a.debug {
			log.Printf("Unmarking %s.", word)
		}
	} else {
		a.markedWords[word] = struct{}{}
		if a.debug {
			log.Printf("Marking %s.", word)
		}
	}
	a.updateWordList(a.inputField.GetText())
}

func (a *App) listMarkedWords() {
	a.detailsView.SetBorderColor(tcell.ColorGreen)
	a.detailsView.SetTitleColor(tcell.ColorGreen)

	count := len(a.markedWords)
	if count == 0 {
		a.detailsView.SetTitle("Marked words list empty. Kotimaa itkee...")
		a.detailsView.SetText(finnishFlag)
	} else {
		a.detailsView.SetTitle(fmt.Sprintf("Listing marked words. (count: %d)", count))

		sortedWords := make([]string, 0, len(a.markedWords))
		for w := range a.markedWords {
			sortedWords = append(sortedWords, w)
		}
		sort.Strings(sortedWords)

		var builder strings.Builder
		builder.WriteString("[green]")
		for _, w := range sortedWords {
			builder.WriteString(w)
			builder.WriteByte('\n')
		}
		builder.WriteString("[white]")
		builder.WriteString("\n\n[gray]Note: Exported files do not include 'go-deeper' phrases automatically.[white]")
		a.detailsView.SetText(builder.String())
	}
}

func (a *App) showExampleSentences() {
	if a.wordList.GetItemCount() == 0 {
		a.detailsView.SetBorderColor(tcell.ColorTeal)
		a.detailsView.SetTitleColor(tcell.ColorTeal)
		a.detailsView.SetTitle("No word selected. Kotimaa itkee...")
		a.detailsView.SetText(finnishFlag)
		return
	}

	idx := a.wordList.GetCurrentItem()
	word, _ := a.wordList.GetItemText(idx)

	if strings.TrimSpace(word) == "" {
		a.detailsView.SetText("[teal]No word entered. Please type something in the search bar.[white]")
		return
	}

	phrase := `"` + cleanTerm(word) + `"`
	q := `SELECT finnish, english FROM sentences WHERE sentences MATCH ?`
	rows, err := a.exampleDB.Query(q, phrase)
	if err != nil {
		a.detailsView.SetText(fmt.Sprintf("Error querying examples: %v", err))
		a.detailsView.SetBorderColor(tcell.ColorRed)
		return
	}
	defer rows.Close()

	var buf strings.Builder
	found := false
	buf.WriteString("[white]Example sentences from https://tatoeba.org (CC BY 2.0 FR).\n\n")

	for rows.Next() {
		found = true
		var fin, eng string
		if err := rows.Scan(&fin, &eng); err != nil {
			continue
		}
		buf.WriteString("[teal]" + fin + "\n")
		buf.WriteString("[pink]" + eng + "\n\n")
	}

	if !found {
		a.detailsView.SetTitle("No examples found")
		a.detailsView.SetText("[red]No Tatoeba example sentences found.[white]")
	} else {
		a.detailsView.SetTitle(fmt.Sprintf("Examples for '%s' (Tab/Shift-Tab to scroll)", word))
		a.detailsView.SetText(buf.String())
	}
	a.detailsView.SetBorderColor(tcell.ColorTeal)
	a.detailsView.SetTitleColor(tcell.ColorTeal)
}

// ----------------------
// Modal Implementations
// ----------------------

func (a *App) showInflectionSearchModal() {
	if a.inflectionsDB == nil {
		a.detailsView.SetTitle("Inflection Search Unavailable")
		a.detailsView.SetBorderColor(tcell.ColorRed)
		a.detailsView.SetTitleColor(tcell.ColorRed)
		a.detailsView.SetText("\n[red]Inflection search is disabled. The inflections.db file was not found.[white]")
		return
	}

	const modalPageName = "inflectionSearch"
	searchInput := tview.NewInputField().SetLabel("Inflected form: ").SetFieldWidth(30)
	resultsList := tview.NewList().ShowSecondaryText(false)
	detailsView := tview.NewTextView().SetDynamicColors(true).SetScrollable(true).SetWrap(true).SetWordWrap(true)
	detailsView.SetBorder(true).SetTitle("Base Form Details")

	modal := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(tview.NewFlex().SetDirection(tview.FlexColumn).
			AddItem(searchInput, 3, 1, true).
			AddItem(resultsList, 0, 4, false), 0, 1, true).
		AddItem(detailsView, 0, 2, false)

	resultsList.SetChangedFunc(func(index int, mainText, secondaryText string, shortcut rune) {
		parts := strings.Split(mainText, " ~> ")
		if len(parts) == 2 {
			baseWord := parts[1]
			glossText := data.GenerateGlossText(baseWord, a.glosses, a.prefixMatcher)
			detailsView.SetText(glossText).ScrollToBeginning()
		}
	})

	resultsList.SetSelectedFunc(func(index int, mainText, secondaryText string, shortcut rune) {
		parts := strings.Split(mainText, " ~> ")
		if len(parts) == 2 {
			a.inputField.SetText(parts[1])
		}
		a.pages.RemovePage(modalPageName)
		a.app.SetFocus(a.inputField)
	})

	searchInput.SetChangedFunc(func(text string) {
		query := strings.TrimSpace(text)
		resultsList.Clear()
		detailsView.Clear()
		if len(query) < 3 {
			return
		}

		ftsQuery := query + "*"
		q := "SELECT inflection, word FROM inflections_fts WHERE inflection MATCH ? LIMIT 50"
		rows, err := a.inflectionsDB.Query(q, ftsQuery)
		if err != nil {
			detailsView.SetText(fmt.Sprintf("[red]Database query failed: %v[white]", err))
			return
		}
		defer rows.Close()

		for rows.Next() {
			var inflection, word string
			if err := rows.Scan(&inflection, &word); err == nil {
				resultsList.AddItem(fmt.Sprintf("%s ~> %s", inflection, word), "", 0, nil)
			}
		}
		resultsList.SetCurrentItem(0)
	})

	searchInput.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyEsc:
			a.pages.RemovePage(modalPageName)
			return nil
		case tcell.KeyEnter:
			a.app.SetFocus(resultsList)
			return nil
		case tcell.KeyDown:
			a.app.SetFocus(resultsList)
			return nil
		}
		return event
	})

	a.pages.AddPage(modalPageName, modal, true, true)
	a.app.SetFocus(searchInput)
}

func (a *App) showMeaningSearchModal() {
	const modalPageName = "meaningSearch"
	searchInput := tview.NewInputField().SetLabel("English term: ").SetFieldWidth(30)
	resultsList := tview.NewList().ShowSecondaryText(false)
	detailsView := tview.NewTextView().SetDynamicColors(true).SetScrollable(true).SetWrap(true).SetWordWrap(true)
	detailsView.SetBorder(true).SetTitle("Finnish Word Details")

	modal := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(tview.NewFlex().SetDirection(tview.FlexColumn).
			AddItem(searchInput, 3, 1, true).
			AddItem(resultsList, 0, 4, false), 0, 1, true).
		AddItem(detailsView, 0, 2, false)

	searchAction := func() {
		query := strings.ToLower(strings.TrimSpace(searchInput.GetText()))
		resultsList.Clear()
		detailsView.Clear()
		if query == "" {
			return
		}

		foundMap := make(map[string]struct{})
		for word, glossSlice := range a.glosses {
			for _, gloss := range glossSlice {
				for _, meaning := range gloss.Meanings {
					if strings.Contains(strings.ToLower(meaning), query) {
						foundMap[word] = struct{}{}
						break
					}
				}
			}
		}

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

	resultsList.SetChangedFunc(func(index int, mainText, secondaryText string, shortcut rune) {
		glossText := data.GenerateGlossText(mainText, a.glosses, a.prefixMatcher)
		detailsView.SetText(glossText).ScrollToBeginning()
	})

	resultsList.SetSelectedFunc(func(index int, mainText, secondaryText string, shortcut rune) {
		a.inputField.SetText(mainText)
		a.pages.RemovePage(modalPageName)
		a.app.SetFocus(a.inputField)
	})

	searchInput.SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEnter {
			searchAction()
		}
	})

	searchInput.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyEsc:
			a.pages.RemovePage(modalPageName)
			return nil
		case tcell.KeyDown, tcell.KeyUp:
			a.app.SetFocus(resultsList)
			return nil
		}
		return event
	})

	a.pages.AddPage(modalPageName, modal, true, true)
	a.app.SetFocus(searchInput)
}

// ----------------------
// Standalone Utilities
// ----------------------

// openBrowser opens the specified URL in the default browser.
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

// cleanTerm removes leading/trailing non-letters from a string.
func cleanTerm(s string) string {
	start, end := 0, len(s)
	for start < end && !unicode.IsLetter(rune(s[start])) {
		start++
	}
	for end > start && !unicode.IsLetter(rune(s[end-1])) {
		end--
	}
	return s[start:end]
}

// ----------------------
// Constants
// ----------------------

const helpText = `[gray]
	Keybindings:
	Esc         = Exit
	Enter       = Clear search
	Up/Down     = Scroll word list

	Tab         = Scroll Word Details forward
	Shift-Tab   = Scroll Word Details backward

	[blue]Control-E[gray]   = [blue]Etsi perusmuotin, aka lemmatizer[gray]. Find a word's base form from its inflected form.
	[teal]Control-T[gray]   = Show [teal]example sentences[gray], from Tatoeba for the selected word.
	[yellow]Control-S[gray] = [yellow]Mark[gray]/unmark words. All marked words will be saved upon Esc to a text file.
	[green]Control-L[gray]  = [green]List[gray] marked words. 
	[cyan]Control-F[gray]   = [cyan]Reverse-find[gray] words by searching their English definitions.
	[pink]Control-H[gray]   = Show this [pink]help[gray] text again.

	[red]Control-R[gray]   = [red]Report a bug[gray] on GitHub.com. [red]Opens your web browser[gray].
	[white]`

const finnishFlag = `[gray]
                         _,-(.;)
                     _,-',###""
                 _,-',###",'|
             _,-',###" ,-" :|
         _,-',###" _,#"   .'|
 ### _,-',###"_,######    : |
_,-',###;-'"~. #####9   :' |
,###"   |   :  ######  ,. _|
"       | :     #####( .;###|
        |:'.    ######,6####|
        |..     ;############|
        ":_,###############|
          |##############'~ |
          |############".   |
         .###########':     |
         :##".   #####. '   |
         | :'    ######.   .|
         |.'     ######     |
         |.      ######   ':|
         ":      ######   .:.|
          |     ."#####    ._|
          |      :#####_,-'""
          |    '.,###""
         :'  .:,-'
         |_.,-'
         "
	[white]`
