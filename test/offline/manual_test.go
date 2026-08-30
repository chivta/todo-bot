package offline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/joho/godotenv"
)

// TestManualSession drives the bot through a whole conversation against the
// fake Bot API and the real Claude API, printing the chat as a user would see
// it. It is a look at the bot working, not an assertion suite — the tests
// beside it are that.
//
//	MANUAL=1 go test ./test/offline/ -run TestManualSession -v
//
// Voice still goes through the transcription stub: fake audio has nothing in it
// to transcribe. Everything downstream of the transcript is real.
func TestManualSession(t *testing.T) {
	if os.Getenv("MANUAL") == "" {
		t.Skip("set MANUAL=1 to run a live session against the real Claude API")
	}

	// Read rather than Load: putting the key in this process's environment
	// would leak it into the bot's too, where a duplicate entry decides which
	// value wins by accident.
	values, err := godotenv.Read(filepath.Join("..", "..", ".env"))
	if err != nil || values["CLAUDE_API_KEY"] == "" {
		t.Skip("no CLAUDE_API_KEY in .env")
	}

	// An empty CLAUDE_API_URL is what sends the bot to the real API.
	h := start(t, map[string]string{
		"CLAUDE_API_KEY": values["CLAUDE_API_KEY"],
		"CLAUDE_API_URL": "",
	})

	say := func(what string) int64 {
		t.Logf("\n\U0001F464 %s", what)
		h.telegram.SendText(chatID, what)

		return h.show()
	}

	reply := func(to int64, what string) int64 {
		t.Logf("\n\U0001F464 (reply) %s", what)
		h.telegram.SendReply(chatID, to, what)

		return h.show()
	}

	list := say("ok tomorrow i need to buy milk and bread, call the dentist about tuesday, " +
		"book train tickets to berlin, and i already paid the electricity bill")
	list = reply(list, "drop the train tickets, i'll do that next week. also add: pick up the parcel at 6pm")
	list = reply(list, "mark the dentist one as done, and the milk should be oat milk")

	t.Logf("\n\U0001F3A4 (voice) %s", "water the plants and take out the recycling")
	h.speech.SetText("water the plants and take out the recycling")
	h.telegram.PutVoice("voice-manual", []byte("OggS-fake-opus-payload"))
	h.telegram.SendVoice(chatID, "voice-manual")
	h.show()

	t.Logf("\nthe earlier list is untouched by that new one, and still repliable:")
	reply(list, "add: buy oat milk twice, we're running out")
}

// show waits for the next list the bot renders and prints it as Telegram would,
// returning the id of the message carrying it.
//
// It waits for a call the bot has not made yet rather than the first one
// matching: every list looks alike, so matching on content alone would return
// the previous one instantly and the session would talk to itself.
func (h *harness) show() int64 {
	h.t.Helper()

	seen := len(h.telegram.Calls())
	deadline := time.Now().Add(wait)

	for time.Now().Before(deadline) {
		calls := h.telegram.Calls()
		for _, call := range calls[min(seen, len(calls)):] {
			if call.Method != "editMessageText" || !strings.Contains(call.Text("text"), "<b>") {
				continue
			}

			h.t.Logf("\U0001F916\n%s", strip(call.Text("text")))

			return call.Int("message_id")
		}
		time.Sleep(50 * time.Millisecond)
	}

	h.t.Fatalf("no new list within %s\n--- bot log ---\n%s", wait, h.bot.Logs())

	return 0
}

// strip renders the bot's HTML the way a Telegram client would.
func strip(html string) string {
	return strings.NewReplacer(
		"<b>", "", "</b>", "", "<s>", "~", "</s>", "~",
		"&amp;", "&", "&lt;", "<", "&gt;", ">",
	).Replace(html)
}
