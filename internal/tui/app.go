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

	"github.com/rivo/tview"
	"github.com/gdamore/tcell/v2"

	"github.com/hiAndrewQuinn/tsk/internal/data"
	"github.com/hiAndrewQuinn/tsk/internal/trie"
)

// --- Constants ---

const helpText = `[gray]
	Keybindings:
	Esc         = Exit the application
	Up/Down     = Scroll lists
	Tab/Shift-Tab = Scroll details view on main screen

	[blue]Ctrl-E[gray]      = Toggle [blue]Etsi perusmuoto (lemmatizer)[gray] search.
	[teal]Ctrl-T[gray]      = Show [teal]example sentences[gray] for the selected word.
	[yellow]Ctrl-S[gray]    = [yellow]Mark[gray]/unmark the selected word for export.
	[green]Ctrl-L[gray]      = [green]List[gray] all marked words.
	[cyan]Ctrl-F[gray]      = Toggle [cyan]Reverse-find[gray] by English definition.
	[pink]Ctrl-H[gray]      = Toggle this [pink]help[gray] screen.

	[red]Ctrl-R[gray]       = [red]Report a bug[gray] on GitHub.com. [red]Opens your web browser[gray].
	[white]`

const finnishFlag = `[gray]
                            _,-(.;)
                          _,-',###""
                        _,-',###",'|
                      _,-',###" ,-" :|
                    _,-',###" _,#"   .'|
          ### _,-',###"_,######   : |
        _,-',###;-'"~. #####9   :' |
      ,###"   |   :  ######  ,. _|
      "       | :     #####( .;###|
              |:'.     ######,6####|
              |..       ;############|
              ":_,###############|
                  |##############'~ |
                  |############".   |
                  .###########':     |
                  :##".   #####. '   |
                  | :'    ######.   .|
                  |.'     ######     |
                  |.       ######   ':|
                  ":       ######   .:.|
                   |      ."#####    ._|
                   |         :#####_,-'""
                   |      '.,###""
                   :'  .:,-'
                   |_.,-'
                   "
	[white]`

// --- Type Definitions ---

// Page encapsulates a tview.Primitive that represents a single screen,
// and a reference to the Primitive that should receive focus when the page is shown.
type Page struct {
	Root        tview.Primitive
	FocusTarget tview.Primitive
}

// App encapsulates all the components and state of the TUI.
type App struct {
	// tview core
	app   *tview.Application
	pages *tview.Pages

	// Page management
	pageMap map[string]Page

	// Components by page
	mainInput       *tview.InputField
	mainWordList    *tview.List
	mainDetailsView *tview.TextView
	inflectionList  *tview.List
	meaningList     *tview.List

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

// ModalTheme defines the complete color scheme for a search page.
type ModalTheme struct {
	BgColor               tcell.Color
	HeaderFooterBg        tcell.Color
	DetailsBg             tcell.Color
	PrimaryTextColor      tcell.Color
	AccentColor           tcell.Color
	FieldBgColor          tcell.Color
	ListSelectedBgColor   tcell.Color
	ListSelectedTextColor tcell.Color
}

// --- Initialization ---

// NewApp creates, initializes, and returns a new TUI application instance.
func NewApp(version string, debug bool, wordTrie *trie.Trie, glossData map[string][]data.Gloss, matcher *data.PrefixMatcher, exDB, inflDB *sql.DB) *App {
	a := &App{
		app:           tview.NewApplication(),
		pages:         tview.NewPages(),
		pageMap:       make(map[string]Page),
		version:       version,
		debug:         debug,
		trie:          wordTrie,
		glosses:       glossData,
		prefixMatcher: matcher,
		exampleDB:     exDB,
		inflectionsDB: inflDB,
		markedWords:   make(map[string]struct{}),
	}

	a.createPages()
	a.setupGlobalInputCapture()

	return a
}

// Run starts the TUI application event loop.
func (a *App) Run() error {
	fmt.Println("Starting the TUI. Thank you for your patience!")
	if err := a.app.SetRoot(a.pages, true).Run(); err != nil {
		return err
	}
	fmt.Println("Stopping the TUI. Thank you for exiting gracefully!")
	return a.saveMarkedWords()
}

// --- Page Creation ---

// createPages initializes all pages of the application.
func (a *App) createPages() {
	// Create and add all pages to the page manager
	a.pageMap["main"] = a.createMainSearchPage()
	a.pageMap["inflections"] = a.createInflectionSearchPage()
	a.pageMap["meanings"] = a.createMeaningSearchPage()
	a.pageMap["help"] = a.createHelpPage()

	for name, page := range a.pageMap {
		a.pages.AddPage(name, page.Root, true, name == "main")
	}
}

// createMainSearchPage builds the primary search interface.
func (a *App) createMainSearchPage() Page {
	// --- Components ---
	a.mainInput = tview.NewInputField().SetLabel("Search: ").SetFieldWidth(30)
	a.mainWordList = tview.NewList().ShowSecondaryText(false)
	a.mainDetailsView = tview.NewTextView().
		SetDynamicColors(true).
		SetWrap(true).
		SetWordWrap(true)
	a.mainDetailsView.SetBorder(true)
	a.mainDetailsView.SetText(helpText) // Show help on startup

	// --- Layout ---
	leftFlex := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(a.mainInput, 3, 1, true).
		AddItem(a.mainWordList, 0, 4, false)

	topFlex := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(leftFlex, 0, 1, true).
		AddItem(a.mainDetailsView, 0, 2, false)

	// --- Handlers ---
	a.setupMainPageWidgetHandlers()

	return Page{
		Root:        a.createPageFrame(topFlex),
		FocusTarget: a.mainInput,
	}
}

// createInflectionSearchPage builds the UI for searching by inflected form.
func (a *App) createInflectionSearchPage() Page {
	theme := ModalTheme{
		BgColor:               tcell.ColorSteelBlue,
		HeaderFooterBg:        tcell.ColorDarkSlateGray,
		DetailsBg:             tcell.ColorMidnightBlue,
		PrimaryTextColor:      tcell.ColorLightCyan,
		AccentColor:           tcell.ColorAqua,
		FieldBgColor:          tcell.ColorDarkBlue,
		ListSelectedBgColor:   tcell.ColorDarkSlateGray,
		ListSelectedTextColor: tcell.ColorAqua,
	}

	onSelect := func(baseWord string) {
		a.mainInput.SetText(baseWord)
		a.updateWordList(baseWord)
		a.switchPage("main")
	}

	onSearch := func(query string, results *tview.List, details *tview.TextView) {
		a.performInflectionSearch(query, results, details)
	}

	onResultChanged := func(mainText string, details *tview.TextView) {
		parts := strings.Split(mainText, " ~> ")
		if len(parts) == 2 {
			details.SetText(data.GenerateGlossText(parts[1], a.glosses, a.prefixMatcher)).ScrollToBeginning()
		}
	}

	title := fmt.Sprintf("tsk (%s) - Inflection Search", a.version)
	searchLabel := "Inflected form: "
	detailsTitle := "Base Form Details"
	footerText := "Esc to exit. Ctrl-E to return to main search. Enter on result to select."

	pageContent, searchInput, list := a.createGenericSearchLayout(title, searchLabel, detailsTitle, footerText, theme, onSearch, onResultChanged, onSelect)
	a.inflectionList = list // Store reference to the list

	return Page{
		Root:        a.createPageFrame(pageContent),
		FocusTarget: searchInput,
	}
}

// createMeaningSearchPage builds the UI for reverse-searching by English meaning.
func (a *App) createMeaningSearchPage() Page {
	theme := ModalTheme{
		BgColor:               tcell.GetColor("#002b36"), // Solarized Dark Base
		HeaderFooterBg:        tcell.GetColor("#073642"), // Solarized Dark Base02
		DetailsBg:             tcell.GetColor("#00222b"), // Darker variant for details
		PrimaryTextColor:      tcell.GetColor("#839496"), // Solarized Text
		AccentColor:           tcell.ColorTeal,
		FieldBgColor:          tcell.GetColor("#073642"), // Solarized Dark Base02
		ListSelectedBgColor:   tcell.ColorTeal,
		ListSelectedTextColor: tcell.ColorWhite,
	}

	onSelect := func(finnishWord string) {
		a.mainInput.SetText(finnishWord)
		a.updateWordList(finnishWord)
		a.switchPage("main")
	}

	onSearch := func(query string, results *tview.List, details *tview.TextView) {
		a.performMeaningSearch(query, results, details)
	}

	onResultChanged := func(mainText string, details *tview.TextView) {
		details.SetText(data.GenerateGlossText(mainText, a.glosses, a.prefixMatcher)).ScrollToBeginning()
	}

	title := fmt.Sprintf("tsk (%s) - English->Finnish Reverse Search", a.version)
	searchLabel := "English term: "
	detailsTitle := "Finnish Word Details"
	footerText := "Esc to exit. Ctrl-F to return to main search. Enter on result to select."

	pageContent, searchInput, list := a.createGenericSearchLayout(title, searchLabel, detailsTitle, footerText, theme, onSearch, onResultChanged, onSelect)
	a.meaningList = list // Store reference to the list

	return Page{
		Root:        a.createPageFrame(pageContent),
		FocusTarget: searchInput,
	}
}

// createHelpPage builds the static help screen.
func (a *App) createHelpPage() Page {
	textView := tview.NewTextView().
		SetDynamicColors(true).
		SetText(helpText).
		SetScrollable(true)
	textView.SetBorder(true).SetTitle("Help (Ctrl-H to return, Esc to exit)")

	// NOTE: The local 'Esc' handler was removed.
	// The global input capture now handles Esc to exit the entire application.

	return Page{
		Root:        a.createPageFrame(textView),
		FocusTarget: textView,
	}
}

// --- UI Creation Helpers ---

// createPageFrame builds the standard layout with a header, footer, and content area.
func (a *App) createPageFrame(content tview.Primitive) *tview.Flex {
	header := a.createHeader()
	footer := a.createFooter()

	pageLayout := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(header, 1, 0, false).
		AddItem(nil, 1, 0, false). // Spacer
		AddItem(content, 0, 1, true).
		AddItem(nil, 1, 0, false). // Spacer
		AddItem(footer, 1, 0, false)

	return pageLayout
}

// createHeader creates the top header component.
func (a *App) createHeader() *tview.Flex {
	headerLeft := tview.NewTextView().
		SetText(fmt.Sprintf("tsk (%s) - Andrew's Pocket Finnish Dictionary", a.version)).
		SetTextAlign(tview.AlignLeft).
		SetTextColor(tcell.ColorBlack)
	headerLeft.SetBackgroundColor(tcell.ColorLightGray)

	headerRight := tview.NewButton("[::u]https://github.com/hiAndrewQuinn/tsk[::-]").
		SetLabelColor(tcell.ColorWhite).
		SetSelectedFunc(func() { openBrowser("https://github.com/hiAndrewQuinn/tsk") })
	headerRight.SetBackgroundColor(tcell.ColorBlue)

	headerFlex := tview.NewFlex().
		SetDirection(tview.FlexColumn).
		AddItem(headerLeft, 0, 1, false).
		AddItem(headerRight, 40, 0, false)
	headerFlex.SetBackgroundColor(tcell.ColorLightGray)

	return headerFlex
}

// createFooter creates the bottom footer component.
func (a *App) createFooter() *tview.Flex {
	footerLeft := tview.NewTextView().
		SetText("Ctrl-H for Help. Esc to exit.").
		SetTextAlign(tview.AlignLeft).
		SetTextColor(tcell.ColorBlack)
	footerLeft.SetBackgroundColor(tcell.ColorLightGray)

	footerRight := tview.NewButton("[::u]https://andrew-quinn.me/[::-]").
		SetLabelColor(tcell.ColorWhite).
		SetSelectedFunc(func() { openBrowser("https://andrew-quinn.me/") })
	footerRight.SetBackgroundColor(tcell.ColorBlue)

	footerFlex := tview.NewFlex().
		SetDirection(tview.FlexColumn).
		AddItem(footerLeft, 0, 1, false).
		AddItem(footerRight, 40, 0, false)
	footerFlex.SetBackgroundColor(tcell.ColorLightGray)

	return footerFlex
}

// createGenericSearchLayout builds a themed search UI and returns its root primitive and the input field for focusing.
func (a *App) createGenericSearchLayout(
	title, searchLabel, detailsTitle, footerText string,
	theme ModalTheme,
	onSearchChanged func(query string, results *tview.List, details *tview.TextView),
	onResultChanged func(mainText string, details *tview.TextView),
	onResultSelected func(mainText string),
) (tview.Primitive, *tview.InputField, *tview.List) {

	// --- Components ---
	searchInput := tview.NewInputField().
		SetLabel(searchLabel).
		SetLabelColor(theme.AccentColor).
		SetFieldBackgroundColor(theme.FieldBgColor).
		SetFieldTextColor(theme.PrimaryTextColor).
		SetFieldWidth(30)

	resultsList := tview.NewList().
		ShowSecondaryText(false).
		SetSelectedBackgroundColor(theme.ListSelectedBgColor).
		SetSelectedTextColor(theme.ListSelectedTextColor)

	detailsView := tview.NewTextView().
		SetDynamicColors(true).
		SetScrollable(true).
		SetWrap(true).
		SetWordWrap(true).
		SetTextColor(theme.PrimaryTextColor)
	detailsView.SetBackgroundColor(theme.DetailsBg) // Set background color separately
	detailsView.SetBorder(true).
		SetTitle(detailsTitle).
		SetBorderColor(theme.AccentColor).
		SetTitleColor(theme.AccentColor)

	// --- Layout ---
	contentFlex := tview.NewFlex().
		SetDirection(tview.FlexColumn).
		AddItem(
			tview.NewFlex().SetDirection(tview.FlexRow).
				AddItem(searchInput, 3, 1, true).
				AddItem(resultsList, 0, 4, false),
			0, 1, true,
		).
		AddItem(detailsView, 0, 2, false)
	contentFlex.SetBackgroundColor(theme.BgColor)

	// --- Event Handlers ---
	resultsList.SetChangedFunc(func(index int, mainText, secondaryText string, shortcut rune) {
		onResultChanged(mainText, detailsView)
	})

	resultsList.SetSelectedFunc(func(index int, mainText, secondaryText string, shortcut rune) {
		onResultSelected(mainText)
	})

	searchInput.SetChangedFunc(func(text string) {
		onSearchChanged(text, resultsList, detailsView)
	})

	searchInput.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		// NOTE: The local 'Esc' handler was removed.
		// The global input capture now handles Esc to exit the entire application.
		switch event.Key() {
		case tcell.KeyDown, tcell.KeyEnter:
			a.app.SetFocus(resultsList)
			return nil
		}
		return event
	})

	return contentFlex, searchInput, resultsList
}

// --- Event Handlers & Actions ---

// setupGlobalInputCapture sets up the application-wide key bindings, which act as a router.
func (a *App) setupGlobalInputCapture() {
	a.app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyEsc:
			// Per user request, Esc always exits the application.
			a.app.Stop()
			return nil
		case tcell.KeyCtrlR:
			openBrowser("https://github.com/hiAndrewQuinn/tsk/issues/new")
			return nil
		case tcell.KeyCtrlF:
			a.togglePage("meanings")
			return nil
		case tcell.KeyCtrlE:
			a.togglePage("inflections")
			return nil
		case tcell.KeyCtrlH:
			a.togglePage("help")
			return nil
		case tcell.KeyCtrlT:
			a.handleActionWithContext(a.showExampleSentences)
			return nil
		case tcell.KeyCtrlL:
			a.handleActionWithContext(a.listMarkedWords)
			return nil
		case tcell.KeyCtrlS:
			a.handleActionWithContext(a.toggleMarkedWord)
			return nil
		}
		return event
	})
}

// setupMainPageWidgetHandlers configures event handlers for the main search page.
func (a *App) setupMainPageWidgetHandlers() {
	a.mainWordList.SetChangedFunc(func(idx int, mainText string, _ string, _ rune) {
		a.displayGloss(mainText)
	})

	a.mainInput.SetChangedFunc(func(text string) {
		a.updateWordList(text)
	})

	a.mainInput.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyDown:
			cur := a.mainWordList.GetCurrentItem()
			if cur < a.mainWordList.GetItemCount()-1 {
				a.mainWordList.SetCurrentItem(cur + 1)
			}
			return nil
		case tcell.KeyUp:
			cur := a.mainWordList.GetCurrentItem()
			if cur > 0 {
				a.mainWordList.SetCurrentItem(cur - 1)
			}
			return nil
		case tcell.KeyEnter:
			a.mainInput.SetText("")
			a.updateWordList("")
			return nil
		}
		return event
	})

	a.mainDetailsView.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyTab:
			row, col := a.mainDetailsView.GetScrollOffset()
			a.mainDetailsView.ScrollTo(row+1, col)
			return nil
		case tcell.KeyBacktab:
			row, col := a.mainDetailsView.GetScrollOffset()
			if row > 0 {
				a.mainDetailsView.ScrollTo(row-1, col)
			}
			return nil
		}
		return event
	})
}

// togglePage switches to the given page if not currently on it,
// otherwise switches back to the main page.
func (a *App) togglePage(pageName string) {
	current, _ := a.pages.GetFrontPage()
	if current == pageName {
		a.switchPage("main")
	} else {
		a.switchPage(pageName)
	}
}

// handleActionWithContext ensures that word-based actions (like marking a word or showing examples)
// are executed in the context of the main page. If called from another page,
// it preserves the currently selected word, returns to the main page,
// and then executes the action.
func (a *App) handleActionWithContext(action func()) {
	name, _ := a.pages.GetFrontPage()

	if name != "main" {
		var selectedList *tview.List
		switch name {
		case "inflections":
			selectedList = a.inflectionList
		case "meanings":
			selectedList = a.meaningList
		default:
			// For pages without a list (e.g., help), just switch to main
			a.switchPage("main")
			action()
			return
		}

		// If there is a list and it has items, grab the selected one
		if selectedList != nil && selectedList.GetItemCount() > 0 {
			idx := selectedList.GetCurrentItem()
			mainText, _ := selectedList.GetItemText(idx)
			wordToSearch := mainText

			// For inflections, we want the base word (e.g., from "juoksen ~> juosta")
			if name == "inflections" {
				if parts := strings.Split(mainText, " ~> "); len(parts) == 2 {
					wordToSearch = parts[1]
				}
			}

			a.switchPage("main")
			a.mainInput.SetText(wordToSearch) // This repopulates the main list
		} else {
			a.switchPage("main") // Switch to main even if no word is selected
		}
	}

	action()
}

// switchPage changes the visible page and sets focus to the correct element.
func (a *App) switchPage(name string) {
	if page, ok := a.pageMap[name]; ok {
		a.pages.SwitchToPage(name)
		if page.FocusTarget != nil {
			a.app.SetFocus(page.FocusTarget)
		}
	}
}

// updateWordList clears and repopulates the main word list based on search text.
func (a *App) updateWordList(text string) {
	a.mainWordList.Clear()
	if text != "" {
		matches := a.trie.FindWords(text)
		for _, w := range matches {
			a.mainWordList.AddItem(w, "", 0, nil)
		}
	}
	if a.mainWordList.GetItemCount() > 0 {
		a.mainWordList.SetCurrentItem(0)
	}
}

// displayGloss shows the definition for the given word in the main details view.
func (a *App) displayGloss(word string) {
	if a.debug {
		log.Printf("displayGloss: called for word: %s", word)
	}

	_, isMarked := a.markedWords[word]
	if isMarked {
		a.mainDetailsView.SetTitle("Word Details (Ctrl-S to unmark)")
		a.mainDetailsView.SetBorderColor(tcell.ColorYellow)
		a.mainDetailsView.SetTitleColor(tcell.ColorYellow)
		a.mainWordList.SetSelectedBackgroundColor(tcell.ColorYellow)
	} else {
		a.mainDetailsView.SetTitle("Word Details (Ctrl-S to mark)")
		a.mainDetailsView.SetBorderColor(tcell.ColorWhite)
		a.mainDetailsView.SetTitleColor(tcell.ColorWhite)
		a.mainWordList.SetSelectedBackgroundColor(tcell.ColorWhite)
	}

	glossText := data.GenerateGlossText(word, a.glosses, a.prefixMatcher)
	a.mainDetailsView.SetText(glossText).ScrollToBeginning()
}

// toggleMarkedWord adds or removes the currently selected word from the marked list.
func (a *App) toggleMarkedWord() {
	if a.mainWordList.GetItemCount() == 0 {
		a.mainDetailsView.SetTitle("Error").SetBorderColor(tcell.ColorRed).SetTitleColor(tcell.ColorRed)
		a.mainDetailsView.SetText("\n  [red]You need to search for something before you can mark or unmark it.[white]")
		return
	}
	idx := a.mainWordList.GetCurrentItem()
	word, _ := a.mainWordList.GetItemText(idx)

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
	a.displayGloss(word) // Re-display to update title and border color
}

// listMarkedWords displays all currently marked words in the details view.
func (a *App) listMarkedWords() {
	count := len(a.markedWords)
	if count == 0 {
		a.mainDetailsView.SetTitle("Marked words list empty. Kotimaa itkee...").SetBorderColor(tcell.ColorGreen).SetTitleColor(tcell.ColorGreen)
		a.mainDetailsView.SetText(finnishFlag)
	} else {
		a.mainDetailsView.SetTitle(fmt.Sprintf("Listing marked words. (count: %d)", count)).SetBorderColor(tcell.ColorGreen).SetTitleColor(tcell.ColorGreen)
		sortedWords := make([]string, 0, len(a.markedWords))
		for w := range a.markedWords {
			sortedWords = append(sortedWords, w)
		}
		sort.Strings(sortedWords)

		var builder strings.Builder
		builder.WriteString("[green]")
		for _, w := range sortedWords {
			builder.WriteString(w + "\n")
		}
		builder.WriteString("\n\n[gray]Note: Exported files do not include 'go-deeper' phrases automatically.[white]")
		a.mainDetailsView.SetText(builder.String())
	}
}

// showExampleSentences queries and displays example sentences for the selected word.
func (a *App) showExampleSentences() {
	if a.mainWordList.GetItemCount() == 0 {
		a.mainDetailsView.SetTitle("No word selected. Kotimaa itkee...").SetBorderColor(tcell.ColorTeal).SetTitleColor(tcell.ColorTeal)
		a.mainDetailsView.SetText(finnishFlag)
		return
	}
	idx := a.mainWordList.GetCurrentItem()
	word, _ := a.mainWordList.GetItemText(idx)
	if strings.TrimSpace(word) == "" {
		a.mainDetailsView.SetText("[teal]No word entered. Please type something in the search bar.[white]")
		return
	}

	phrase := `"` + cleanTerm(word) + `"`
	rows, err := a.exampleDB.Query(`SELECT finnish, english FROM sentences WHERE sentences MATCH ?`, phrase)
	if err != nil {
		a.mainDetailsView.SetText(fmt.Sprintf("Error querying examples: %v", err)).SetBorderColor(tcell.ColorRed)
		return
	}
	defer rows.Close()

	var buf strings.Builder
	found := false
	buf.WriteString("[white]Example sentences from https://tatoeba.org (CC BY 2.0 FR).\n\n")
	for rows.Next() {
		found = true
		var fin, eng string
		if err := rows.Scan(&fin, &eng); err == nil {
			buf.WriteString("[teal]" + fin + "\n")
			buf.WriteString("[pink]" + eng + "\n\n")
		}
	}

	if !found {
		a.mainDetailsView.SetTitle("No examples found")
		a.mainDetailsView.SetText("[red]No Tatoeba example sentences found.[white]")
	} else {
		a.mainDetailsView.SetTitle(fmt.Sprintf("Examples for '%s'", word))
		a.mainDetailsView.SetText(buf.String())
	}
	a.mainDetailsView.SetBorderColor(tcell.ColorTeal).SetTitleColor(tcell.ColorTeal)
}

// --- Search Logic ---

// performInflectionSearch handles the logic for the inflection search page.
func (a *App) performInflectionSearch(query string, results *tview.List, details *tview.TextView) {
	results.Clear()
	details.Clear()

	if a.inflectionsDB == nil {
		details.SetText("\n[red]Inflection search is disabled. The inflections.db file was not found.[white]")
		return
	}
	if len(query) < 3 {
		if len(query) > 0 {
			details.SetText(fmt.Sprintf("[gray]Please enter at least 3 characters (you entered %d).", len(query)))
		}
		return
	}

	ftsQuery := query + "*"
	q := "SELECT inflection, word FROM inflections_fts WHERE inflection MATCH ? ORDER BY LENGTH(inflection) LIMIT 50"
	rows, err := a.inflectionsDB.Query(q, ftsQuery)
	if err != nil {
		details.SetText(fmt.Sprintf("[red]Database query failed: %v[white]", err))
		return
	}
	defer rows.Close()

	for rows.Next() {
		var inflection, word string
		if err := rows.Scan(&inflection, &word); err == nil {
			results.AddItem(fmt.Sprintf("%s ~> %s", inflection, word), "", 0, nil)
		}
	}
	if results.GetItemCount() > 0 {
		results.SetCurrentItem(0)
	} else {
		details.SetText(fmt.Sprintf("No base form found for '%s'.", query))
	}
}

// performMeaningSearch handles the logic for the meaning search page.
func (a *App) performMeaningSearch(query string, results *tview.List, details *tview.TextView) {
	results.Clear()
	details.Clear()
	trimmedQuery := strings.ToLower(strings.TrimSpace(query))
	if trimmedQuery == "" {
		return
	}

	foundMap := make(map[string]struct{})
	for word, glossSlice := range a.glosses {
		for _, gloss := range glossSlice {
			for _, meaning := range gloss.Meanings {
				if strings.Contains(strings.ToLower(meaning), trimmedQuery) {
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
		results.AddItem(match, "", 0, nil)
	}

	if results.GetItemCount() > 0 {
		results.SetCurrentItem(0)
	} else {
		details.SetText(fmt.Sprintf("No Finnish words found for '%s'.", query))
	}
}

// --- File I/O & Utilities ---

// saveMarkedWords writes the list of marked words to timestamped files.
func (a *App) saveMarkedWords() error {
	if len(a.markedWords) == 0 {
		return nil
	}
	ts := time.Now().Format("2006-01-02-15-04-05")
	base := fmt.Sprintf("tsk-marked_%s", ts)
	jsonFile, txtFile := base+".jsonl", base+".txt"

	// JSONL dump
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

	// TXT dump
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
