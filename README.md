# Linglike

A Lingoes-style dictionary and translation tool for Windows: select text in any
program, press a hotkey (or Ctrl + right-click) and get an instant popup with
dictionary definitions and a Google translation, in the spirit of the classic
Lingoes 2.x dictionary.

```
┌───────────────────────────────────────────────────────────────────────┐
│ [ happy                          ] [Search] [Translate]               │
├───────────────┬───────────────────────────────────────────────────────┤
│ Results       │ ▣ Concise English Dictionary                        ▼ │
│   Concise Eng…│   happy  ['hæpɪ]                                      │
│   Synonym & A…│   adj.  1. enjoying or showing or marked by joy …     │
│   Google Tran…│         2. marked by good fortune …                   │
│ Options       │ ▣ Concise English Synonym and Antonym Dictionary    ▼ │
│   Dictionarie…│   Synonyms  blissful  bright  cheerful  contented …   │
│   Configurat… │   Antonyms  unhappy                                   │
│   Text Transl…│ ▣ Google Translate                                  ▼ │
│ Index         │   glücklich   (English → German)                      │
│   happy       │   adjective: glücklich, froh, zufrieden, fröhlich     │
│   happy hour  │                                                       │
└───────────────┴───────────────────────────────────────────────────────┘
```

## Features

* **Dictionary formats**
  * Lingoes **LD2 / LDF** files (the original Lingoes dictionaries), with the
    Lingoes markup rendered like the original program: pronunciation,
    parts of speech, numbered senses, synonym/antonym links, cross references.
  * **MDict MDX** (versions 1.x and 2.x; zlib and LZO compression;
    key-block encryption type 2) – the most common "modern" dictionary format.
  * **StarDict** (`.ifo` + `.idx`/`.idx.gz` + `.dict`/`.dict.dz` + `.syn`),
    including random access into dictzip files and XDXF/HTML/plain fields.
  * **Plain text** tab separated files (`word<TAB>definition`).
* **Look up selected text anywhere**: a global hotkey (default
  `Ctrl+Alt+D`), `Ctrl + right-click`, or automatically whenever text is
  copied to the clipboard ("clipboard watch"). The popup appears next to the
  mouse pointer, stays on top, and closes when you click elsewhere.
* **Google Translate** of words and whole sentences (auto-detected source
  language, configurable target language) through the free web endpoint –
  no API key needed. Sentences are translated first, single words show
  dictionaries first and the translation with dictionary alternatives after.
* **Main window** modelled on Lingoes: search box, *Results* / *Options*
  tree, index of matching headwords while you type, collapsible per-dictionary
  sections, clickable cross references between entries, search history.
* **Text Translation** window for longer texts (Ctrl+Enter to translate).
* Runs in the **notification area** (tray), single instance, remembers
  window size and settings in `%APPDATA%\Linglike\config.json`.

## Download / build

Every push builds `linglike.exe` on GitHub Actions (see the *Actions* tab,
artifact `linglike-windows-amd64`); tags starting with `v` create a release
with a zip.

To build yourself you need Go 1.24 or newer. It cross-compiles from any OS:

```sh
GOOS=windows GOARCH=amd64 go build -ldflags "-H windowsgui -s -w" -o linglike.exe ./cmd/linglike
```

On Windows (PowerShell):

```powershell
$env:GOOS="windows"; go build -ldflags "-H windowsgui -s -w" -o linglike.exe ./cmd/linglike
```

The executable is self-contained (no runtime to install); it only needs
Windows 7 or later with Internet Explorer's rendering component, which is part
of Windows.

The icon and manifest are embedded from `cmd/linglike/rsrc_windows_*.syso`.
To regenerate them after editing `cmd/linglike/winres/winres.json`:

```sh
go install github.com/tc-hib/go-winres@latest
go run ./tools/mkicon cmd/linglike/winres
cd cmd/linglike && go-winres make --in winres/winres.json --out rsrc
```

## Using it

1. Start `linglike.exe`. Add dictionaries with **Options › Dictionaries...**
   (or simply put the files in a `dictionaries` folder next to the exe, or
   drag dictionary files onto the exe). Lingoes `.ld2` files can be taken
   from an old Lingoes installation (`Lingoes\dict\*.ld2`).
2. Type a word in the search box and press Enter. Click a dictionary in the
   *Results* tree to jump to it, click any linked word to look it up.
3. In any other program, select some text and press **Ctrl+Alt+D** (or hold
   **Ctrl** and right-click). A popup shows the definitions; click *Pin* to
   keep it open, *Open* to continue in the main window.
4. **Options › Configuration...** sets the hotkey, popup size and auto-close
   time, clipboard watching, translation languages, and tray behaviour.

The `lingcli` tool (built from `cmd/lingcli`) inspects dictionaries from the
command line:

```
lingcli info  "Concise English Dictionary.ld2"
lingcli look  "Concise English Dictionary.ld2" happy
lingcli dump  some.mdx 20
lingcli tr    "good morning" de
```

## Notes and limitations

* Lingoes **LDX** files are encrypted/packed variants and are not supported;
  only the open LD2/LDF layout is read (this is the same subset that the known
  open-source LD2 readers support).
* MDX resources (`.mdd` files with images/audio) are not loaded; `sound://`
  links are stripped and images inside entries will not display. MDX
  version 3 files and record-block encryption (registration required) are
  not supported.
* Google Translate is used through its public web endpoint, which is not an
  official API. Google may rate-limit or change it; the app then shows the
  error in the translation section while dictionaries keep working.
* Text is captured from other programs by simulating **Ctrl+C**, like
  Lingoes did. The previous clipboard content is restored afterwards (option).
  Programs running elevated (as administrator) do not accept simulated input
  from a normal process – run Linglike as administrator to capture from them.

## Project layout

```
cmd/linglike        Windows GUI application (walk / Win32)
cmd/lingcli         command line inspector (any OS)
internal/dict       dictionary interface, shared index, format readers:
  ld2/              Lingoes LD2/LDF parser + markup → HTML
  mdx/              MDict MDX parser (zlib/LZO, RIPEMD-128 key decryption)
  stardict/         StarDict reader with dictzip random access
  textdict/         tab separated text dictionaries
internal/translate  Google Translate client
internal/render     HTML pages (Lingoes-like styling)
internal/app        configuration and lookup service
internal/ui         main window, popup, dialogs, tray, hotkeys, hooks
tools/mkicon        generates the application icon
```

## License

MIT – see [LICENSE](LICENSE). The LD2 layout follows the public reverse
engineering by Xiaoyun Zhu (LingoesLd2Reader); the MDX layout follows
`readmdict.py` by Xiaoqiang Wang.
