package chat

import "fmt"

// Session owns the in-memory conversation transcript and enforces the context
// budget. It is the source of truth for the dialogue: the backend request is
// rebuilt from the transcript each turn, so the backend may cache prefixes but
// never holds authoritative state.
type Session struct {
	plan   *Plan
	system Message
	// turns holds the alternating user/assistant dialogue in order. A trailing
	// lone user message is the pending turn awaiting a response.
	turns []Message
	// retrievedContext is transient RAG context injected before the latest user
	// message in the request. It is not stored as a turn and is cleared after
	// each request is built.
	retrievedContext string
}

// NewSession creates a session seeded with the compiled system prompt.
func NewSession(plan *Plan) *Session {
	return &Session{
		plan:   plan,
		system: Message{Role: RoleSystem, Content: plan.SystemPrompt},
	}
}

// AddUser appends a user message as the pending turn.
func (s *Session) AddUser(content string) {
	s.turns = append(s.turns, Message{Role: RoleUser, Content: content})
}

// SetRetrievedContext sets transient RAG context to be injected into the next
// request. The context augments the latest user message in the request but is
// not stored in the transcript. It is consumed (cleared) when Request is called.
func (s *Session) SetRetrievedContext(ctx string) {
	s.retrievedContext = ctx
}

// AddAssistant records a completed assistant response. It must be called only
// after a response finishes successfully, exactly once per response.
func (s *Session) AddAssistant(content string) {
	s.turns = append(s.turns, Message{Role: RoleAssistant, Content: content})
}

// Reset clears the conversational turns while retaining the compiled system
// prompt.
func (s *Session) Reset() {
	s.turns = nil
}

// DropPendingUser removes a trailing unpaired user message. It is used when a
// message could not be sent or its response failed, so a half turn is not left
// in the transcript.
func (s *Session) DropPendingUser() {
	if n := len(s.turns); n%2 == 1 {
		s.turns = s.turns[:n-1]
	}
}

// Request builds the backend request for the current transcript, applying the
// sliding-window context budget.
func (s *Session) Request() (CompletionRequest, error) {
	msgs, err := s.buildMessages()
	if err != nil {
		return CompletionRequest{}, err
	}
	// The request names the model by the same reference the backend was
	// launched with, not the logical CHAT name. mlx_lm.server takes the body's
	// model field seriously: an unrecognized value is loaded as a fresh model
	// (and a bare name that is not a path relative to its working directory
	// becomes a Hugging Face download), so a model resolved out of a
	// chat.model_paths directory has to travel as its resolved path to match
	// the one already in memory.
	return CompletionRequest{
		Model:    cmpOr(s.plan.ModelPath, s.plan.Model),
		Messages: msgs,
		Sampling: s.plan.Sampling,
	}, nil
}

// buildMessages applies the deterministic sliding-window policy: the system
// prompt is always pinned, the newest user message is always retained, and the
// oldest complete user/assistant pairs are dropped first. A pair is never split
// in half. The result always begins with the system message followed by a user
// turn, so it satisfies strict-alternation chat templates. If the pinned
// content still cannot fit, it returns an error rather than silently truncating.
//
// When retrievedContext is set, it is prepended to the latest user message in
// the returned request (not stored in the transcript) so the model sees the
// relevant knowledge alongside the question.
func (s *Session) buildMessages() ([]Message, error) {
	budget := s.plan.History.InputBudget(s.plan.Sampling.MaxOutputTokens)

	// Count complete pairs, excluding any trailing pending user message.
	paired := len(s.turns)
	if paired%2 == 1 {
		paired--
	}
	maxPairs := paired / 2

	assemble := func(dropPairs int) []Message {
		msgs := make([]Message, 0, len(s.turns)+1)
		msgs = append(msgs, s.system)
		retained := s.turns[dropPairs*2:]
		msgs = append(msgs, retained...)

		// Inject retrieved context into the latest user message (the pending
		// turn at the end). This augments only the request, not the stored
		// transcript, so the model sees the context but the history stays clean.
		if s.retrievedContext != "" && len(msgs) > 1 {
			last := &msgs[len(msgs)-1]
			if last.Role == RoleUser {
				last.Content = s.retrievedContext + "\n\n" + last.Content
			}
		}
		return msgs
	}

	for dropPairs := 0; dropPairs <= maxPairs; dropPairs++ {
		msgs := assemble(dropPairs)
		if estimateTokens(msgs) <= budget {
			s.retrievedContext = "" // consumed
			return msgs, nil
		}
	}

	minimal := assemble(maxPairs)
	return nil, fmt.Errorf("conversation cannot fit the context window: the system prompt and newest message need ~%d tokens but only %d are available (increase context_window or shorten the character prompt)",
		estimateTokens(minimal), budget)
}

// ContextInfo summarizes the current context budget for the /context command.
type ContextInfo struct {
	RetainedTurns   int
	EstimatedTokens int
	InputBudget     int
	ContextWindow   int
	OutputReserve   int
	SafetyMargin    int
}

// Context reports the current context accounting for what is actually sent
// (system prompt plus retained turns).
func (s *Session) Context() ContextInfo {
	msgs := make([]Message, 0, len(s.turns)+1)
	msgs = append(msgs, s.system)
	msgs = append(msgs, s.turns...)
	return ContextInfo{
		RetainedTurns:   len(s.turns),
		EstimatedTokens: estimateTokens(msgs),
		InputBudget:     s.plan.History.InputBudget(s.plan.Sampling.MaxOutputTokens),
		ContextWindow:   s.plan.History.ContextWindow,
		OutputReserve:   s.plan.Sampling.MaxOutputTokens,
		SafetyMargin:    s.plan.History.SafetyMargin,
	}
}

// estimateTokens conservatively estimates the token count of a message list.
// A local backend exposes no reliable tokenizer through this path, so this
// deliberately over-counts (~4 chars/token, rounded up, plus per-message
// framing) and relies on the configured safety margin to absorb the slack.
func estimateTokens(msgs []Message) int {
	const perMessageOverhead = 4
	total := 3 // priming tokens for the reply
	for _, m := range msgs {
		total += (len(m.Content)+3)/4 + perMessageOverhead
	}
	return total
}
