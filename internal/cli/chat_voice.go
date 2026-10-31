package cli

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/jesse0michael/evoke/internal/audio"
	"github.com/jesse0michael/evoke/internal/chat"
	evoke "github.com/jesse0michael/evoke/pkg/evoke"
)

// chatVoice is the state of the chat session's /voice command: the assistant's
// reply spoken aloud as it arrives. It is off until /voice is issued — the same
// reasoning as /image: nothing about an ordinary conversation says it wants to
// be heard, and starting a second local model is not free.
//
// Unlike /image, there is nothing to configure — it is a bare toggle, because
// the speaker is a fact about the character (VOICE/AUDIO), not a setting the
// caller tunes per message.
//
// The backend it talks to is resident rather than one-shot: Evoke launches
// mlx_audio.server once and reuses it for every turn, the same lifecycle
// evoke chat already gives the LLM backend, rather than paying Generate's full
// model load on every reply. Turning voice off does not stop that backend — it
// idles, so turning voice back on later is instant — only the end of the
// session does (see close).
type chatVoice struct {
	enabled bool

	comp    *evoke.Composition
	trusted audio.Trusted
	runtime audio.ServerTrusted

	player         string
	startupTimeout time.Duration

	// plan and backend are filled in by the first successful toggle-on and kept
	// for the rest of the session; see acquireVoiceBackend.
	plan    *audio.Plan
	backend *audio.Backend
}

// newChatVoice prepares the command for a session. comp is the same resolved
// composition the chat Plan was compiled from — VOICE and AUDIO are declarations
// the chat prompt compiler already excludes, so nothing here reaches the dialogue.
func newChatVoice(comp *evoke.Composition, trusted audio.Trusted, runtime audio.ServerTrusted, player string, startupTimeout time.Duration) chatVoice {
	return chatVoice{comp: comp, trusted: trusted, runtime: runtime, player: player, startupTimeout: startupTimeout}
}

// isVoiceCommand reports whether a line is the /voice command. Unlike /image it
// takes no arguments — there is nothing to set, only to flip.
func isVoiceCommand(msg string) bool { return msg == "/voice" }

// on reports whether a reply is spoken.
func (c chatVoice) on() bool { return c.enabled }

// label renders the /voice entry for the header, matching /image's convention:
// dim while off, highlighted while on, so the setting steering every reply
// stays on screen rather than scrolling out of the log.
func (c chatVoice) label(st chatStyle) string {
	if !c.enabled {
		return st.dim("/voice")
	}
	return st.active("/voice")
}

// toggle flips /voice. Turning off needs no I/O. Turning on may need to compile
// the speaker and launch the backend — slow enough that the caller decides how
// to run it: the line loop calls this inline and blocks, the TUI instead builds
// a tea.Cmd around acquireVoiceBackend so the event loop is never blocked.
//
// sess is where the setting is persisted, the same way /image's is — so a
// session resumed later picks up still speaking, without being asked twice.
func (c *chatVoice) toggle(ctx context.Context, sess *chat.Session, report func(string)) error {
	if c.enabled {
		c.enabled = false
		sess.SetVoiceOn(false)
		return nil
	}
	plan, backend, err := acquireVoiceBackend(ctx, c.comp, c.trusted, c.runtime, c.startupTimeout, c.plan, c.backend, report)
	c.plan, c.backend = plan, backend
	if err != nil {
		return err
	}
	c.enabled = true
	sess.SetVoiceOn(true)
	return nil
}

// acquireVoiceBackend compiles the speaker (once) and finds or launches the
// backend (once), reusing whatever the caller already has — which is how voice
// stays resident across an off/on cycle within one session instead of being
// relaunched. It touches the network and the process table, so it is kept free
// of any *chatVoice state: the TUI calls it from inside a tea.Cmd, where
// mutating shared model state directly would race the event loop.
func acquireVoiceBackend(ctx context.Context, comp *evoke.Composition, trusted audio.Trusted, runtime audio.ServerTrusted, startupTimeout time.Duration, plan *audio.Plan, backend *audio.Backend, report func(string)) (*audio.Plan, *audio.Backend, error) {
	if plan == nil {
		p, err := audio.Compile(comp, trusted)
		if err != nil {
			return nil, backend, fmt.Errorf("no voice to speak with: %w", err)
		}
		// Compiling warns about things a portable file carries that this target
		// cannot honor. They are reported for the same reason the chat plan's
		// are: a diagnostic that is compiled and discarded hides exactly the
		// misconfiguration it exists to catch.
		if report != nil {
			for _, d := range p.Diagnostics {
				report(d)
			}
		}
		plan = p
	}
	if backend != nil {
		return plan, backend, nil
	}
	if audio.FindRunning(runtime) {
		if report != nil {
			report(fmt.Sprintf("Using the voice backend already running at %s", runtime.Addr()))
		}
		return plan, audio.Connect(runtime), nil
	}
	if report != nil {
		report("Starting voice backend ...")
	}
	b, err := audio.Launch(ctx, runtime, startupTimeout)
	if err != nil {
		return plan, nil, err
	}
	return plan, b, nil
}

// speak synthesizes reply through the resident backend and plays it, deleting
// the audio the moment playback ends — nothing of a spoken reply is kept. It is
// a no-op when voice is off or the reply is empty (e.g. a dropped turn), and
// returns an error to report rather than panicking on a nil plan/backend: both
// are only ever nil when voice is also off.
func (c chatVoice) speak(ctx context.Context, reply string) error {
	if !c.enabled || strings.TrimSpace(reply) == "" {
		return nil
	}
	data, unsupported, err := audio.Speak(ctx, c.backend, c.plan, reply)
	if err != nil {
		return err
	}
	if len(unsupported) > 0 {
		return fmt.Errorf("spoke, but the resident backend has no field for AUDIO %s (evoke audio honors them)",
			strings.Join(unsupported, ", "))
	}
	return audio.Play(ctx, c.player, "", c.plan.Format, data)
}

// close stops the backend if this session started it. Safe to call even when
// voice was never turned on, since a nil backend has nothing to close — the
// same way closing a chat LLM backend Evoke never launched is a no-op.
func (c *chatVoice) close(ctx context.Context) error {
	if c.backend == nil {
		return nil
	}
	return c.backend.Close(ctx)
}

// voiceConfig is the machine-local configuration for the resident voice
// backend, read from the environment the same way chatConfig is for the LLM
// backend: a portable .evoke file names a speaker, never a host, port, or
// player.
type voiceConfig struct {
	Host           string        `envconfig:"EVOKE_AUDIO_HOST" default:"127.0.0.1"`
	Port           int           `envconfig:"EVOKE_AUDIO_PORT" default:"8090"`
	Executable     string        `envconfig:"EVOKE_AUDIO_SERVER_EXECUTABLE" default:""`
	Player         string        `envconfig:"EVOKE_AUDIO_PLAYER" default:""`
	StartupTimeout time.Duration `envconfig:"EVOKE_AUDIO_STARTUP_TIMEOUT" default:"180s"`
}

// reportLine prints a status line the way acquireBackend's own startup
// messages are printed, for the line interface's synchronous toggle.
func reportLine(out io.Writer) func(string) {
	return func(s string) { fmt.Fprintln(out, s) }
}
