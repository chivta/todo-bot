package bot

import (
	"fmt"
	"strings"

	"github.com/arvlas/todo-bot/internal/domain"
)

const (
	// defaultTitle stands in when the model returns none.
	defaultTitle = "To do"
	// Items are numbered so a user can say "drop the second one" and be
	// understood, and done ones are struck through rather than removed.
	itemFormat     = "%d. %s"
	doneItemFormat = "%d. ✅ <s>%s</s>"
)

// render turns a list into the HTML the user sees. This is the only place a
// domain.List becomes text.
func render(list domain.List) string {
	title := list.Title
	if strings.TrimSpace(title) == "" {
		title = defaultTitle
	}

	lines := make([]string, 0, len(list.Items)+2)
	lines = append(lines, "<b>"+escape(title)+"</b>", "")

	for i, item := range list.Items {
		format := itemFormat
		if item.Done {
			format = doneItemFormat
		}
		lines = append(lines, fmt.Sprintf(format, i+1, escape(item.Text)))
	}

	return strings.Join(lines, "\n")
}
