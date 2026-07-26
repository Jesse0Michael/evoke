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
	evoke "github.com/jesse0michael/evoke/pkg/evoke"
	"github.com/kelseyhightower/envconfig"
)

type chatConfig struct {
	Host           string        `envconfig:"EVOKE_CHAT_HOST" default:"127.0.0.1"`
	Port           int           `envconfig:"EVOKE_CHAT_PORT" default:"8080"`
	Executable     string        `envconfig:"EVOKE_LLAMA_SERVER" default:"llama-server"`
	ModelPaths     string        `envconfig:"EVOKE_CHAT_MODEL_PATH" default:""`
	StartupTimeout time.Duration `envconfig:"EVOKE_CHAT_STARTUP_TIMEOUT" default:"180s"`
}

// Chat resolves and merges the selected .evoke files, compiles a chat plan,
// launches a llama.cpp backend with the resolved model and settings, runs an
// interactive conversation against it, and shuts the backend down on exit.
// Evoke owns the backend process for the duration of the session.
func Chat(args []string, verbose bool) int {
	fs := flag.NewFlagSet("chat", flag.ContinueOnError)
	fs.BoolVar(&verbose, "v", verbose, "verbose output")
	fs.BoolVar(&verbose, "verbose", verbose, "verbose output")
	explain := fs.Bool("explain", false, "compile and print the chat plan without starting a backend")
	noStream := fs.Bool("no-stream", false, "disable streaming output")

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
	// aborted; the managed backend is then shut down by the deferred Close.
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

	res, err := prepareResolution(ctx, inputArgs, settings, manifest)
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

	// Dry-run: inspect the compiled prompt and launch command without starting.
	if *explain {
		fmt.Print(plan.Explain())
		return 0
	}

	fmt.Printf("Starting %s backend (%s) ...\n", plan.Backend, plan.Display.Model)
	lease, err := chat.StartManaged(ctx, plan, cfg.StartupTimeout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "evoke chat: %v\n", err)
		return 1
	}
	// Always stop the owned backend, even on interrupt: use a fresh context so a
	// canceled session context still permits a clean shutdown.
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if cerr := lease.Close(shutdownCtx); cerr != nil {
			fmt.Fprintf(os.Stderr, "evoke chat: backend shutdown: %v\n", cerr)
		}
	}()

	client := chat.NewClient(lease.Endpoint().String(), "")

	// In verbose mode, surface the backend's own log between turns.
	var backendLog func() string
	if dl, ok := lease.(interface{ DrainLog() string }); ok {
		backendLog = dl.DrainLog
	}

	var colorPref *bool
	if settings != nil && settings.Chat != nil {
		colorPref = settings.Chat.Color
	}
	st := newChatStyle(os.Stdout, colorPref)

	if err := runChatLoop(ctx, plan, client, !*noStream, verbose, os.Stdin, os.Stdout, lease.Done(), lease.Err, backendLog, st); err != nil {
		fmt.Fprintf(os.Stderr, "evoke chat: %v\n", err)
		return 1
	}
	return 0
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
// and any backend log output) after each reply. backendDone fires if the managed
// backend exits unexpectedly, with backendErr reporting the cause. It returns nil
// on a clean exit (/exit, EOF, or interrupt).
func runChatLoop(ctx context.Context, plan *chat.Plan, client chatBackend, stream, verbose bool, in io.Reader, out io.Writer, backendDone <-chan struct{}, backendErr func() error, backendLog func() string, st chatStyle) error {
	sess := chat.NewSession(plan)
	printStartupSummary(out, plan, st)

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
		fmt.Fprintf(out, "\n%s> ", st.user("You"))

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
		if quit, handled := handleSlashCommand(out, sess, msg); handled {
			if quit {
				fmt.Fprintln(out, "Session ended.")
				return nil
			}
			continue
		}

		sess.AddUser(msg)
		req, err := sess.Request()
		if err != nil {
			// The message cannot fit the budget; drop it rather than keep a turn
			// that will keep failing.
			sess.DropPendingUser()
			fmt.Fprintf(out, "%s %v\n", st.errorText("error:"), err)
			continue
		}

		fmt.Fprintf(out, "\n%s> ", st.character(plan.Display.CharacterName))
		reply, usage, err := sendReply(ctx, client, req, stream, out)
		fmt.Fprintln(out)
		if err != nil {
			if ctx.Err() != nil {
				fmt.Fprintln(out, "Session ended.")
				return nil
			}
			// The response failed; do not record half a turn.
			sess.DropPendingUser()
			fmt.Fprintf(out, "%s %v\n", st.errorText("error:"), err)
			if verbose {
				printBackendLog(out, backendLog)
			}
			continue
		}
		sess.AddAssistant(reply)
		if verbose {
			printTurnDiagnostics(out, sess, plan, usage, backendLog, st)
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

// sendReply sends the request and returns the assistant reply and token usage,
// streaming deltas to out when enabled.
func sendReply(ctx context.Context, client chatBackend, req chat.CompletionRequest, stream bool, out io.Writer) (string, chat.Usage, error) {
	if stream {
		return client.Stream(ctx, req, func(delta string) {
			fmt.Fprint(out, delta)
		})
	}
	reply, usage, err := client.Complete(ctx, req)
	if err == nil {
		fmt.Fprint(out, reply)
	}
	return reply, usage, err
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
// the model. It returns (quit, handled).
func handleSlashCommand(out io.Writer, sess *chat.Session, msg string) (quit, handled bool) {
	switch msg {
	case "/exit", "/quit":
		return true, true
	case "/reset":
		sess.Reset()
		fmt.Fprintln(out, "(conversation reset)")
		return false, true
	case "/context":
		info := sess.Context()
		fmt.Fprintf(out, "retained turns: %d\n", info.RetainedTurns)
		fmt.Fprintf(out, "estimated input tokens: %d / %d available (context %d, reserve %d, margin %d)\n",
			info.EstimatedTokens, info.InputBudget, info.ContextWindow, info.OutputReserve, info.SafetyMargin)
		return false, true
	}
	if strings.HasPrefix(msg, "/") {
		fmt.Fprintf(out, "unknown command %q (try /exit, /reset, /context)\n", msg)
		return false, true
	}
	return false, false
}

func printStartupSummary(out io.Writer, plan *chat.Plan, st chatStyle) {
	fmt.Fprintf(out, "Character: %s\n", st.character(plan.Display.CharacterName))
	fmt.Fprintf(out, "Backend:   %s (managed)\n", plan.Display.Backend)
	fmt.Fprintf(out, "Model:     %s\n", plan.Display.Model)
	fmt.Fprintf(out, "Context:   %d tokens\n", plan.Display.ContextWindow)
	fmt.Fprintln(out, st.dim("Type /exit to quit, /reset to clear history, /context for budget info."))
}
