package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/arvlas/todo-bot/internal/domain"
)

// ListRepo stores every list the bot has sent, keyed by the message showing it.
type ListRepo struct {
	db *sql.DB
}

// NewListRepo builds a repository over an open database handle.
func NewListRepo(db *sql.DB) *ListRepo {
	return &ListRepo{db: db}
}

// Get returns the list shown by a bot message, or domain.ErrNotFound when the
// message is not one of ours — which is the normal case for a reply to anything
// else, and means "start a new list".
//
// Translating sql.ErrNoRows here is the point of the repository: no layer above
// this one should have to import database/sql to understand a miss.
func (r *ListRepo) Get(ctx context.Context, chatID int64, messageID int) (domain.List, error) {
	var payload string
	err := r.db.QueryRowContext(ctx,
		`SELECT payload FROM lists WHERE chat_id = ? AND message_id = ?`,
		chatID, messageID,
	).Scan(&payload)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.List{}, domain.ErrNotFound
		}
		return domain.List{}, fmt.Errorf("query list: %w", err)
	}

	var list domain.List
	err = json.Unmarshal([]byte(payload), &list)
	if err != nil {
		return domain.List{}, fmt.Errorf("decode list: %w", err)
	}

	return list, nil
}

// Save records the list a message now shows. Re-saving the same message
// replaces the payload, which is what an edited list message needs.
func (r *ListRepo) Save(ctx context.Context, chatID int64, messageID int, list domain.List) error {
	payload, err := json.Marshal(list)
	if err != nil {
		return fmt.Errorf("encode list: %w", err)
	}

	_, err = r.db.ExecContext(ctx,
		`INSERT INTO lists (chat_id, message_id, payload, created_at) VALUES (?, ?, ?, ?)
		 ON CONFLICT(chat_id, message_id) DO UPDATE SET payload = excluded.payload`,
		chatID, messageID, string(payload), time.Now().Unix(),
	)
	if err != nil {
		return fmt.Errorf("insert list: %w", err)
	}

	return nil
}
