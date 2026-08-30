package llm

import (
	"errors"
	"testing"

	"github.com/arvlas/todo-bot/internal/domain"
)

func TestParseListReadsBareJSON(t *testing.T) {
	list, err := parseList(`{"title":"Errands","items":[{"text":"Buy milk","done":false},{"text":"Post the parcel","done":true}]}`)
	if err != nil {
		t.Fatalf("parseList() error = %v", err)
	}

	if list.Title != "Errands" || len(list.Items) != 2 {
		t.Fatalf("parseList() = %+v", list)
	}
	if list.Items[1].Text != "Post the parcel" || !list.Items[1].Done {
		t.Errorf("parseList() second item = %+v", list.Items[1])
	}
}

// The prompt asks for bare JSON; tolerating a fence or a stray sentence is the
// cheapest failure mode there is to absorb.
func TestParseListToleratesFencesAndProse(t *testing.T) {
	list, err := parseList("Here you go:\n```json\n{\"title\":\"Errands\",\"items\":[{\"text\":\"Buy milk\"}]}\n```")
	if err != nil {
		t.Fatalf("parseList() error = %v", err)
	}

	if len(list.Items) != 1 || list.Items[0].Text != "Buy milk" {
		t.Errorf("parseList() = %+v", list)
	}
}

func TestParseListDropsEmptyItems(t *testing.T) {
	list, err := parseList(`{"title":"Errands","items":[{"text":"Buy milk"},{"text":"   "}]}`)
	if err != nil {
		t.Fatalf("parseList() error = %v", err)
	}

	if len(list.Items) != 1 {
		t.Errorf("parseList() kept %d items, want 1: %+v", len(list.Items), list.Items)
	}
}

func TestParseListRejectsNonJSON(t *testing.T) {
	_, err := parseList("I'm afraid I can't do that")
	if !errors.Is(err, domain.ErrInvalidInput) {
		t.Errorf("parseList() error = %v, want ErrInvalidInput", err)
	}
}
