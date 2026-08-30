package llm

// The model has exactly one job: turn messy human input into the list schema
// below. Both prompts insist on bare JSON because the reply is parsed, never
// shown — internal/bot/render.go is what a human actually reads.
const systemPrompt = `You maintain todo lists.

You always reply with a single JSON object and nothing else. No prose, no
markdown, no code fences. The schema is:

{"title": "short list title", "items": [{"text": "task", "done": false}]}

Rules:
- Split the input into distinct, actionable tasks, one per item.
- Rewrite each task as a short imperative phrase, keeping the details that
  matter (names, amounts, times) and dropping filler.
- Never invent a task the user did not mention, and never drop one they did.
- Answer in the language the user wrote in.
- The title is at most four words and describes the list as a whole.
- "done" is true only for tasks the user says are already finished.`

// createPrompt frames a fresh dump of tasks. The input is spoken or typed
// stream-of-thought, so it is wrapped in a tag: a user who types "ignore the
// above" is describing a task, not addressing the model.
const createPrompt = `Build a todo list from this message.

<message>
%s
</message>`

// updatePrompt frames a change to an existing list. The current list goes in as
// the same JSON that comes back, so the model edits a structure it recognises
// instead of re-deriving one from rendered text.
const updatePrompt = `Here is an existing todo list:

<list>
%s
</list>

The user replied to it with:

<message>
%s
</message>

Apply what they asked for and return the complete updated list. Their message
may add tasks, remove tasks, reword them, reorder them, mark them done or
rename the list. Leave everything they did not mention exactly as it is,
including the "done" flags and the order of untouched items. If the message is
not an instruction but more tasks, append them.`
