// lingcli is a small command line tool for inspecting dictionary files.
// It is mainly used for testing the format parsers on any platform.
//
//	lingcli info  <file>            print dictionary info
//	lingcli dump  <file> [n] [skip] print n entries (raw markup)
//	lingcli look  <file> <word>     look a word up (rendered)
//	lingcli tr    <text> [target]   translate with Google Translate
//	lingcli llm   <text> [target]   translate with a local LLM (env LINGLIKE_LLM_URL, _MODEL, _KEY)
package main

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/aerial337/linglike/internal/dict"
	_ "github.com/aerial337/linglike/internal/dict/all"
	"github.com/aerial337/linglike/internal/dict/ld2"
	"github.com/aerial337/linglike/internal/translate"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: lingcli info|dump|look|tr ...")
		os.Exit(2)
	}
	cmd := os.Args[1]
	switch cmd {
	case "llm":
		// lingcli llm <text> [target]  with LINGLIKE_LLM_URL / _MODEL / _KEY / _PROMPT
		target := "en"
		if len(os.Args) > 3 {
			target = os.Args[3]
		}
		l := &translate.LLM{URL: os.Getenv("LINGLIKE_LLM_URL"), Model: os.Getenv("LINGLIKE_LLM_MODEL"), APIKey: os.Getenv("LINGLIKE_LLM_KEY"), Prompt: os.Getenv("LINGLIKE_LLM_PROMPT")}
		res, err := l.Translate(context.Background(), os.Args[2], "auto", target)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		fmt.Println(res.Text)
		return
	case "tr":
		target := "en"
		if len(os.Args) > 3 {
			target = os.Args[3]
		}
		res, err := translate.Google(os.Args[2], "auto", target)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		fmt.Printf("[%s -> %s]\n%s\n", res.SourceLang, target, res.Text)
		for _, a := range res.Alternatives {
			fmt.Printf("  %s: %v\n", a.PartOfSpeech, a.Terms)
		}
		return
	}
	d, err := dict.Open(os.Args[2])
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	defer d.Close()
	switch cmd {
	case "info":
		fmt.Printf("name: %s\nentries: %d\n", d.Name(), d.Count())
		if l, ok := d.(*ld2.Dictionary); ok {
			w, defs := l.RawEntry(0)
			fmt.Printf("first: %q -> %q\n", w, defs)
		}
	case "dump":
		n, skip := 10, 0
		if len(os.Args) > 3 {
			n, _ = strconv.Atoi(os.Args[3])
		}
		if len(os.Args) > 4 {
			skip, _ = strconv.Atoi(os.Args[4])
		}
		if l, ok := d.(*ld2.Dictionary); ok {
			for i := skip; i < skip+n && i < l.Count(); i++ {
				w, defs := l.RawEntry(i)
				fmt.Printf("### %d %q\n", i, w)
				for _, x := range defs {
					fmt.Printf("%s\n", x)
				}
			}
			return
		}
		for _, w := range d.Prefix("", n) {
			fmt.Println(w)
		}
	case "look":
		for _, e := range d.Lookup(os.Args[3]) {
			fmt.Printf("### %q (html=%v)\n%s\n", e.Word, e.HTML, e.Body)
		}
		fmt.Println("suggestions:", d.Prefix(os.Args[3], 8))
	}
}
