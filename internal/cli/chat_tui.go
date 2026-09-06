package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/jesse0michael/evoke/internal/chat"
	"github.com/jesse0michael/evoke/internal/knowledge"
)

// runChatTUI drives the interactive conversation as a full-screen terminal UI:
// a pinned header, a scrolling transcript, and a pinned input line. The character's reply is
// generated non-streaming — a spinner marks where it will land in the log while
// the input line stays put — and Enter is ignored while a reply is in flight, so
// the next message can be composed but not submitted until the current one
// resolves. It is used only on an interactive terminal; piped or non-interactive
// runs use runChatLoop instead.
func runChatTUI(ctx context.Context, plan *chat.Plan, client chatBackend, sess *chat.Session, st chatStyle, verbose bool, backendDone <-chan struct{}, backendErr func() error, knowledgeBases []*knowledge.Base, backendOrigin string) error {
	m := newChatTUIModel(ctx, plan, client, sess, st, verbose, backendDone, backendErr, knowledgeBases, backendOrigin)
	// Deliberately do NOT capture the mouse: mouse reporting would steal native
	// click-drag text selection. Most terminals translate the wheel into ↑/↓ keys
	// for an alt-screen app when the mouse isn't captured, so the viewport still
	// wheel-scrolls (see the arrow-key handling in Update), and selection/copy work
	// as in a normal CLI.
	p := tea.NewProgram(m, tea.WithAltScreen())

	// Quit the program if the session context is cancelled (e.g. SIGTERM) so the
	// backend can still be shut down cleanly by the caller's deferred Close.
	go func() {
		<-ctx.Done()
		p.Quit()
	}()

	final, err := p.Run()
	if err != nil {
		return err
	}
	if fm, ok := final.(chatTUIModel); ok && fm.exitErr != nil {
		return fm.exitErr
	}
	return nil
}

// chatTUIModel is the Bubble Tea model for the interactive chat UI. The session
// remains the source of truth for the transcript sent to the backend; the model
// only adds presentation (a rendered log, a pinned input, and a busy gate).
type chatTUIModel struct {
	ctx            context.Context
	plan           *chat.Plan
	client         chatBackend
	sess           *chat.Session
	st             chatStyle
	verbose        bool
	backendDone    <-chan struct{}
	backendErr     func() error
	knowledgeBases []*knowledge.Base
	backendOrigin  string

	viewport viewport.Model
	input    textinput.Model
	spinner  spinner.Model

	transcript []string // rendered message blocks, newest last
	busy       bool     // a reply is in flight; Enter is gated until it resolves
	ready      bool     // first WindowSizeMsg received (layout known)
	exitErr    error    // non-nil to surface an error after the program exits
}

// chatHeaderHeight is how many terminal rows the pinned header occupies: the
// status line, the command list, and the rule dividing them from the transcript.
// Update reserves exactly this many rows, so header must never wrap past them.
const chatHeaderHeight = 3

// chatCommands is the command list shown in the pinned header. It is the same
// set slash handles, glossed, so the options stay on screen rather than
// scrolling out of the log the way a startup banner would. Quitting is not among
// them: Ctrl-C ends the session cleanly and needs no command of its own.
const chatCommands = "/reset clear history · /context token budget"

// replyMsg carries the result of a backend completion back into the update loop.
type replyMsg struct {
	reply string
	usage chat.Usage
	err   error
}

// backendDeadMsg signals the backend exited unexpectedly.
type backendDeadMsg struct{}

func newChatTUIModel(ctx context.Context, plan *chat.Plan, client chatBackend, sess *chat.Session, st chatStyle, verbose bool, backendDone <-chan struct{}, backendErr func() error, knowledgeBases []*knowledge.Base, backendOrigin string) chatTUIModel {
	ti := textinput.New()
	ti.Prompt = "> "
	ti.Placeholder = "type a message"
	ti.Focus()

	sp := spinner.New()
	sp.Spinner = spinner.Ellipsis

	m := chatTUIModel{
		ctx:            ctx,
		plan:           plan,
		client:         client,
		st:             st,
		verbose:        verbose,
		backendDone:    backendDone,
		backendErr:     backendErr,
		knowledgeBases: knowledgeBases,
		backendOrigin:  backendOrigin,
		input:          ti,
		spinner:        sp,
		sess:           sess,
	}

	// Show the restored dialogue so a resumed conversation opens on its own
	// history instead of a blank pane.
	restored := m.sess.Turns()
	// The opening scene was seeded as a user turn and deliberately never shown;
	// reading it back from the stored transcript must not reveal it now.
	if len(restored) > 0 && plan.Opening != "" && restored[0].Content == plan.Opening {
		restored = restored[1:]
	}
	for _, t := range restored {
		switch t.Role {
		case chat.RoleUser:
			m.transcript = append(m.transcript, m.renderUser(t.Content))
		case chat.RoleAssistant:
			m.transcript = append(m.transcript, m.renderAssistant(t.Content))
		}
	}

	// Seed the opening scene once; the character responds first (see the loop's
	// seedOpening for the same rationale). The seed message is not shown, and a
	// restored conversation already had its scene set.
	if plan.Opening != "" && len(m.sess.Turns()) == 0 {
		m.sess.AddUser(plan.Opening)
		m.busy = true
	}
	return m
}

func (m chatTUIModel) Init() tea.Cmd {
	cmds := []tea.Cmd{textinput.Blink, waitForBackendDeath(m.backendDone)}
	if m.busy {
		cmds = append(cmds, m.spinner.Tick, m.replyCmd())
	}
	return tea.Batch(cmds...)
}

func (m chatTUIModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		// Reserve the header rows and one footer line for the pinned input; the
		// thinking indicator lives inside the transcript (where the reply will
		// land), not here.
		vpHeight := max(1, msg.Height-chatHeaderHeight-1)
		if !m.ready {
			m.viewport = viewport.New(msg.Width, vpHeight)
			m.ready = true
		} else {
			m.viewport.Width = msg.Width
			m.viewport.Height = vpHeight
		}
		m.input.Width = max(1, msg.Width-3) // account for the "> " prompt
		m.refreshViewport()
		return m, nil

	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC:
			return m, tea.Quit
		case tea.KeyEnter:
			if m.busy {
				// Gated: compose freely, but a reply is in flight — do nothing.
				return m, nil
			}
			return m.submit()
		case tea.KeyPgUp, tea.KeyPgDown, tea.KeyUp, tea.KeyDown:
			// Scroll the transcript. These keys don't produce text, so they never
			// conflict with composing a message in the single-line input.
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd
		default:
			// All other keys edit the input, including while busy (type-ahead).
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, cmd
		}

	case spinner.TickMsg:
		if !m.busy {
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		m.refreshViewport() // redraw the in-log thinking indicator with the new frame
		return m, cmd

	case replyMsg:
		m.busy = false
		switch {
		case msg.err != nil && m.ctx.Err() != nil:
			// The session was cancelled mid-generation; exit cleanly.
			return m, tea.Quit
		case msg.err != nil:
			m.sess.DropPendingUser() // never record half a turn
			m.transcript = append(m.transcript, m.st.errorText("error: ")+msg.err.Error())
		default:
			m.sess.AddAssistant(msg.reply)
			m.save()
			m.transcript = append(m.transcript, m.renderAssistant(msg.reply))
			if m.verbose {
				m.transcript = append(m.transcript, m.renderDiagnostics(msg.usage))
			}
		}
		m.refreshViewport()
		return m, nil

	case backendDeadMsg:
		m.exitErr = backendExitError(m.backendErr)
		return m, tea.Quit
	}

	// Forward anything else (e.g. cursor blink) to the input so it keeps ticking.
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m chatTUIModel) View() string {
	if !m.ready {
		return "starting chat…"
	}
	return m.header() + "\n" + m.viewport.View() + "\n" + m.input.View()
}

// header is the chrome pinned above the transcript: who you are talking to, and
// the commands available. Each line is truncated rather than wrapped, so the
// header is exactly chatHeaderHeight rows however narrow the terminal is and the
// viewport arithmetic in Update holds.
func (m chatTUIModel) header() string {
	width := max(1, m.viewport.Width)
	status := fmt.Sprintf("%s · %s (%s) · %d ctx",
		m.plan.Display.CharacterName, m.plan.Display.Model, m.backendOrigin, m.plan.Display.ContextWindow)
	clip := lipgloss.NewStyle().MaxWidth(width)
	return strings.Join([]string{
		clip.Render(m.st.dim(status)),
		clip.Render(m.st.dim(chatCommands)),
		m.st.dim(strings.Repeat("─", width)),
	}, "\n")
}

// submit handles a completed input line: a slash command or a new user message.
func (m chatTUIModel) submit() (tea.Model, tea.Cmd) {
	text := strings.TrimSpace(m.input.Value())
	if text == "" {
		return m, nil
	}
	if strings.HasPrefix(text, "/") {
		return m.slash(text)
	}
	m.transcript = append(m.transcript, m.renderUser(text))
	m.sess.AddUser(text)
	m.retrieveContext(text)
	m.input.Reset()
	m.busy = true
	m.refreshViewport()
	return m, tea.Batch(m.spinner.Tick, m.replyCmd())
}

// slash handles the local commands, mirroring the line loop's handleSlashCommand.
func (m chatTUIModel) slash(cmd string) (tea.Model, tea.Cmd) {
	m.input.Reset()
	switch cmd {
	case "/reset":
		m.sess.Reset()
		// Store the cleared transcript at once, so quitting straight after a
		// reset does not resume the conversation just discarded.
		m.save()
		m.transcript = []string{m.st.dim("(conversation reset)")}
		if m.plan.Opening != "" {
			// Replay the opening so a reset returns to the scene, not a blank stage.
			m.sess.AddUser(m.plan.Opening)
			m.busy = true
			m.refreshViewport()
			return m, tea.Batch(m.spinner.Tick, m.replyCmd())
		}
		m.refreshViewport()
		return m, nil
	case "/context":
		info := m.sess.Context()
		m.transcript = append(m.transcript, m.st.dim(fmt.Sprintf(
			"retained turns: %d · est. input %d / %d available (context %d, reserve %d, margin %d)",
			info.RetainedTurns, info.EstimatedTokens, info.InputBudget, info.ContextWindow, info.OutputReserve, info.SafetyMargin)))
		m.refreshViewport()
		return m, nil
	default:
		m.transcript = append(m.transcript, m.st.dim(fmt.Sprintf("unknown command %q (try /reset, /context)", cmd)))
		m.refreshViewport()
		return m, nil
	}
}

// save records the transcript after a completed turn or a reset. A failed write
// is reported in the log rather than ending the session: durable memory is a
// convenience, not a precondition for talking.
func (m *chatTUIModel) save() {
	if err := m.sess.Save(); err != nil {
		m.transcript = append(m.transcript, m.st.errorText("warning: ")+err.Error())
	}
}

// replyCmd builds the request from the current session and returns a command
// that performs the (non-streaming) completion and reports the result.
func (m chatTUIModel) replyCmd() tea.Cmd {
	req, err := m.sess.Request()
	if err != nil {
		return func() tea.Msg { return replyMsg{err: err} }
	}
	ctx, client := m.ctx, m.client
	return func() tea.Msg {
		reply, usage, err := client.Complete(ctx, req)
		return replyMsg{reply: reply, usage: usage, err: err}
	}
}

// retrieveContext queries all knowledge bases and sets the combined results
// on the session for injection into the next request.
func (m *chatTUIModel) retrieveContext(query string) {
	if len(m.knowledgeBases) == 0 {
		return
	}
	var combined []string
	for _, kb := range m.knowledgeBases {
		result, err := kb.Retrieve(m.ctx, query)
		if err != nil {
			continue
		}
		if result != "" {
			combined = append(combined, result)
		}
	}
	if len(combined) > 0 {
		m.sess.SetRetrievedContext(strings.Join(combined, "\n\n"))
	}
}

// refreshViewport re-renders the transcript into the viewport, wrapping to its
// width. While a reply is in flight it appends a thinking indicator as the last
// line — the same "<Name>: " prefix the reply will use — so the spinner sits
// exactly where the response will appear and is replaced by it in place. It
// follows to the bottom only when the user was already there, so scrolling up to
// read history isn't yanked back down.
func (m *chatTUIModel) refreshViewport() {
	follow := m.viewport.AtBottom()
	blocks := m.transcript
	if m.busy {
		blocks = append(append([]string(nil), m.transcript...), m.thinkingBlock())
	}
	content := strings.Join(blocks, "\n\n")
	if m.viewport.Width > 0 {
		content = lipgloss.NewStyle().Width(m.viewport.Width).Render(content)
	}
	m.viewport.SetContent(content)
	if follow {
		m.viewport.GotoBottom()
	}
}

// thinkingBlock is the transient in-log placeholder shown while the character's
// reply generates; it shares the reply's speaker prefix so the reply supplants it.
func (m chatTUIModel) thinkingBlock() string {
	return m.st.character(m.plan.Display.CharacterName) + ": " + m.spinner.View()
}

func (m chatTUIModel) renderUser(text string) string {
	return m.st.user("You") + ": " + text
}

func (m chatTUIModel) renderAssistant(text string) string {
	return m.st.character(m.plan.Display.CharacterName) + ": " + m.st.markdown(text)
}

func (m chatTUIModel) renderDiagnostics(usage chat.Usage) string {
	info := m.sess.Context()
	if usage.PromptTokens > 0 {
		return m.st.dim(fmt.Sprintf("  [tokens: prompt %d · reply %d · window %d · turns %d]",
			usage.PromptTokens, usage.CompletionTokens, m.plan.Display.ContextWindow, info.RetainedTurns))
	}
	return m.st.dim(fmt.Sprintf("  [tokens: est. input ~%d · window %d · turns %d]",
		info.EstimatedTokens, m.plan.Display.ContextWindow, info.RetainedTurns))
}

// waitForBackendDeath returns a command that resolves when the backend exits,
// so the UI can surface the failure and quit. Nil when there is no channel to
// watch — a server Evoke connected to has no exit of its own to await.
func waitForBackendDeath(done <-chan struct{}) tea.Cmd {
	if done == nil {
		return nil
	}
	return func() tea.Msg {
		<-done
		return backendDeadMsg{}
	}
}
