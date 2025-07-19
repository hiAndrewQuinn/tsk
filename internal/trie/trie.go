// Package trie implements a trie data structure for efficient prefix-based
// string searching. It is optimized for fast lookups and insertions of words.
package trie

// TRIE_MAX_SEARCH_DEPTH defines the maximum number of words to return from a search.
// This prevents runaway searches on very short prefixes that could match a large
// portion of the dictionary.
const TRIE_MAX_SEARCH_DEPTH = 50

// TrieNode represents a single node in the trie. It contains a map of its
// children nodes and a boolean flag to indicate if it marks the end of a word.
type TrieNode struct {
	children map[rune]*TrieNode
	isEnd    bool
}

// newTrieNode creates and returns a new, initialized TrieNode.
func newTrieNode() *TrieNode {
	return &TrieNode{children: make(map[rune]*TrieNode)}
}

// Trie represents the complete trie data structure with a pointer to the root node.
type Trie struct {
	root *TrieNode
}

// NewTrie creates and returns a new, initialized Trie.
func NewTrie() *Trie {
	return &Trie{root: newTrieNode()}
}

// Insert adds a word to the trie, creating nodes as necessary.
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

// FindWords searches the trie for all words that start with the given prefix.
// The search is capped by TRIE_MAX_SEARCH_DEPTH.
func (t *Trie) FindWords(prefix string) []string {
	node := t.root
	// Navigate to the node corresponding to the end of the prefix.
	for _, ch := range prefix {
		next, exists := node.children[ch]
		if !exists {
			// If the prefix doesn't exist in the trie, return no words.
			return []string{}
		}
		node = next
	}

	// Once at the prefix node, collect all words that stem from it.
	var words []string
	node.collectWords(prefix, &words)
	return words
}

// collectWords is a recursive helper function that traverses the trie from a
// given node, collecting all complete words it finds until the search depth
// limit is reached.
func (node *TrieNode) collectWords(prefix string, words *[]string) {
	// Stop if the maximum number of results has been found.
	if len(*words) >= TRIE_MAX_SEARCH_DEPTH {
		return
	}

	// If the current node marks the end of a word, add it to the results.
	if node.isEnd {
		*words = append(*words, prefix)
		// Check the limit again after adding, to stop as early as possible.
		if len(*words) >= TRIE_MAX_SEARCH_DEPTH {
			return
		}
	}

	// Recursively call collectWords for all children of the current node.
	for ch, child := range node.children {
		child.collectWords(prefix+string(ch), words)
		// Final check within the loop to exit promptly.
		if len(*words) >= TRIE_MAX_SEARCH_DEPTH {
			return
		}
	}
}

// CountNodes traverses the entire trie and returns the total number of nodes.
// This is useful for debugging and understanding the memory footprint of the trie.
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
