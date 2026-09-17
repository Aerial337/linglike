# Project requests

This file records what was asked for in this project, in the order it was
asked, in the requester's own words, together with how each request was
understood and what was built for it. It is the requirements history of
Linglike; see [README.md](README.md) for how the finished program works.

---

## 1. Build a Lingoes-like dictionary app for Windows

> can you implement lingoes (old dictionary) like app in windows, for
> selecting text translation, dictionary, support LD2 files and ...
> if it can support newer dictionaries, google translate
> i sent you old lingoes dict picture for you
> you can use any prog lang, i want exe in windows

A screenshot of the original Lingoes 2.x window was attached: search box at
the top, a *Results* / *Options* tree on the left, and stacked
per-dictionary result panels on the right ("Concise English Dictionary",
"English Synonym and Antonym Dictionary", "Kernerman English Multilingual
Online") for the word *happy*.

**What this asked for**

* A desktop program for Windows, delivered as an `.exe`, language of
  implementation left free.
* Look up text selected in other programs ("selecting text translation").
* Read Lingoes **LD2** dictionary files.
* Also read newer dictionary formats, if possible.
* Google Translate as well as dictionaries.
* Look and behave like the program in the screenshot.

**What was built** (commit `94e4467`)

* Written in Go, cross-compiled from Linux to a self-contained
  `linglike.exe`; no runtime to install.
* Dictionary readers written from scratch: Lingoes **LD2/LDF** (with the
  Lingoes markup rendered as HTML: pronunciation, part of speech, numbered
  senses, synonym/antonym links, cross references), **MDict MDX**,
  **StarDict**, and tab separated text files.
* Google Translate through its public web endpoint, no API key.
* Main window modelled on the screenshot, popup near the mouse pointer,
  tray icon, global hotkey, Ctrl + right-click, clipboard watching,
  settings under `%APPDATA%\Linglike`.
* Verified against 100 real Lingoes dictionaries and a real MDX dictionary;
  unit tests for every parser. A follow-up commit (`124d304`) made the tray
  menu open the main window before showing a dialog.

---

## 2. Settings were not saved, and the popup language had to be chosen every time

> its configs when i check them not saved, also every time pop up open i
> should select desired language

**What this asked for**

* Settings ticked in the Configuration dialog had to survive pressing OK
  and restarting the program.
* The translation target language had to be remembered instead of being
  picked again for every popup.

**What was built** (commit `bf40317`)

* Every control in the Configuration dialog is filled from the saved
  configuration when the dialog opens, and the status bar names the file
  that was written.
* Saving is logged, falls back to a direct write if the atomic rename
  fails, and an unreadable settings file raises a warning instead of
  silently resetting to defaults. Portable mode: a `config.json` next to
  the executable is used in preference to `%APPDATA%`.
* A **Translate to** dropdown was added to the main window toolbar and to
  the popup header. The choice is saved immediately and re-runs the current
  translation.

---

## 3. Ticked settings still did not apply; the hotkey stayed "A"

> when i chekmark these settings, changes not apply
> it still shows A as pop, also when i press A it doesnt show pop up

Two screenshots were attached: the Configuration dialog with every checkbox
empty, the hotkey key showing `A` and the language showing *Afrikaans*; and
the main window showing *Translate to: Afrikaans* and the text "press **A**
to look it up in a popup".

**What this asked for**

* Fix the settings dialog for real: it was still writing defaults rather
  than the chosen values.

**What was built** (commit `0b9a2d3`)

* Root cause: the dialog read its checkboxes and dropdowns *after* the
  dialog had closed, at which point the toolkit had already destroyed the
  controls, so every checkbox read as unchecked and every dropdown as its
  first entry, which is `A` for keys and *Afrikaans* for languages. The
  values are now captured in the OK handler while the dialog is still open.
  The rename prompt in the Dictionaries dialog had the same flaw and was
  fixed too.
* The hotkey no longer registers without a modifier, so a bare letter can
  never be swallowed system-wide, and the welcome page refreshes after
  saving.

---

## 4. Open the popup automatically when text is selected, close it on mouse move

> can you add feature that when i select text automatically pop up open?
> lingoes was very good it had option when you select text pop open and when
> you move mouse a bit after a pop up or under it or ... it closed
> automatically

**What this asked for**

* Selecting text with the mouse should open the popup by itself, with no
  key press, exactly as Lingoes did.
* Moving the mouse away from the popup should close it again.

**What was built** (commit `ca3995a`)

* A low level mouse hook detects a drag of at least 6 pixels or a
  double-click outside Linglike's own windows and triggers the lookup.
  Configurable as *Off*, *Always*, or only while holding Ctrl, Shift or Alt.
* The popup closes once the cursor moves farther than a configurable
  distance (default 60 pixels) from it. Pinning it, or working inside it,
  suspends that. Clicks on combo box drop-down lists are ignored so the
  language can be changed without the popup closing.

---

## 5. Can it be built on Linux?

> can i run (build) it also on linux? just answer that yes or no dont do
> anything

Answered without changing anything: **building on Linux: yes** (that is how
every `.exe` in this project was produced, with
`GOOS=windows GOARCH=amd64 go build`). **Running the app on Linux: no** —
the user interface is Win32-only; only the `lingcli` command line tool runs
natively on Linux.

---

## 6. Add local LLM translation next to Google Translate

> beside google translate, can you add local llm translation option for it?
> in settings user should specify url address of server, api key,
> translation prompt, target language

**What this asked for**

* A second translation engine backed by a local LLM server.
* Four settings named explicitly: server URL, API key, translation prompt,
  target language.

**What was built** (commit `b81cf9e`)

* A client for the OpenAI-compatible chat completions API, which covers
  Ollama, LM Studio, llama.cpp, vLLM, LocalAI and hosted providers. The URL
  may be a base address or a full endpoint. Reasoning models'
  `<think>…</think>` blocks, stray quotes and code fences are stripped from
  the answer.
* Settings: enabled, **server URL**, **API key** (sent as a Bearer token),
  model name, **prompt template** with `{text}`, `{target}` and `{source}`
  placeholders, **target language**, timeout, and whether it also runs in
  the popup. A **Test connection** button translates a sample sentence and
  shows the result or the error. The Configuration dialog was reorganised
  into Capture / Translation / General tabs to fit it.
* The result appears as its own "Local LLM (model)" section beside the
  dictionaries and Google Translate, in the main window and in the popup.
  The Text Translation window gained an engine selector, and `lingcli` an
  `llm` command.

---

## 7. Fix right-to-left text

> please fix RTL of google translate results and LLM results in pop up and
> app for language that are RTL like Arabic, Persian, ...

**What this asked for**

* Persian, Arabic and other right-to-left results must be displayed
  right-to-left and right-aligned, in the popup and in the main window.

**What was built** (commit `4a64bcd`)

* The embedded renderer has no `dir="auto"`, so the writing direction is
  detected from the script of the text itself and applied as `dir="rtl"`
  with right alignment and a larger line height.
* Applied to the Google Translate result and its alternatives, the local
  LLM result, dictionary headwords and entry bodies, and the Text
  Translation window's text boxes. Mixed text is decided by majority.

---

## 8. Switch the popup off and on from the right-click menu and the settings

> can you add enable and disable of pop up window when i rightclick in
> toolbar and also in settings?

**What this asked for**

* A master switch for the lookup popup, reachable from a right-click menu
  as well as from the Configuration dialog, so the popup can be silenced
  without quitting the program.

**What was built** (commit `b27cf35`)

* A checkable **Enable popup window** item in three places: the tray icon's
  right-click menu, the right-click menu of the main window toolbar (and of
  the window background), and the *Capture* tab of the Configuration
  dialog. The same menus also carry the hotkey and clipboard toggles, and a
  shortcut to Configuration.
* All copies of the toggles stay in sync, the state is saved immediately,
  and while the popup is off the selection hook, the hotkey and the
  clipboard listener are all unregistered, so nothing is captured. An open
  popup is closed as soon as the switch is turned off.

---

## 9. Write the requests down

> can you write what i write and what i wanted from you in this project in a
> .md file?

This file.

---

## Summary of what the project was meant to be

A modern rebuild of the classic Lingoes dictionary for Windows, shipped as a
single `.exe`, that:

1. reads old Lingoes **LD2** dictionaries as well as newer formats
   (MDict MDX, StarDict, plain text);
2. looks words up from text selected in any other program, opening a popup
   at the mouse pointer and closing it when the mouse moves away;
3. translates with **Google Translate** and with a **local LLM** server the
   user configures (URL, API key, prompt, target language);
4. remembers its settings reliably; and
5. displays right-to-left languages such as Persian and Arabic correctly;
   and
6. can have its popup switched off and on from a right-click menu or the
   settings.
