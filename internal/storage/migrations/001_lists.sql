-- One row per list message the bot has sent. A reply to a Telegram message
-- carries the id of the message it answers, so (chat_id, message_id) is what
-- turns "user replied to this" into "here is the list they meant".
CREATE TABLE lists (
    chat_id    INTEGER NOT NULL,
    message_id INTEGER NOT NULL,
    payload    TEXT    NOT NULL,
    created_at INTEGER NOT NULL,
    PRIMARY KEY (chat_id, message_id)
);

CREATE INDEX lists_created_at_idx ON lists (created_at);
