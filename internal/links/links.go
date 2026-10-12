// Package links finds the web links in a description, numbers them, and opens
// them with the program configured for their kind.
package links

import (
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"syscall"

	"github.com/geovannewashington/ytwatch/internal/config"
)

var urlPattern = regexp.MustCompile(`https?://[^\s<>"]+`)

// Annotate numbers the links of text. Every link gets a "[n] " marker in front
// of it, so the number survives when a long link is cut off at the pane edge.
// A link that appears twice keeps its first number. The second result lists
// the links in number order: link n is element n-1.
func Annotate(text string) (string, []string) {
	var (
		out     strings.Builder
		urls    []string
		numbers = map[string]int{}
		last    int
	)
	for _, loc := range urlPattern.FindAllStringIndex(text, -1) {
		start, end := loc[0], loc[1]
		url := trimTrailing(text[start:end])
		end = start + len(url)
		number, seen := numbers[url]
		if !seen {
			urls = append(urls, url)
			number = len(urls)
			numbers[url] = number
		}
		out.WriteString(text[last:start])
		out.WriteString("[" + strconv.Itoa(number) + "] ")
		out.WriteString(url)
		last = end
	}
	out.WriteString(text[last:])
	return out.String(), urls
}

// trimTrailing drops punctuation that ends a sentence, not the link: "see
// https://a.com/x." A closing ")" stays only when the link has a matching
// "(", as in some Wikipedia links.
func trimTrailing(url string) string {
	for url != "" {
		switch last := url[len(url)-1]; {
		case strings.IndexByte(".,;:!?'\"]}>", last) >= 0:
		case last == ')' && strings.Count(url, ")") > strings.Count(url, "("):
		default:
			return url
		}
		url = url[:len(url)-1]
	}
	return url
}

// Command returns the command for url: the first handler whose pattern matches
// it, with %l replaced by the link. ok is false when no handler matches, and
// the caller then uses the browser.
func Command(url string, handlers []config.LinkHandler) (argv []string, name string, ok bool) {
	for _, h := range handlers {
		if h.Match != nil && h.Match.MatchString(url) {
			argv = make([]string, len(h.Command))
			for i, arg := range h.Command {
				argv[i] = strings.ReplaceAll(arg, "%l", url)
			}
			return argv, h.Name, true
		}
	}
	return nil, "", false
}

// Run starts argv detached from ytwatch. The link is one argument and is never
// passed through a shell, so a link cannot inject commands.
func Run(argv []string) error {
	if len(argv) == 0 {
		return fmt.Errorf("empty link command")
	}
	path, err := exec.LookPath(argv[0])
	if err != nil {
		return fmt.Errorf("%s is not installed", argv[0])
	}
	cmd := exec.Command(path, argv[1:]...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start %s: %w", argv[0], err)
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
