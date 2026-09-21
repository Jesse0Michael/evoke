package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/jesse0michael/evoke/internal/chat"
)

// chatImage is the state of the chat session's /image command: scene images
// generated from the conversation as it happens. It is off until /image is
// issued, so an ordinary chat never queues a generation — the feature costs
// GPU time on someone else's machine, and inferring that a conversation wants
// pictures is not something a chat can do.
//
// A generation is one `evoke image` composition: the inputs the user set on
// /image, the .evoke files the chat itself resolved, and the scene as the
// literal prompt.
//
// Argument order is not document order — prepareResolution puts every path,
// registry reference, and literal ahead of anything a selector resolves — so
// what decides the picture is the merge mode rather than the position. That
// works out: IMAGE/LORA/DETAILER settings layer per key with the last writer
// winning, so a pipeline file named on /image sets the sampler, while NAME
// stays singular and the character file's wins, which is what keeps the output
// directory named for the character.
//
// The scene travels as a literal, so literalDocument's empty explicit
// contributions apply: the character's `?APPAREL`, `?ENVIRONMENT` and `?PROMPT`
// defaults are suppressed for the generation. That is mostly what you want,
// since the scene states the clothing and the place itself — but it also drops
// a `?PROMPT` subject count, which is why a count belongs in the /image inputs
// (`/image ill "solo, 1girl"`) when it matters. The matching `?!PROMPT` negative
// is untouched, since channels resolve separately.
//
// Those inputs are the whole setting: naming them turns generation on and
// clearing them turns it off. There is no separate switch, so the header line
// and the state cannot disagree.
type chatImage struct {
	// files are the .evoke files the chat resolved, not the selectors it was
	// started with: a selector re-resolved here could roll a different file
	// than the one the character is being played from.
	files []string
	args  []string // the inputs /image was set with; empty means off
}

// newChatImage prepares the command for a session, carrying over whatever
// setting the resumed conversation was last generating with — so a session
// picked up again is still making the pictures it was, without being asked
// twice. A conversation with none (or one started with --new) opens off.
//
// Only real sources carry over — a literal prompt argument merges into a chat
// with no file behind it, so its source is empty and there is nothing to pass on.
func newChatImage(plan *chat.Plan, sess *chat.Session) chatImage {
	files := make([]string, 0, len(plan.Sources))
	for _, s := range plan.Sources {
		if s != "" {
			files = append(files, s)
		}
	}
	return chatImage{files: files, args: sess.ImageInputs()}
}

// isImageCommand reports whether a line is the /image command rather than a
// longer word that happens to start with it, so both front-ends agree on what
// counts as one.
func isImageCommand(msg string) bool {
	return msg == "/image" || strings.HasPrefix(msg, "/image ")
}

// set applies an /image command line, replacing the inputs with whatever it
// names. It returns the line to report and whether a generation should fire
// from the reply already on screen.
//
// A bare /image clears the inputs, which is what turns generation off — there
// is nothing else to switch. Re-issuing the same inputs fires again: trying a
// setting against the scene already on screen is the whole reason to change one
// mid-conversation.
func (c *chatImage) set(line string, sess *chat.Session) (status string, fire bool) {
	c.args = splitArgs(strings.TrimSpace(strings.TrimPrefix(line, "/image")))
	// The setting belongs to the conversation, so it is stored with it. Writing
	// it here rather than at the next turn means setting it and quitting keeps
	// it, which is the only behavior that makes resuming trustworthy.
	sess.SetImageInputs(c.args)
	if !c.on() {
		return "image generation off", false
	}
	return "image generation on: " + strings.Join(c.args, " "), true
}

// on reports whether a reply generates an image.
func (c chatImage) on() bool { return len(c.args) > 0 }

// label renders the /image entry for the header: the bare command while it is
// off, and what it is set to — highlighted — while it is on, so the setting
// steering every generation stays on screen rather than scrolling out of the log.
func (c chatImage) label(st chatStyle) string {
	if !c.on() {
		return st.dim("/image")
	}
	return st.active("/image - " + strings.Join(c.args, " "))
}

// baseArgs is the fixed part of a generation's command line: the inputs set on
// /image lead, then the .evoke files the chat resolved. The scene is appended
// to this by generate.
func (c chatImage) baseArgs() []string {
	args := make([]string, 0, len(c.args)+len(c.files)+2)
	args = append(args, "image")
	args = append(args, c.args...)
	args = append(args, c.files...)
	return args
}

// promptLiteral prepares text as the composition's literal prompt argument. Its
// own quotes and newlines need no escaping, since it travels as a single
// argument; what it must not be is anything `evoke image` reads as a selector
// or an xN count, which is why the classifier that command uses decides whether
// there is a prompt at all — a one-word scene generates nothing rather than
// resolving as a tag.
func promptLiteral(prompt string) (string, bool) {
	text := strings.Join(strings.Fields(prompt), " ")
	if classifyInput(text).Kind != inputLiteral {
		return "", false
	}
	return text, true
}

// sceneGeneration is what one fire produced: what the scene was read as, what
// `evoke image` reported, and what went wrong. Reported rather than discarded —
// a generation nobody waits on still has to say when it failed.
type sceneGeneration struct {
	scene string
	out   string
	err   error
}

// generate runs one scene generation end to end: ask the model to read the
// moment as visual tags, then hand them to `evoke image` as the composition's
// literal prompt.
//
// req must have been built on the goroutine that owns the session (see
// chat.SceneRequest); everything here is safe to run alongside the conversation.
//
// A failed extraction falls back to the reply itself rather than generating
// nothing: narration is a poor image prompt, but no image at all is worse, and
// the reason travels with it.
func (c chatImage) generate(ctx context.Context, client chatBackend, req chat.CompletionRequest, reply string) sceneGeneration {
	if !c.on() {
		return sceneGeneration{}
	}

	var g sceneGeneration
	prompt, err := extractScene(ctx, client, req)
	if err != nil {
		prompt = reply
		g.scene = fmt.Sprintf("(%v) using the reply: ", err)
	}

	text, ok := promptLiteral(prompt)
	if !ok {
		return sceneGeneration{}
	}
	g.scene += text

	// The prompt goes last, after the inputs that decide how it is rendered.
	g.out, g.err = runEvokeSelf(ctx, append(c.baseArgs(), text))
	return g
}

// extractScene asks the model to read the current moment and cleans up the
// answer.
func extractScene(ctx context.Context, client chatBackend, req chat.CompletionRequest) (string, error) {
	text, _, err := client.Complete(ctx, req)
	if err != nil {
		return "", err
	}
	return chat.ParseScene(text)
}

// lastAssistantReply is the most recent thing the character said — the scene an
// image is generated from. Empty when the character has not spoken yet.
func lastAssistantReply(sess *chat.Session) string {
	turns := sess.Turns()
	for i := len(turns) - 1; i >= 0; i-- {
		if turns[i].Role == chat.RoleAssistant {
			return turns[i].Content
		}
	}
	return ""
}

// lastLine is the last non-empty line of a child command's output: `evoke
// image` prints its resolution trace before the result, and only the result is
// worth a line in the transcript.
func lastLine(out string) string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if line := strings.TrimSpace(lines[i]); line != "" {
			return line
		}
	}
	return ""
}
