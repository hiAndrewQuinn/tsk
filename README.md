# tsk - Taskusanakirja

A tiny, fast, and portable Finnish-English dictionary that searches as you type.

![recording](https://github.com/user-attachments/assets/e88fa507-b945-420b-a7db-05b8634c0750)


## Overview

**tsk** (*taskusanakirja*, "pocket dictionary") is a lightweight Finnish-English dictionary tool written in Go. It leverages a custom trie data structure to provide real-time search results and displays word definitions instantly in a sleek terminal user interface built with [tcell](https://github.com/gdamore/tcell) and [tview](https://github.com/rivo/tview). With pre-built binaries for multiple platforms, tsk is designed and has been tested to work seamlessly on Linux, Windows, and macOS.

## Features

- **Instant Search:** Get immediate word suggestions as you type.
- **Efficient Lookup:** Uses a custom trie for fast and effective searching.
- **Responsive TUI:** Clean, intuitive terminal interface for quick navigation.
- **Cross-Platform Support:** Pre-built binaries available for:
  - macOS, aka Darwin (amd64, arm64)
  - Linux (amd64, arm64)
  - Windows (386, amd64)
- **Single-file portability:** All of the dictionary info has been embedded right alongside the program itself, so you really do only need that one file. Plug and play!

## Installation

You can either build `tsk` from source or download a pre-built binary from Releases.

### Downloading Pre-built Binaries

Visit the [release page](https://github.com/hiAndrewQuinn/tsk/releases) to download the binary suitable for your platform.

### Building from Source

1. **Clone the repository:**
   ```bash
   git clone https://github.com/hiAndrewQuinn/tsk.git
   cd tsk
   ```

2. **Build the project using the provided Makefile:**
   ```bash
   make
   ```
   The compiled binaries will be located in the `build` directory.

## Makefile Commands

The Makefile provides several useful targets for building, installing, and cleaning up the project:

- **`make` or `make all`**  
  This is the default target. It first generates `words.txt` from `glosses.jsonl` using the command:
  ```bash
  jq '.word' glosses.jsonl | sort -u > words.txt
  ```
  Then it creates the build directory and compiles the binary for all defined platforms, placing them in the `build` directory.

- **`make words.txt`**  
  Regenerates the `words.txt` file from `glosses.jsonl` independently. This is useful if you update the glosses and want to update the word list without rebuilding the entire project.

- **`make build-all`**  
  Builds the binary for all supported target platforms. This target is run as part of the default `all` target but can also be invoked on its own if you wish to rebuild the binaries.

- **`make install`**  
  Builds the project (if not already built) and then installs the binary for your current platform into your system's PATH (defaulting to `/usr/local/bin`). On non-Windows systems, it copies the appropriate binary from the `build` directory so you can run `tsk` from anywhere. (Installation is not supported on Windows.)

- **`make clean`**  
  Removes the entire `build` directory and any compiled binaries, effectively cleaning up the project build artifacts.

Additionally, you can use flags such as `make -B` to force rebuilds or `make -j4` to build in parallel for faster compilation.

## Usage

Run the binary from your terminal:

- On macOS or Linux:
  ```bash
  ./tsk
  ```
- On Windows:
  ```bash
  tsk_windows_amd64.exe    # or just double click it!
  ```

Once launched, type in the search bar to see instant Finnish word suggestions along with their definitions. Use the arrow keys to navigate through the list, and press `Enter` to clear the search field.

### Security Alerts and Permissions

When downloading pre-built binaries on macOS and Windows, you might encounter security warnings or alerts. These are standard precautions by your operating system to protect against unverified software. If you trust the source (aka, this project), here’s how to bypass these warnings:

#### macOS

- **Unidentified Developer Warning:**  
  If you see a message that the app is from an unidentified developer, go to **System Preferences > Security & Privacy > General** and click the **Open Anyway** button.
  
- **Alternate Method:**  
  Right-click (or Control-click) the binary in Finder and select **Open**. This will prompt a confirmation dialog that allows you to run the application.

#### Windows

- **SmartScreen Warning:**  
  Windows Defender SmartScreen might display a warning. Click **More Info** in the warning dialog, then select **Run Anyway** to launch the application.
  
- **Verification:**  
  Ensure that the binary is downloaded from the official [release page](https://github.com/hiAndrewQuinn/tsk/releases) to maintain security and integrity.

Following these steps will allow you to run tsk safely while acknowledging your operating system’s built-in security measures.

## Data Sources

- **words.txt:** A comprehensive list of Finnish words.
- **glosses.jsonl:** Word definitions (glosses) derived from Wiktionary.

**Note:** The word list and gloss data are derivatives from Wiktionary and are licensed under [CC BY-SA](https://creativecommons.org/licenses/by-sa/3.0/).

## License

- **Go Code:** Released under the [Unlicense](https://unlicense.org/).
- **Data Files (words.txt and glosses.jsonl):** Licensed under [CC BY-SA](https://creativecommons.org/licenses/by-sa/3.0/) due to their derivation from Wiktionary.

## Credits & Acknowledgments

- **Andrew Quinn:** Creator of tsk and a passionate contributor to the Finnish language community.
- [tcell](https://github.com/gdamore/tcell) and [tview](https://github.com/rivo/tview) for their excellent, portable libraries that power the TUI.
- Wiktionary for providing the source data for word definitions.

## Contributing

Contributions are welcome! If you have ideas for improvements, bug fixes, or additional features, please fork the repository and submit a pull request.

## Contact

For more information, visit the [project repository](https://github.com/hiAndrewQuinn/tsk) or check out [Andrew's website](https://andrew-quinn.me/).

---

Happy searching, and thank you for using tsk!
