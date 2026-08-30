# todo-bot

A Telegram bot that turns a rambling message — typed or spoken — into a
formatted todo list, and edits that list when you reply to it.

## The flow

1. You send tasks: *"tomorrow buy milk and bread, call the dentist about Tuesday,
   already paid the electricity bill"*. Text or a voice message, either works.
2. The bot answers with a numbered list, done tasks struck through.
3. You **reply to that list** to change it: *"drop the dentist one and add pick
   up parcel at 6pm"*. The bot rewrites the list in place.
4. A message that is **not** a reply always starts a new list. The previous one
   stays in the chat, untouched.

Replying to anything the bot has no list for — an old message, its own error
notice — starts a new list too, which is what you would have got by not
replying.

## Setup

```bash
cp .example.env .env   # then fill in BOT_TOKEN
go run ./cmd/todobot
```

`BOT_TOKEN` comes from [@BotFather](https://t.me/BotFather). `CLAUDE_API_KEY`
and `OPENAI_API_KEY` are already in `.env`. Put your own Telegram user ID in
`ALLOWED_USERS` unless you want strangers spending your API budget — an empty
list makes the bot public.

Every setting is documented in `.example.env` and in `internal/config`.

For local development with hot reload: `docker compose up`.

## Costs

List building runs on `claude-haiku-4-5`, the cheapest model the Claude API
offers ($1 / $5 per million tokens). A list is a few hundred tokens, so a heavy
day of use costs cents. Voice messages go through OpenAI's `whisper-1` first,
because Claude models take no audio.

## Layout

```
cmd/todobot        wiring and graceful shutdown
internal/bot       Telegram handlers, rendering, every user-facing string
internal/worker    the job queue: transcribe, build or edit, deliver
internal/llm       Claude prompts and the list schema
internal/speech    voice transcription
internal/storage   SQLite: which list each bot message shows
```

Handlers never block. They answer with a placeholder, queue the job and return;
a worker edits that placeholder into the finished list. A blocked handler would
stall telebot's update loop for every other user.

## Tests

```bash
go test ./...
```

Two tiers. Unit tests cover the parts that break quietly: HTML escaping in the
renderer, parsing the model's reply, the reply-versus-new-list routing, and the
list store.

`test/offline` runs the **built binary** as a real process against a fake Bot
API server (`internal/tgfake`), with stubs for Claude and transcription. Real
polling loop, real routing, real SQLite — only the three hosts are replaced, via
`TELEGRAM_API_URL`, `CLAUDE_API_URL` and `OPENAI_API_URL`. It needs no network
and no API budget, and it covers the wiring a handler test cannot see: the
placeholder arriving before the list, the reply carrying the right list into the
model, voice download through `getFile`, the allow list, and the bot surviving
a 400 from Telegram.

To watch a whole conversation against the **real** Claude API:

```bash
MANUAL=1 go test ./test/offline/ -run TestManualSession -v
```

It prints the chat as a user would see it. Voice still uses the transcription
stub — fake audio has nothing in it to transcribe.
