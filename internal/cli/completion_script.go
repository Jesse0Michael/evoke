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
        'generate:Compose evoke files and generate images'
        'settings:Manage user settings'
        'index:Update the local file index'
        'completion:Output shell completion script'
    )

    if (( CURRENT == 2 )); then
        _describe 'command' commands
        return
    fi

    case "${words[2]}" in
        generate)
            _evoke_generate
            ;;
    esac
}

_evoke_generate() {
    local completions
    completions=(${(f)"$(evoke __complete generate ${words[3,CURRENT-1]} "${words[CURRENT]}" 2>/dev/null)"})
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
        COMPREPLY=($(compgen -W "login generate settings index completion" -- "${cur}"))
        return
    fi

    case "${words[1]}" in
        generate)
            local completions
            completions=$(evoke __complete generate "${words[@]:2:cword-2}" "${cur}" 2>/dev/null)
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
complete -c evoke -n '__fish_use_subcommand' -a generate -d 'Compose evoke files and generate images'
complete -c evoke -n '__fish_use_subcommand' -a settings -d 'Manage user settings'
complete -c evoke -n '__fish_use_subcommand' -a index -d 'Update the local file index'
complete -c evoke -n '__fish_use_subcommand' -a completion -d 'Output shell completion script'

# Generate completions
complete -c evoke -n '__fish_seen_subcommand_from generate' -a '(evoke __complete generate (commandline -cop)[3..] (commandline -ct) 2>/dev/null)'
`
