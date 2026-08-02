package cli

import (
	"fmt"
	"os"
)

// CompletionCmd outputs shell completion scripts.
func CompletionCmd(args []string, _ bool) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "evoke completion: specify a shell (zsh, bash, fish)")
		return 2
	}

	switch args[0] {
	case "zsh":
		fmt.Print(zshCompletion)
	case "bash":
		fmt.Print(bashCompletion)
	case "fish":
		fmt.Print(fishCompletion)
	default:
		fmt.Fprintf(os.Stderr, "evoke completion: unsupported shell %q (use zsh, bash, or fish)\n", args[0])
		return 2
	}
	return 0
}

const zshCompletion = `#compdef evoke

_evoke() {
    local -a commands
    commands=(
        'login:Sign in to the registry'
        'image:Compose evoke files and generate images'
        'chat:Compose evoke files and chat with a local LLM'
        'inspect:List files matching a tag or show what selected files compose into'
        'knowledge:Build a RAG knowledge database from a directory of markdown'
        'push:Push a .evoke file to the registry'
        'pull:Download a registry artifact to the local library'
        'settings:Manage user settings'
        'completion:Output shell completion script'
    )

    if (( CURRENT == 2 )); then
        _describe 'command' commands
        return
    fi

    case "${words[2]}" in
        image)
            _evoke_complete image
            ;;
        chat)
            _evoke_complete chat
            ;;
        inspect)
            _evoke_complete inspect
            ;;
        knowledge)
            _evoke_complete knowledge
            ;;
        pull)
            _evoke_complete pull
            ;;
    esac
}

_evoke_complete() {
    local subcmd=$1
    local completions
    completions=(${(f)"$(evoke __complete $subcmd ${words[3,CURRENT-1]} "${words[CURRENT]}" 2>/dev/null)"})
    if [[ ${#completions[@]} -gt 0 ]]; then
        compadd -a completions
    fi
}

compdef _evoke evoke
`

const bashCompletion = `_evoke() {
    local cur prev words cword
    _init_completion || return

    if [[ ${cword} -eq 1 ]]; then
        COMPREPLY=($(compgen -W "login image chat inspect knowledge push pull settings completion" -- "${cur}"))
        return
    fi

    case "${words[1]}" in
        image|chat|inspect|knowledge|pull)
            local completions
            completions=$(evoke __complete "${words[1]}" "${words[@]:2:cword-2}" "${cur}" 2>/dev/null)
            COMPREPLY=($(compgen -W "${completions}" -- "${cur}"))
            ;;
    esac
}

complete -F _evoke evoke
`

const fishCompletion = `# Fish completions for evoke
complete -c evoke -f

# Subcommands
complete -c evoke -n '__fish_use_subcommand' -a login -d 'Sign in to the registry'
complete -c evoke -n '__fish_use_subcommand' -a image -d 'Compose evoke files and generate images'
complete -c evoke -n '__fish_use_subcommand' -a chat -d 'Compose evoke files and chat with a local LLM'
complete -c evoke -n '__fish_use_subcommand' -a inspect -d 'List files matching a tag or show what selected files compose into'
complete -c evoke -n '__fish_use_subcommand' -a knowledge -d 'Build a RAG knowledge database from a directory of markdown'
complete -c evoke -n '__fish_use_subcommand' -a push -d 'Push a .evoke file to the registry'
complete -c evoke -n '__fish_use_subcommand' -a pull -d 'Download a registry artifact to the local library'
complete -c evoke -n '__fish_use_subcommand' -a settings -d 'Manage user settings'
complete -c evoke -n '__fish_use_subcommand' -a completion -d 'Output shell completion script'

# Generate completions
complete -c evoke -n '__fish_seen_subcommand_from image' -a '(evoke __complete image (commandline -cop)[3..] (commandline -ct) 2>/dev/null)'

# Chat completions
complete -c evoke -n '__fish_seen_subcommand_from chat' -a '(evoke __complete chat (commandline -cop)[3..] (commandline -ct) 2>/dev/null)'

# Inspect completions
complete -c evoke -n '__fish_seen_subcommand_from inspect' -a '(evoke __complete inspect (commandline -cop)[3..] (commandline -ct) 2>/dev/null)'

# Knowledge completions
complete -c evoke -n '__fish_seen_subcommand_from knowledge' -a '(evoke __complete knowledge (commandline -cop)[3..] (commandline -ct) 2>/dev/null)'

# Pull completions
complete -c evoke -n '__fish_seen_subcommand_from pull' -a '(evoke __complete pull (commandline -cop)[3..] (commandline -ct) 2>/dev/null)'
`
