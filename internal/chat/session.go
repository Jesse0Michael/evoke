package chat

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Session owns the conversation transcript, enforces the context budget, and
// persists the transcript between runs. It is the source of truth for the
// dialogue: the backend request is rebuilt from the transcript each turn, so
// the backend may cache prefixes but never holds authoritative state, and the
// turns are the only thing there is to store.
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
	// path is the file the transcript is persisted to between runs, empty when
	// the conversation is not remembered. See memory.go.
	path string
	// inputs are the CLI arguments this conversation belongs to, recorded in
	// the stored file for whoever reads the sessions directory.
	inputs []string
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

// Turns returns the full transcript, excluding the system prompt. It is what
// gets persisted; the caller must not modify the returned slice.
func (s *Session) Turns() []Message {
	return s.turns
}

// restore replaces the transcript with a previously stored one, trimming it to
// the alternating user/assistant pairs that buildMessages assumes: it stops at
// the first message breaking the alternation and drops a trailing user message
// with no reply, so a truncated or hand-edited transcript cannot produce a
// request a strict chat template rejects.
func (s *Session) restore(turns []Message) {
	n := 0
	for i, t := range turns {
		want := RoleUser
		if i%2 == 1 {
			want = RoleAssistant
		}
		if t.Role != want {
			break
		}
		n = i + 1
	}
	if n%2 == 1 {
		n--
	}
	s.turns = turns[:n]
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

// memoryVersion is the on-disk schema version of a stored transcript. A file
// written by a different version is ignored rather than migrated: the cost of
// losing a conversation is low, and refusing to start a chat over it is not.
const memoryVersion = 1

// memoryFile is the on-disk representation. Inputs are recorded for whoever
// reads the sessions directory, not for matching — the file name is the key.
type memoryFile struct {
	Version int       `json:"version"`
	Inputs  []string  `json:"inputs,omitempty"`
	Updated time.Time `json:"updated"`
	Turns   []Message `json:"turns"`
}

// Remember gives the session durable storage at path, so repeating a command
// picks the conversation up where it left off, and when resume is true restores
// whatever transcript is already stored there.
//
// The dialogue is stored verbatim rather than summarized. The session is
// authoritative and rebuilds every request from its turns, so the turns are the
// only state there is; and buildMessages already decides how much of a long
// history a request can carry, so restoring far more turns than the context
// window holds is safe — the oldest pairs are simply dropped from the request.
//
// Passing resume false backs the --new flag: the session starts empty and
// leaves the stored conversation on disk until it has a turn of its own to
// replace it with, so starting fresh and quitting immediately loses nothing.
//
// Because losing history must never stop a session from starting, an unreadable
// or malformed file leaves the session empty and usable and returns an error
// describing the problem for the caller to report as a warning.
func (s *Session) Remember(path string, inputs []string, resume bool) error {
	s.path = path
	s.inputs = inputs
	if !resume {
		return nil
	}
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return nil
	case err != nil:
		return fmt.Errorf("failed to read stored conversation %s: %w", path, err)
	}
	var f memoryFile
	if err := json.Unmarshal(data, &f); err != nil {
		return fmt.Errorf("failed to parse stored conversation %s: %w", path, err)
	}
	if f.Version != memoryVersion {
		return fmt.Errorf("stored conversation %s is version %d, not %d; starting a new conversation", path, f.Version, memoryVersion)
	}
	s.restore(f.Turns)
	return nil
}

// Save writes the transcript, replacing whatever was stored, and does nothing
// for a session that was never given somewhere to store it. It is called after
// every completed turn rather than once at exit: an interrupt then has no work
// to do before the backend is shut down, and an abrupt kill loses at most the
// turn in flight.
//
// The data is written to a temporary file in the same directory and renamed
// into place, so an interrupted write cannot leave a half-written transcript.
func (s *Session) Save() error {
	if s.path == "" {
		return nil
	}
	data, err := json.MarshalIndent(memoryFile{
		Version: memoryVersion,
		Inputs:  s.inputs,
		Updated: time.Now().UTC(),
		Turns:   s.turns,
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode conversation: %w", err)
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("failed to create session directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".session-*.json")
	if err != nil {
		return fmt.Errorf("failed to stage conversation: %w", err)
	}
	_, werr := tmp.Write(append(data, '\n'))
	if err := errors.Join(werr, tmp.Close()); err != nil {
		_ = os.Remove(tmp.Name())
		return fmt.Errorf("failed to write conversation: %w", err)
	}
	if err := os.Rename(tmp.Name(), s.path); err != nil {
		_ = os.Remove(tmp.Name())
		return fmt.Errorf("failed to store conversation: %w", err)
	}
	return nil
}

// MemoryKey derives the file name a conversation is stored under from the
// inputs the command was invoked with. Only the inputs decide it: editing a
// character file, changing its model, or a selector resolving to a different
// file all continue the same conversation, because what the user typed is what
// they expect to pick up where it left off.
//
// Inputs are trimmed, lowercased and sorted, so naming the same files in a
// different order resumes the same conversation. The name is a readable slug
// plus a hash of the normalized inputs; the slug is lossy (sanitized and
// truncated) and the hash is what makes the name unique.
func MemoryKey(inputs []string) string {
	norm := make([]string, 0, len(inputs))
	for _, in := range inputs {
		if v := strings.ToLower(strings.TrimSpace(in)); v != "" {
			norm = append(norm, v)
		}
	}
	slices.Sort(norm)
	// The separator keeps ["ab", "c"] from hashing the same as ["a", "bc"].
	sum := sha256.Sum256([]byte(strings.Join(norm, "\x00")))
	return fmt.Sprintf("%s-%x.json", memorySlug(norm), sum[:4])
}

// maxSlugLen bounds the readable part of a session file name; the hash that
// follows it is what guarantees uniqueness, so truncating here is lossless.
const maxSlugLen = 48

// memorySlug renders normalized inputs as a file-name-safe label. It whitelists
// rather than escapes, since an input may be a path or a literal prompt and no
// separator may survive into the name.
func memorySlug(norm []string) string {
	var b strings.Builder
	for i, in := range norm {
		if i > 0 {
			b.WriteByte('+')
		}
		for _, r := range in {
			switch {
			case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '_':
				b.WriteRune(r)
			default:
				b.WriteByte('-')
			}
		}
	}
	slug := b.String()
	for strings.Contains(slug, "--") {
		slug = strings.ReplaceAll(slug, "--", "-")
	}
	slug = strings.Trim(slug, "-._")
	if len(slug) > maxSlugLen {
		slug = strings.Trim(slug[:maxSlugLen], "-._")
	}
	if slug == "" {
		return "session"
	}
	return slug
}
