// Package offline runs the bot as a real process against a fake Bot API server.
//
// Nothing here is mocked inside the bot: it is the production binary, its real
// polling loop, its real routing and its real SQLite store. Only the three
// hosts it talks to are replaced. That covers the wiring, which is where bots
// break — reachability, routing, authorisation, ordering, side effects — with
// no network and no API budget, so it belongs in CI.
package offline

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/arvlas/todo-bot/internal/tgfake"
)

// wait is how long an assertion gives the bot. It is generous because the bot
// polls: a queued update is picked up on the next getUpdates, not instantly.
const wait = 20 * time.Second

// binary is the bot under test, built once for the whole suite.
var binary string

// TestMain builds the bot once into a directory it cleans up itself. Building
// into t.TempDir() would work until the test that created it ends and takes the
// binary with it, mid-suite.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "todobot-offline")
	if err != nil {
		fmt.Fprintln(os.Stderr, "create build dir:", err)
		os.Exit(1)
	}

	binary = filepath.Join(dir, "todobot")
	build := exec.Command("go", "build", "-o", binary, "../../cmd/todobot")
	output, err := build.CombinedOutput()
	if err != nil {
		fmt.Fprintf(os.Stderr, "build bot: %v\n%s", err, output)
		os.RemoveAll(dir)
		os.Exit(1)
	}

	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// harness is one isolated bot: its own fake Telegram, its own stub model, its
// own database. Tests share nothing, so they can run in any order.
type harness struct {
	t        *testing.T
	telegram *tgfake.Server
	claude   *claudeStub
	speech   *speechStub
	bot      *tgfake.Process
}

// start launches the bot with the given extra environment and waits until it is
// actually listening for updates.
func start(t *testing.T, extra map[string]string) *harness {
	t.Helper()

	telegram := tgfake.New()
	claude := newClaudeStub()
	speech := newSpeechStub()

	env := map[string]string{
		"BOT_TOKEN":        "test-token",
		"TELEGRAM_API_URL": telegram.URL(),
		"CLAUDE_API_KEY":   "test-key",
		"CLAUDE_API_URL":   claude.URL(),
		"OPENAI_API_KEY":   "test-key",
		"OPENAI_API_URL":   speech.URL(),
		"DB_PATH":          filepath.Join(t.TempDir(), "todobot.db"),
		// Port 0 keeps parallel tests off each other's toes.
		"HTTP_ADDR": "127.0.0.1:0",
		"LOG_LEVEL": "debug",
	}
	for key, value := range extra {
		env[key] = value
	}

	process, err := tgfake.StartProcess(binary, env)
	if err != nil {
		t.Fatalf("start bot: %v", err)
	}

	h := &harness{t: t, telegram: telegram, claude: claude, speech: speech, bot: process}
	t.Cleanup(func() {
		process.Stop()
		telegram.Close()
		claude.Close()
		speech.Close()
	})

	err = process.WaitForLog("telegram bot listening", wait)
	if err != nil {
		t.Fatalf("bot never started: %v", err)
	}

	return h
}

// waitForEdit blocks until the bot edits a message to text containing want, and
// returns that text.
//
// The list arrives as an edit of the placeholder, not as a new message, so this
// rather than WaitForMessage is what most assertions here need.
func (h *harness) waitForEdit(want string) string {
	h.t.Helper()

	call, err := h.telegram.WaitForCall("editMessageText", "text", want, wait)
	if err != nil {
		// Printing the bot's own log separates "the bot tried and failed" from
		// "the bot never tried", which look identical from the API side.
		h.t.Fatalf("%v\n--- bot log ---\n%s", err, h.bot.Logs())
	}

	return call.Text("text")
}
