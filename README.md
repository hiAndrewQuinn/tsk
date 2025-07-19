# tsk - taskusanakirja

A tiny, fast, and feature-rich Finnish-English dictionary. Search as fast as you can type. *Tsemppiä!*

### 📺 [Watch the v0.0.6 release in action on YouTube\!](http://www.youtube.com/watch?v=MSa3P491mNw)

## Overview

**tsk** (from *taskusanakirja*, "pocket dictionary") is a powerful, lightweight Finnish-English dictionary designed for the command line. Written in **Go**, it's a single, portable file with no dependencies.

It's built for language learners, developers, and anyone who needs a fast, offline-first dictionary. It uses a custom **trie** for instant autocompletion and embeds its data—including word glosses, example sentences, and inflection data—directly into the binary. This means you get a full-featured dictionary in one file that works seamlessly on **Linux**, **Windows**, and **macOS**.

## Features

  - ✨ **Instant Search-As-You-Type**: Get immediate word suggestions from a massive wordlist.
  - 🧠 **Inflection/Lemmatizer Search (Ctrl-E)**: Don't know the base word? No problem. Search for `talossa` and find `talo`.
  - 🔄 **Reverse English-to-Finnish Search (Ctrl-F)**: Find Finnish words by searching their English definitions.
  - 📚 **Example Sentences (Ctrl-T)**: See the selected word in context with real-world examples from Tatoeba.
  - 🌱 **"Go Deeper" Definitions**: Automatically see definitions for words within other definitions for a seamless learning experience.
  - ⭐ **Vocabulary Builder**: **Mark** words with `Ctrl-S`, view your list with `Ctrl-L`, and export it to JSONL or TXT files when you quit.
  - 🚀 **Cross-Platform & Portable**: A single, dependency-free binary for macOS (amd64, arm64), Linux (amd64, arm64), and Windows (386, amd64).
  - 🎨 **Sleek TUI**: A clean and responsive terminal interface powered by the excellent `tview` and `tcell` libraries.

## Installation

Download the latest pre-built binary for your operating system from the [**Releases Page**](https://github.com/hiAndrewQuinn/tsk/releases).

No installation is needed. Just download the file, (optionally) rename it to `tsk`, and run it\!

-----

## How to Use

Run the binary from your terminal:

```bash
./tsk
```

Or on Windows, just double-click the `.exe` file\!

### Keybindings

| Key | Action |
| :--- | :--- |
| `Up/Down` | Scroll through the word list. |
| `Tab/Shift-Tab` | Scroll the details view. |
| `Ctrl-E` | Toggle **Inflection Search** (find base words). |
| `Ctrl-F` | Toggle **Reverse Find** (search English definitions). |
| `Ctrl-T` | Show **Example Sentences** for the selected word. |
| `Ctrl-S` | **Mark/Unmark** the selected word. |
| `Ctrl-L` | **List** all your marked words. |
| `Ctrl-H` | Show the **Help** screen. |
| `Esc` | Exit the application. |

-----

## Building From Source

If you'd rather build it yourself:

1.  **Clone the repository:**

    ```bash
    git clone https://github.com/hiAndrewQuinn/tsk.git
    cd tsk
    ```

2.  **Build the data and binaries:**
    The project uses a `Makefile` to simplify the build process. These commands are now powered by Go programs in the `cmd/` directory.

    ```bash
    make
    ```

    This command will:

      - Generate `glosses.gob` from `data/glosses.jsonl`.
      - Generate `words.txt` from `data/glosses.jsonl`.
      - Build the `tsk` binary for your current system into the `build/` directory.

### Makefile Commands

  - `make all`: The default target. Builds all data files and the binary for the host OS.
  - `make glosses`: Builds the `glosses.gob` data file.
  - `make words`: Builds the `words.txt` list.
  - `make build-all`: Compiles binaries for all supported platforms.
  - `make install`: Installs the `tsk` binary to `/usr/local/bin` (not supported on Windows).
  - `make clean`: Removes the `build` directory and generated data files.

-----

## Data Sources

  - **Glosses & Words**: Derived from Wiktionary, licensed under [CC BY-SA 3.0](https://creativecommons.org/licenses/by-sa/3.0/).
  - **Example Sentences**: Sourced from [Tatoeba](https://tatoeba.org), licensed under [CC BY 2.0 FR](https://creativecommons.org/licenses/by/2.0/fr/).
  - **Inflections**: An optional, user-provided SQLite database.

## License

  - **Code**: The Unlicense (Public Domain).
  - **Data**: See Data Sources section for details.
