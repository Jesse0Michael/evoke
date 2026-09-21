package chat

import (
	"fmt"
	"strings"
)

// sceneTurns is how many of the newest messages an extraction carries. A reply
// is often dialogue alone ("I'd love that."), so the scene has to be read from
// the exchange around it rather than the last line; a handful of turns is
// enough to place the subject and cheap to prefill.
const sceneTurns = 4

// sceneMaxTokens caps the extraction. The answer is one line of short tags, so
// anything longer is a model narrating instead of extracting.
const sceneMaxTokens = 2048

// sceneTemperature is deliberately far below a character's. The sampling that
// makes prose feel alive makes tags drift, and an aside is the one place the
// two can be set independently — which is the reason this is a separate request
// rather than something appended to the reply.
const sceneTemperature = 0.2

// sceneSystem replaces the character's system prompt for the extraction rather
// than being appended to it: a roleplay prompt pulls the model toward answering
// in character, and the extractor needs the opposite.
//
// It asks for visual details only. Framing, shot type, and subject count are
// deliberately excluded — those belong to the composition the caller chose
// (a character file's ?PROMPT, or a shot file named on /image), and a model
// inventing "close-up" every turn would fight it. Negatives are excluded for a
// blunter reason: the extraction becomes a positive prompt literal, so there is
// nowhere for an exclusion to go.
const sceneSystem = `You extract image tags from a roleplay transcript. You are not a character and never speak as one.

Describe only the visual details of THIS MOMENT that the transcript establishes: what the subject is wearing, what their face and body are doing, and the place and light around them. The subject's permanent traits should be known to the renderer - do not invent them, only include them if stated in the transcript. Do not describe camera framing, shot type, or subject count.

Include the visual descriptions of the things that are implied in the view from the transaction for objects and environmental details.

Weight the most important detail of the scene in one prose, wrapped and weighted like (important detail of the scene:1.3)

Reply with a comma-separated tags and prose that can be used to generate an image. No sentences, no preamble, no explanation. No negation ("no shoes" draws shoes). No abstractions ("beautiful", "tense"). Prefer the canonical booru tags and phrases`

// sceneInstruction is the final user message. It is a plain restatement rather
// than a fresh set of rules: the constraints belong in the system prompt, where
// they are not competing with the transcript for the model's attention.
const sceneInstruction = "Describe the visual details of the current moment as one line of tags."

// SceneRequest builds the aside that asks the model to describe the current
// moment as image tags.
//
// Build it on the goroutine that owns the session — the returned request is a
// value with no reference back into the transcript, so the completion itself
// can run concurrently with the conversation.
func SceneRequest(sess *Session) CompletionRequest {
	temp := sceneTemperature
	// Thinking is switched off explicitly: a reasoning model can spend the whole
	// output budget on a chain of thought and return empty content, which for an
	// extraction is a total loss.
	thinking := false
	// No seed, ever. mlx_lm.server only batches a request into the running
	// generation when it carries none (`args.seed is None` in _is_batchable), so
	// a seed here would make the aside queue behind the conversation instead of
	// decoding alongside it.
	return sess.Aside(sceneSystem, sceneInstruction, sceneTurns, Sampling{
		Temperature:     &temp,
		MaxOutputTokens: sceneMaxTokens,
		Thinking:        &thinking,
	})
}

// ParseScene reads the tag line out of a model's answer.
//
// Neither backend can constrain the output — llama.cpp accepts a grammar, but
// mlx_lm.server has no `response_format` and no grammar at all, its state
// machine covering only stop words, reasoning, and tool-call framing — so the
// shape arrives by instruction and this cleans up what instruction alone
// cannot: a code fence, and the newlines that would end a value mid-declaration
// once the text is a prompt.
//
// It does not try to repair a model that answered in prose. That failure is
// visible in the reported scene line, where it can be read and the character or
// model changed, rather than silently rewritten into something else.
func ParseScene(text string) (string, error) {
	text = strings.ReplaceAll(text, "```", " ")
	// A leading "json" or "tags" label is what a fence usually carries with it.
	text = strings.Join(strings.Fields(text), " ")
	text = strings.Trim(text, " ,.")
	if text == "" {
		return "", fmt.Errorf("the scene reported nothing")
	}
	return text, nil
}
