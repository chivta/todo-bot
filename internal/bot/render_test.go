package bot

import (
	"strings"
	"testing"

	"github.com/arvlas/todo-bot/internal/domain"
)

func TestRenderNumbersItemsAndStrikesDoneOnes(t *testing.T) {
	got := render(domain.List{
		Title: "Weekend",
		Items: []domain.Item{
			{Text: "Buy milk"},
			{Text: "Call the dentist", Done: true},
		},
	})

	want := "<b>Weekend</b>\n\n1. Buy milk\n2. ✅ <s>Call the dentist</s>"
	if got != want {
		t.Errorf("render() = %q, want %q", got, want)
	}
}

// One raw "<" in a task makes Telegram reject the whole message, so escaping is
// the difference between a list and silence.
func TestRenderEscapesHTML(t *testing.T) {
	got := render(domain.List{
		Title: "A & B",
		Items: []domain.Item{{Text: "fix <div> & move on"}},
	})

	if strings.Contains(got, "<div>") || !strings.Contains(got, "&lt;div&gt; &amp; move on") {
		t.Errorf("render() left raw HTML in: %q", got)
	}
	if !strings.Contains(got, "<b>A &amp; B</b>") {
		t.Errorf("render() did not escape the title: %q", got)
	}
}

func TestRenderFallsBackToDefaultTitle(t *testing.T) {
	got := render(domain.List{Items: []domain.Item{{Text: "Buy milk"}}})

	if !strings.HasPrefix(got, "<b>"+defaultTitle+"</b>") {
		t.Errorf("render() = %q, want it to start with the default title", got)
	}
}
