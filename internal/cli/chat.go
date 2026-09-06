package cli

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/jesse0michael/evoke/internal/chat"
	"github.com/jesse0michael/evoke/internal/knowledge"
	"github.com/jesse0michael/evoke/internal/ollama"
	evoke "github.com/jesse0michael/evoke/pkg/evoke"
	"github.com/kelseyhightower/envconfig"
	"golang.org/x/term"
)

type chatConfig struct {
	Host string `envconfig:"EVOKE_CHAT_HOST" default:"127.0.0.1"`
	Port int    `envconfig:"EVOKE_CHAT_PORT" default:"8080"`
	// Executable is empty by default so the compiled plan's backend chooses its
	// own binary; an explicit value overrides whichever backend is selected.
	Executable     string        `envconfig:"EVOKE_CHAT_EXECUTABLE" default:""`
	ModelPaths     string        `envconfig:"EVOKE_CHAT_MODEL_PATH" default:""`
	StartupTimeout time.Duration `envconfig:"EVOKE_CHAT_STARTUP_TIMEOUT" default:"180s"`
}

// Chat resolves and merges the selected .evoke files, compiles a chat plan,
// launches a backend with the resolved model and settings, runs an interactive
// conversation against it, and shuts the backend down on exit. Evoke owns the
// backend process for the duration of the session.
func Chat(args []string, verbose bool) int {
	fs := flag.NewFlagSet("chat", flag.ContinueOnError)
	fs.BoolVar(&verbose, "v", verbose, "verbose output")
	fs.BoolVar(&verbose, "verbose", verbose, "verbose output")
	streamFlag := fs.Bool("stream", false, "stream tokens as they generate (default: reply shown once complete)")
	noTUI := fs.Bool("no-tui", false, "use the plain line-based interface instead of the full-screen UI")
	newSession := fs.Bool("new", false, "start a new conversation instead of resuming the stored one for these inputs")

	if err := fs.Parse(args); err != nil {
		return 2
	}

	inputArgs := fs.Args()
	if len(inputArgs) == 0 {
		fmt.Fprintln(os.Stderr, "evoke chat: at least one input is required")
		return 2
	}

	var cfg chatConfig
	if err := envconfig.Process("", &cfg); err != nil {
		fmt.Fprintf(os.Stderr, "evoke chat: %v\n", err)
		return 1
	}

	// Interrupt/terminate cancel the session context so an in-flight request is
	// aborted; a backend Evoke launched is then shut down by the deferred Close.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	settings, err := settings()
	if err != nil {
		fmt.Fprintf(os.Stderr, "evoke chat: %v\n", err)
		return 1
	}
	manifest, err := manifest()
	if err != nil {
		fmt.Fprintf(os.Stderr, "evoke chat: %v\n", err)
		return 1
	}

	res, err := prepareResolution(ctx, inputArgs, settings, manifest, verbose)
	if err != nil {
		fmt.Fprintf(os.Stderr, "evoke chat: %v\n", err)
		return 1
	}
	defer func() { _ = res.Close() }()

	if res.manifestChanged {
		if err := saveManifest(manifest); err != nil {
			fmt.Fprintf(os.Stderr, "evoke chat: failed to save manifest: %v\n", err)
			return 1
		}
	}

	docs, err := res.documents(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "evoke chat: %v\n", err)
		return 1
	}
	fmt.Println()

	composition := evoke.Merge(docs)
	composition.Inputs = inputArgs

	if verbose {
		fmt.Println("=== Composition ===")
		fmt.Println(evoke.Render(composition))
	}

	plan, err := chat.Compile(composition, buildTrustedChatConfig(settings, cfg))
	if err != nil {
		fmt.Fprintf(os.Stderr, "evoke chat: %v\n", err)
		return 1
	}

	// Compilation diagnostics explain what a portable file asked for that this
	// machine or backend cannot honor — an unresolvable model, a setting the
	// runtime ignores. They are warnings, not failures, so print them and go on.
	for _, d := range plan.Diagnostics {
		fmt.Fprintf(os.Stderr, "evoke chat: %s\n", d)
	}

	// The conversation is stored per invocation, keyed by the inputs alone, so
	// repeating a command resumes where it left off while editing a character
	// file or its model continues the same conversation. Losing history must
	// never stop a chat from starting, so a broken store is a warning.
	sess := chat.NewSession(plan)
	if err := rememberChat(sess, inputArgs, !*newSession); err != nil {
		fmt.Fprintf(os.Stderr, "evoke chat: memory: %v\n", err)
	}

	// Open knowledge bases for RAG if configured.
	var knowledgeBases []*knowledge.Base
	var embedURL string
	for _, kcfg := range plan.Knowledge {
		if kcfg.DBPath == "" {
			continue
		}
		// Every knowledge config carries the one trusted embed endpoint.
		embedURL = kcfg.EmbedURL
		kb, kerr := knowledge.Open(kcfg)
		if kerr != nil {
			fmt.Fprintf(os.Stderr, "evoke chat: knowledge: %v\n", kerr)
			return 1
		}
		knowledgeBases = append(knowledgeBases, kb)
		if verbose {
			fmt.Printf("Knowledge base loaded: %s (%d chunks, %s)\n", kcfg.DBPath, kb.Len(), kb.Meta().EmbedModel)
		}
	}

	// Retrieval embeds the user's message on every turn, so the embedding
	// endpoint has to stay up for the whole session — start one if nothing is
	// answering, and stop it again only if it was ours.
	if len(knowledgeBases) > 0 {
		models := make([]string, 0, len(knowledgeBases))
		for _, kb := range knowledgeBases {
			models = append(models, kb.EmbedModel())
		}
		embedder, eerr := ollama.Ensure(ctx, embedURL, models, 0)
		if eerr != nil {
			fmt.Fprintf(os.Stderr, "evoke chat: knowledge: %v\n", eerr)
			return 1
		}
		defer func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = embedder.Close(shutdownCtx)
		}()
		if embedder.Started() && verbose {
			fmt.Printf("Started ollama for embedding at %s\n", embedURL)
		}
	}

	backend, err := acquireBackend(ctx, plan, cfg.StartupTimeout, os.Stdout, os.Stdin, isInteractive())
	if err != nil {
		fmt.Fprintf(os.Stderr, "evoke chat: %v\n", err)
		return 1
	}
	// Always stop a backend Evoke started, even on interrupt: use a fresh
	// context so a canceled session context still permits a clean shutdown.
	// Closing one it merely connected to stops nothing.
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if cerr := backend.Close(shutdownCtx); cerr != nil {
			fmt.Fprintf(os.Stderr, "evoke chat: backend shutdown: %v\n", cerr)
		}
	}()

	client := chat.NewClient(backend.Endpoint().String(), "")

	var colorPref *bool
	if settings != nil && settings.Chat != nil {
		colorPref = settings.Chat.Color
	}
	st := newChatStyle(os.Stdout, colorPref)

	// Streaming defaults off (reply is rendered once complete); a persisted
	// setting sets the default, and an explicit --stream flag overrides it.
	stream := false
	if settings != nil && settings.Chat != nil && settings.Chat.Stream != nil {
		stream = *settings.Chat.Stream
	}
	if flagPassed(fs, "stream") {
		stream = *streamFlag
	}

	// The full-screen UI needs an interactive terminal, and its spinner-then-block
	// presentation only fits the non-streaming path; streaming or a non-TTY (pipe,
	// test) falls back to the line-based loop.
	var runErr error
	if !*noTUI && !stream && isInteractive() {
		runErr = runChatTUI(ctx, plan, client, sess, st, verbose, backend.Done(), backend.Err, knowledgeBases, backendOrigin(backend))
	} else {
		runErr = runChatLoop(ctx, plan, client, sess, stream, verbose, os.Stdin, os.Stdout, backend.Done(), backend.Err, backend.DrainLog, st, knowledgeBases, backendOrigin(backend))
	}
	if runErr != nil {
		fmt.Fprintf(os.Stderr, "evoke chat: %v\n", runErr)
		return 1
	}
	return 0
}

// acquireBackend gets the session a backend: use one already running when it
// fits, otherwise start one. A running server with the same model is used as
// is; anything else is a decision the user makes, since whether the cost is
// worth paying depends on what else they have running. Non-interactive runs are
// never asked — they get the refusal with the reason in it, so a script can
// neither hang on a question nor silently talk to the wrong model.
func acquireBackend(ctx context.Context, plan *chat.Plan, timeout time.Duration, out io.Writer, in io.Reader, interactive bool) (*chat.Backend, error) {
	running, err := chat.FindRunning(ctx, plan)
	if err != nil {
		return nil, err
	}
	if running == nil {
		fmt.Fprintf(out, "Starting %s backend (%s) ...\n", plan.Backend, plan.Display.Model)
		return chat.Launch(ctx, plan, timeout)
	}
	if len(running.Mismatches) == 0 {
		fmt.Fprintf(out, "Using the %s backend already running at %s (%s)\n", plan.Backend, running.Endpoint, plan.Display.Model)
		return chat.Connect(plan), nil
	}
	// The mismatch is worth reporting either way; only the offer to override it
	// depends on there being someone at the keyboard to answer.
	mismatch := fmt.Sprintf("a %s backend is already running at %s but does not match this chat:\n  - %s",
		plan.Backend, running.Endpoint, strings.Join(running.Mismatches, "\n  - "))
	if !interactive {
		return nil, fmt.Errorf("refusing to start backend: %s\n%s\nstop it, set a different chat.port in settings, or run interactively to connect anyway",
			mismatch, running.Consequence)
	}
	fmt.Fprintf(out, "%s\n%s\n", capitalize(mismatch), running.Consequence)
	ok, err := confirm(ctx, out, in, "Connect anyway?")
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("refusing to start backend: %s", mismatch)
	}
	return chat.Connect(plan), nil
}

// capitalize upper-cases the first letter, so one message reads as a sentence
// on its own and as a clause after an error prefix.
func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// backendOrigin labels where the session's backend came from, so the startup
// summary says whether exiting will shut anything down.
func backendOrigin(b *chat.Backend) string {
	if b.Launched() {
		return "launched"
	}
	return "already running"
}

// confirm asks a yes/no question, defaulting to no. It reads a byte at a time
// so no input beyond the answer is consumed — the chat loop reads the same
// stdin afterwards — and reads on a goroutine so an interrupt at the prompt
// ends the command instead of blocking on a read that will never return.
func confirm(ctx context.Context, out io.Writer, in io.Reader, question string) (bool, error) {
	fmt.Fprintf(out, "%s [y/N]: ", question)
	type result struct {
		line string
		err  error
	}
	answer := make(chan result, 1)
	go func() {
		var b [1]byte
		var line []byte
		for {
			n, err := in.Read(b[:])
			if n > 0 && b[0] == '\n' {
				break
			}
			if n > 0 {
				line = append(line, b[0])
			}
			if err != nil {
				answer <- result{string(line), err}
				return
			}
		}
		answer <- result{string(line), nil}
	}()
	select {
	case <-ctx.Done():
		fmt.Fprintln(out)
		return false, ctx.Err()
	case r := <-answer:
		if r.err != nil && len(r.line) == 0 {
			return false, nil // EOF with no answer is a no
		}
		switch strings.ToLower(strings.TrimSpace(r.line)) {
		case "y", "yes":
			return true, nil
		}
		return false, nil
	}
}

// rememberChat points a session at where its transcript is stored and restores
// it, unless resume is false (--new). The session is left usable whatever goes
// wrong: an error only says what history was lost, never that the chat cannot
// start.
func rememberChat(sess *chat.Session, inputs []string, resume bool) error {
	dir, err := sessions()
	if err != nil {
		return err
	}
	return sess.Remember(filepath.Join(dir, chat.MemoryKey(inputs)), inputs, resume)
}

// isInteractive reports whether both stdin and stdout are terminals, so the
// full-screen UI can take over the screen and read keystrokes.
func isInteractive() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
}

// buildTrustedChatConfig assembles the trusted chat configuration from local
// settings and the environment. Settings values override environment defaults;
// model directories from both are searched. Model directories and the
// executable are machine-specific and never come from a portable .evoke file.
func buildTrustedChatConfig(s *Settings, cfg chatConfig) chat.TrustedConfig {
	tc := chat.TrustedConfig{
		Executable: cfg.Executable,
		Host:       cfg.Host,
		Port:       cfg.Port,
	}
	addModelDir := func(p string) {
		if abs, err := expandPath(p); err == nil {
			tc.ModelDirs = append(tc.ModelDirs, abs)
		}
	}
	for _, p := range filepath.SplitList(cfg.ModelPaths) {
		addModelDir(p)
	}
	if s != nil && s.Chat != nil {
		if s.Chat.Executable != "" {
			tc.Executable = s.Chat.Executable
		}
		if s.Chat.Host != "" {
			tc.Host = s.Chat.Host
		}
		if s.Chat.Port != 0 {
			tc.Port = s.Chat.Port
		}
		for _, p := range s.Chat.ModelPaths {
			addModelDir(p)
		}
		if s.Chat.EmbedURL != "" {
			tc.EmbedURL = s.Chat.EmbedURL
		}
	}
	return tc
}

// chatBackend is the subset of the transport client the interactive loop needs.
type chatBackend interface {
	Stream(ctx context.Context, req chat.CompletionRequest, onDelta func(string)) (string, chat.Usage, error)
	Complete(ctx context.Context, req chat.CompletionRequest) (string, chat.Usage, error)
}

// runChatLoop drives the interactive conversation. It reads user lines from in,
// writes assistant output to out, handles local slash commands, and streams
// responses. In verbose mode it prints per-turn diagnostics (real token usage
// and any backend log output) after each reply. backendDone fires if a backend
// Evoke launched exits unexpectedly, with backendErr reporting the cause. It returns nil
// on a clean exit (EOF or interrupt).
//
// sess arrives already carrying any transcript restored for these inputs, and
// is saved after every completed turn.
func runChatLoop(ctx context.Context, plan *chat.Plan, client chatBackend, sess *chat.Session, stream, verbose bool, in io.Reader, out io.Writer, backendDone <-chan struct{}, backendErr func() error, backendLog func() string, st chatStyle, knowledgeBases []*knowledge.Base, backendOrigin string) error {
	printStartupSummary(out, plan, st, len(sess.Turns()), backendOrigin)

	// save records the transcript after each completed turn, so an interrupt has
	// nothing to finish before shutdown. A failed write is reported and the
	// conversation continues — durable memory is a convenience, not a
	// precondition for talking.
	save := func() {
		if err := sess.Save(); err != nil {
			fmt.Fprintf(out, "%s %v\n", st.errorText("warning:"), err)
		}
	}

	// retrieveContext queries all knowledge bases and sets the combined results
	// on the session for injection into the next request.
	retrieveContext := func(query string) {
		if len(knowledgeBases) == 0 {
			return
		}
		var combined []string
		for _, kb := range knowledgeBases {
			result, err := kb.Retrieve(ctx, query)
			if err != nil {
				fmt.Fprintf(out, "%s knowledge retrieval: %v\n", st.errorText("warning:"), err)
				continue
			}
			if result != "" {
				combined = append(combined, result)
			}
		}
		if len(combined) > 0 {
			sess.SetRetrievedContext(strings.Join(combined, "\n\n"))
		}
	}

	// respond generates, prints, and records one assistant reply for the pending
	// user turn already added to the session. It returns done=true if the session
	// should end (context cancelled mid-generation); other failures are reported
	// and the pending turn dropped so a half turn is never left behind.
	respond := func() (done bool) {
		req, err := sess.Request()
		if err != nil {
			// The message cannot fit the budget; drop it rather than keep a turn
			// that will keep failing.
			sess.DropPendingUser()
			fmt.Fprintf(out, "%s %v\n", st.errorText("error:"), err)
			return false
		}
		fmt.Fprintf(out, "\n%s: ", st.character(plan.Display.CharacterName))
		reply, usage, err := sendReply(ctx, client, req, stream, out, st)
		fmt.Fprintln(out)
		if err != nil {
			if ctx.Err() != nil {
				fmt.Fprintln(out, "Session ended.")
				return true
			}
			// The response failed; do not record half a turn.
			sess.DropPendingUser()
			fmt.Fprintf(out, "%s %v\n", st.errorText("error:"), err)
			if verbose {
				printBackendLog(out, backendLog)
			}
			return false
		}
		sess.AddAssistant(reply)
		save()
		if verbose {
			printTurnDiagnostics(out, sess, plan, usage, backendLog, st)
		}
		return false
	}

	// seedOpening plays the opening scene: the scenario is added as one turn and
	// the character responds to it. It runs at startup and again after /reset, so
	// the scene is set once (never re-sent in the system prompt) and a reset
	// returns to the opening rather than an empty stage. The seed message itself
	// is not echoed. It is a no-op when there is no scenario, and when a stored
	// conversation was restored — that scene was already set the first time.
	seedOpening := func() (done bool) {
		if plan.Opening == "" || len(sess.Turns()) > 0 {
			return false
		}
		sess.AddUser(plan.Opening)
		return respond()
	}

	if seedOpening() {
		return nil
	}

	// Read input on a goroutine so context cancellation (Ctrl-C) or a backend
	// crash can interrupt a blocking read.
	lines := make(chan string)
	readDone := make(chan error, 1)
	go func() {
		sc := bufio.NewScanner(in)
		sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
		for sc.Scan() {
			lines <- sc.Text()
		}
		readDone <- sc.Err()
	}()

	for {
		fmt.Fprintf(out, "\n%s: ", st.user("You"))

		var msg string
		select {
		case <-ctx.Done():
			fmt.Fprintln(out, "\nSession ended.")
			return nil
		case <-backendDone:
			fmt.Fprintln(out, "\nbackend exited unexpectedly.")
			return backendExitError(backendErr)
		case err := <-readDone:
			fmt.Fprintln(out, "\nSession ended.")
			return err // nil on EOF
		case line := <-lines:
			msg = strings.TrimSpace(line)
		}

		if msg == "" {
			continue
		}
		if reset, handled := handleSlashCommand(out, sess, msg); handled {
			// A reset clears the transcript, including the seeded scene; store
			// that immediately so quitting right after a reset does not resume
			// the conversation just discarded, then replay the opening so the
			// character starts over rather than on a blank stage.
			if reset {
				save()
				if seedOpening() {
					return nil
				}
			}
			continue
		}

		sess.AddUser(msg)
		retrieveContext(msg)
		if respond() {
			return nil
		}
	}
}

func backendExitError(backendErr func() error) error {
	if backendErr != nil {
		if err := backendErr(); err != nil {
			return fmt.Errorf("backend exited: %w", err)
		}
	}
	return fmt.Errorf("backend exited")
}

// sendReply sends the request and returns the assistant reply and token usage.
// When streaming, deltas are written raw as they arrive. Otherwise a spinner
// runs until the full reply is ready, which is then rendered (markdown emphasis)
// in one write.
func sendReply(ctx context.Context, client chatBackend, req chat.CompletionRequest, stream bool, out io.Writer, st chatStyle) (string, chat.Usage, error) {
	if stream {
		return client.Stream(ctx, req, func(delta string) {
			fmt.Fprint(out, delta)
		})
	}
	stop := startSpinner(out, st)
	reply, usage, err := client.Complete(ctx, req)
	stop()
	if err == nil {
		fmt.Fprint(out, st.markdown(reply))
	}
	return reply, usage, err
}

// flagPassed reports whether a flag was explicitly set on the command line,
// distinguishing an explicit value from its default.
func flagPassed(fs *flag.FlagSet, name string) bool {
	found := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			found = true
		}
	})
	return found
}

// startSpinner animates a "." / ".." / "..." / empty cycle at the cursor until
// the returned stop function is called; stop blocks until the goroutine has
// erased the animation. Frames differ in width, so each tick backspaces over the
// previous frame and clears to end of line before drawing the next, leaving the
// speaker label untouched. It is a no-op when styling is disabled (non-terminal
// output), so piped or test output stays clean.
func startSpinner(out io.Writer, st chatStyle) func() {
	if !st.on {
		return func() {}
	}
	done := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		frames := []string{".", "..", "...", ""}
		ticker := time.NewTicker(300 * time.Millisecond)
		defer ticker.Stop()
		prev := 0
		erase := func() {
			if prev > 0 {
				fmt.Fprintf(out, "%s\033[K", strings.Repeat("\b", prev))
			}
		}
		for i := 0; ; i++ {
			select {
			case <-done:
				erase()
				return
			case <-ticker.C:
				erase()
				f := frames[i%len(frames)]
				fmt.Fprint(out, f)
				prev = len(f) // frames are ASCII dots: byte length == column width
			}
		}
	}()
	return func() { close(done); <-stopped }
}

// printTurnDiagnostics prints token accounting and any new backend log output
// after a completed turn (verbose mode). Real backend token counts are shown
// when available; otherwise Evoke's conservative estimate is reported.
func printTurnDiagnostics(out io.Writer, sess *chat.Session, plan *chat.Plan, usage chat.Usage, backendLog func() string, st chatStyle) {
	info := sess.Context()
	var line string
	if usage.PromptTokens > 0 {
		line = fmt.Sprintf("  [tokens: prompt %d · reply %d · window %d · turns %d]",
			usage.PromptTokens, usage.CompletionTokens, plan.Display.ContextWindow, info.RetainedTurns)
	} else {
		line = fmt.Sprintf("  [tokens: est. input ~%d · window %d · turns %d]",
			info.EstimatedTokens, plan.Display.ContextWindow, info.RetainedTurns)
	}
	fmt.Fprintln(out, st.dim(line))
	printBackendLog(out, backendLog)
}

// printBackendLog prints backend log output captured since the last turn, if any.
func printBackendLog(out io.Writer, backendLog func() string) {
	if backendLog == nil {
		return
	}
	logs := strings.TrimSpace(backendLog())
	if logs == "" {
		return
	}
	fmt.Fprintln(out, "  --- llama-server ---")
	for line := range strings.SplitSeq(logs, "\n") {
		fmt.Fprintf(out, "  | %s\n", line)
	}
}

// handleSlashCommand executes local commands. Slash commands are never sent to
// the model. It returns (reset, handled); reset is true when the caller should
// replay the opening scene.
//
// There is no quit command: the session ends on EOF (Ctrl-D) or interrupt
// (Ctrl-C), both of which shut the backend down cleanly and lose at most a reply
// still generating, since the transcript is stored after every completed turn.
func handleSlashCommand(out io.Writer, sess *chat.Session, msg string) (reset, handled bool) {
	switch msg {
	case "/reset":
		sess.Reset()
		fmt.Fprintln(out, "(conversation reset)")
		return true, true
	case "/context":
		info := sess.Context()
		fmt.Fprintf(out, "retained turns: %d\n", info.RetainedTurns)
		fmt.Fprintf(out, "estimated input tokens: %d / %d available (context %d, reserve %d, margin %d)\n",
			info.EstimatedTokens, info.InputBudget, info.ContextWindow, info.OutputReserve, info.SafetyMargin)
		return false, true
	}
	if strings.HasPrefix(msg, "/") {
		fmt.Fprintf(out, "unknown command %q (try /reset, /context)\n", msg)
		return false, true
	}
	return false, false
}

func printStartupSummary(out io.Writer, plan *chat.Plan, st chatStyle, restored int, backendOrigin string) {
	fmt.Fprintf(out, "Character: %s\n", st.character(plan.Display.CharacterName))
	fmt.Fprintf(out, "Backend:   %s (%s)\n", plan.Display.Backend, backendOrigin)
	fmt.Fprintf(out, "Model:     %s\n", plan.Display.Model)
	fmt.Fprintf(out, "Context:   %d tokens\n", plan.Display.ContextWindow)
	if restored > 0 {
		fmt.Fprintf(out, "Resumed:   %d stored turns\n", restored)
	}
	fmt.Fprintln(out, st.dim("Ctrl-C or Ctrl-D to quit, /reset to clear history, /context for budget info."))
}
