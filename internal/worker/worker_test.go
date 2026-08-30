package worker

import (
	"context"
	"errors"
	"testing"

	"github.com/arvlas/todo-bot/internal/domain"
)

// fakeRepo holds one list, keyed the way the real store keys them.
type fakeRepo struct {
	lists map[int]domain.List
}

func (r *fakeRepo) Get(_ context.Context, _ int64, messageID int) (domain.List, error) {
	list, ok := r.lists[messageID]
	if !ok {
		return domain.List{}, domain.ErrNotFound
	}

	return list, nil
}

func (r *fakeRepo) Save(context.Context, int64, int, domain.List) error { return nil }

// fakeLister records which of the two paths the pipeline took.
type fakeLister struct {
	created string
	updated string
	base    domain.List
}

func (l *fakeLister) Create(_ context.Context, message string) (domain.List, error) {
	l.created = message

	return domain.List{Title: "new", Items: []domain.Item{{Text: message}}}, nil
}

func (l *fakeLister) Update(_ context.Context, list domain.List, message string) (domain.List, error) {
	l.updated = message
	l.base = list

	return list, nil
}

func TestComposeUpdatesTheListTheUserRepliedTo(t *testing.T) {
	existing := domain.List{Title: "Errands", Items: []domain.Item{{Text: "Buy milk"}}}
	lists := &fakeLister{}
	p := New(&fakeRepo{lists: map[int]domain.List{42: existing}}, lists, nil, nil, Limits{})

	_, err := p.compose(context.Background(), domain.Job{ChatID: 1, ReplyToMessageID: 42}, "add bread")
	if err != nil {
		t.Fatalf("compose() error = %v", err)
	}

	if lists.updated != "add bread" {
		t.Errorf("compose() called Update with %q, want %q", lists.updated, "add bread")
	}
	if lists.created != "" {
		t.Errorf("compose() also built a new list from %q", lists.created)
	}
	if lists.base.Title != existing.Title {
		t.Errorf("compose() edited %+v, want %+v", lists.base, existing)
	}
}

func TestComposeStartsANewListWhenTheMessageIsNotAReply(t *testing.T) {
	lists := &fakeLister{}
	p := New(&fakeRepo{lists: map[int]domain.List{42: {Title: "Errands"}}}, lists, nil, nil, Limits{})

	_, err := p.compose(context.Background(), domain.Job{ChatID: 1}, "buy milk, call mum")
	if err != nil {
		t.Fatalf("compose() error = %v", err)
	}

	if lists.created != "buy milk, call mum" {
		t.Errorf("compose() called Create with %q", lists.created)
	}
	if lists.updated != "" {
		t.Errorf("compose() edited an existing list from %q", lists.updated)
	}
}

// A reply to something the bot has no list for — an old message, its own error
// notice, someone else's — has to behave like a plain message, not fail.
func TestComposeStartsANewListWhenTheRepliedToMessageIsUnknown(t *testing.T) {
	lists := &fakeLister{}
	p := New(&fakeRepo{lists: map[int]domain.List{}}, lists, nil, nil, Limits{})

	_, err := p.compose(context.Background(), domain.Job{ChatID: 1, ReplyToMessageID: 99}, "buy milk")
	if err != nil {
		t.Fatalf("compose() error = %v", err)
	}

	if lists.created != "buy milk" {
		t.Errorf("compose() called Create with %q", lists.created)
	}
}

func TestComposeRejectsAnEmptyMessage(t *testing.T) {
	p := New(&fakeRepo{}, &fakeLister{}, nil, nil, Limits{})

	_, err := p.compose(context.Background(), domain.Job{ChatID: 1}, "   ")
	if !errors.Is(err, domain.ErrInvalidInput) {
		t.Errorf("compose() error = %v, want ErrInvalidInput", err)
	}
}
