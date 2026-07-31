package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"slices"
)

// SettingsCmd manages the user settings file.
func SettingsCmd(args []string, _ bool) int {
	fs := flag.NewFlagSet("settings", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return 2
	}

	sub := fs.Args()
	if len(sub) == 0 {
		return settingsShow()
	}

	switch sub[0] {
	case "set":
		return settingsSet(sub[1:])
	case "remove":
		return settingsRemove(sub[1:])
	default:
		fmt.Fprintf(os.Stderr, "evoke settings: unknown subcommand %q\n", sub[0])
		settingsUsage()
		return 2
	}
}

func settingsShow() int {
	s, err := settings()
	if err != nil {
		fmt.Fprintf(os.Stderr, "evoke settings: %v\n", err)
		return 1
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "evoke settings: %v\n", err)
		return 1
	}
	fmt.Println(string(data))
	return 0
}

func settingsSet(args []string) int {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "evoke settings set: requires <key> <value>")
		settingsUsage()
		return 2
	}
	key, value := args[0], args[1]

	s, err := settings()
	if err != nil {
		fmt.Fprintf(os.Stderr, "evoke settings set: %v\n", err)
		return 1
	}

	switch key {
	case "path":
		abs, err := expandPath(value)
		if err != nil {
			fmt.Fprintf(os.Stderr, "evoke settings set: %v\n", err)
			return 1
		}
		if slices.Contains(s.Paths, abs) {
			fmt.Fprintf(os.Stderr, "path already configured: %s\n", abs)
			return 0
		}
		s.Paths = append(s.Paths, abs)
	case "chat.color":
		v, ok := parseBoolSetting(value)
		if !ok {
			fmt.Fprintln(os.Stderr, "evoke settings set: chat.color must be on, off, or auto")
			return 2
		}
		ensureChat(s).Color = v
	case "chat.stream":
		v, ok := parseBoolSetting(value)
		if !ok {
			fmt.Fprintln(os.Stderr, "evoke settings set: chat.stream must be on, off, or auto")
			return 2
		}
		ensureChat(s).Stream = v
	case "chat.model_path":
		abs, err := expandPath(value)
		if err != nil {
			fmt.Fprintf(os.Stderr, "evoke settings set: %v\n", err)
			return 1
		}
		if slices.Contains(ensureChat(s).ModelPaths, abs) {
			fmt.Fprintf(os.Stderr, "model path already configured: %s\n", abs)
			return 0
		}
		ensureChat(s).ModelPaths = append(ensureChat(s).ModelPaths, abs)
	default:
		fmt.Fprintf(os.Stderr, "evoke settings set: unknown key %q\n", key)
		settingsUsage()
		return 2
	}

	if err := saveSettings(s); err != nil {
		fmt.Fprintf(os.Stderr, "evoke settings set: %v\n", err)
		return 1
	}
	return 0
}

func settingsRemove(args []string) int {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "evoke settings remove: requires <key> <value>")
		settingsUsage()
		return 2
	}
	key, value := args[0], args[1]

	s, err := settings()
	if err != nil {
		fmt.Fprintf(os.Stderr, "evoke settings remove: %v\n", err)
		return 1
	}

	switch key {
	case "path":
		abs, err := expandPath(value)
		if err != nil {
			fmt.Fprintf(os.Stderr, "evoke settings remove: %v\n", err)
			return 1
		}
		idx := slices.Index(s.Paths, abs)
		if idx == -1 {
			fmt.Fprintf(os.Stderr, "path not configured: %s\n", abs)
			return 0
		}
		s.Paths = slices.Delete(s.Paths, idx, idx+1)
	case "chat.model_path":
		abs, err := expandPath(value)
		if err != nil {
			fmt.Fprintf(os.Stderr, "evoke settings remove: %v\n", err)
			return 1
		}
		if s.Chat == nil {
			fmt.Fprintf(os.Stderr, "model path not configured: %s\n", abs)
			return 0
		}
		idx := slices.Index(s.Chat.ModelPaths, abs)
		if idx == -1 {
			fmt.Fprintf(os.Stderr, "model path not configured: %s\n", abs)
			return 0
		}
		s.Chat.ModelPaths = slices.Delete(s.Chat.ModelPaths, idx, idx+1)
	default:
		fmt.Fprintf(os.Stderr, "evoke settings remove: unknown key %q\n", key)
		settingsUsage()
		return 2
	}

	if err := saveSettings(s); err != nil {
		fmt.Fprintf(os.Stderr, "evoke settings remove: %v\n", err)
		return 1
	}
	return 0
}

// ensureChat returns the chat settings, allocating them if unset.
func ensureChat(s *Settings) *ChatSettings {
	if s.Chat == nil {
		s.Chat = &ChatSettings{}
	}
	return s.Chat
}

// parseBoolSetting parses a tri-state on/off/auto value. "auto" resolves to a
// nil pointer (meaning: fall back to auto-detection). ok is false for anything else.
func parseBoolSetting(value string) (result *bool, ok bool) {
	switch value {
	case "on", "true":
		b := true
		return &b, true
	case "off", "false":
		b := false
		return &b, true
	case "auto":
		return nil, true
	default:
		return nil, false
	}
}

func settingsUsage() {
	fmt.Fprint(os.Stderr, `Usage:
    evoke settings                        Show current settings
    evoke settings set path <dir>         Add a source path
    evoke settings remove path <dir>      Remove a source path
    evoke settings set chat.model_path <dir>      Add a model/knowledge search path
    evoke settings remove chat.model_path <dir>   Remove a model/knowledge search path
    evoke settings set chat.color <on|off|auto>   Style chat output
    evoke settings set chat.stream <on|off|auto>  Stream chat replies as they generate
`)
}
