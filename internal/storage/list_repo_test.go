package storage

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/arvlas/todo-bot/internal/domain"
)

func TestListRepoRoundTrip(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer db.Close()

	repo := NewListRepo(db)
	list := domain.List{Title: "Errands", Items: []domain.Item{
		{Text: "Buy milk"},
		{Text: "Call the dentist", Done: true},
	}}

	err = repo.Save(ctx, 7, 42, list)
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	got, err := repo.Get(ctx, 7, 42)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Title != list.Title || len(got.Items) != 2 || !got.Items[1].Done {
		t.Errorf("Get() = %+v, want %+v", got, list)
	}

	// The same message id in another chat is a different list.
	_, err = repo.Get(ctx, 8, 42)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Get() for another chat error = %v, want ErrNotFound", err)
	}
}

func TestListRepoSaveReplacesTheStoredList(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer db.Close()

	repo := NewListRepo(db)
	for _, title := range []string{"First", "Second"} {
		err = repo.Save(ctx, 7, 42, domain.List{Title: title})
		if err != nil {
			t.Fatalf("Save() error = %v", err)
		}
	}

	got, err := repo.Get(ctx, 7, 42)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Title != "Second" {
		t.Errorf("Get() title = %q, want %q", got.Title, "Second")
	}
}
