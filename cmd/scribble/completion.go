package main

import (
	"fmt"
	"io"
)

const bashCompletionScript = `# bash completion for scribble                               -*- shell-script -*-
#
# To load completions in your current shell session:
#   source <(scribble completion)
#
# To install completions permanently:
#   # For current user:
#   mkdir -p ~/.local/share/bash-completion/completions
#   scribble completion > ~/.local/share/bash-completion/completions/scribble
#
#   # Or system-wide (requires root):
#   sudo scribble completion > /etc/bash_completion.d/scribble

_scribble() {
    local cur prev words cword
    if type _init_completion >/dev/null 2>&1; then
        _init_completion -n : || return
    else
        COMPREPLY=()
        cur="${COMP_WORDS[COMP_CWORD]}"
        prev="${COMP_WORDS[COMP_CWORD-1]}"
        words=("${COMP_WORDS[@]}")
        cword=$COMP_CWORD
    fi

    local commands="new show edit append ls search links backlinks projects rename autolink import rm completion help"

    if [[ $cword -eq 1 ]]; then
        COMPREPLY=( $(compgen -W "$commands" -- "$cur") )
        return 0
    fi

    local cmd="${words[1]}"
    case "$cmd" in
        show|edit|append|links|backlinks|autolink|rm)
            if [[ $cword -eq 2 ]]; then
                _scribble_complete_titles "$cur"
            fi
            ;;
        rename)
            if [[ $cword -eq 2 ]]; then
                _scribble_complete_titles "$cur"
            fi
            ;;
        projects)
            if [[ $cword -eq 2 ]]; then
                _scribble_complete_projects "$cur"
            fi
            ;;
        import)
            compopt -o default 2>/dev/null || true
            COMPREPLY=()
            ;;
        completion)
            if [[ $cword -eq 2 ]]; then
                COMPREPLY=( $(compgen -W "bash" -- "$cur") )
            fi
            ;;
        help)
            if [[ $cword -eq 2 ]]; then
                COMPREPLY=( $(compgen -W "$commands" -- "$cur") )
            fi
            ;;
        *)
            ;;
    esac
}

_scribble_complete_titles() {
    local cur="$1"
    local titles
    titles="$(scribble ls 2>/dev/null)" || return 0
    [[ -z "$titles" ]] && return 0

    local prefix="$cur"
    local quoted=0
    if [[ "$cur" == \"* || "$cur" == \'* ]]; then
        quoted=1
        prefix="${cur:1}"
    fi

    local matches=()
    while IFS= read -r line; do
        [[ -z "$line" ]] && continue
        if [[ "${line,,}" == "${prefix,,}"* ]]; then
            if [[ $quoted -eq 1 ]]; then
                matches+=("$line")
            else
                printf -v escaped "%q" "$line"
                matches+=("$escaped")
            fi
        fi
    done <<< "$titles"
    COMPREPLY=("${matches[@]}")
}

_scribble_complete_projects() {
    local cur="$1"
    local projects
    projects="$(scribble projects 2>/dev/null)" || return 0
    [[ -z "$projects" ]] && return 0

    local prefix="$cur"
    local has_hash=0
    if [[ "$prefix" == \#* ]]; then
        has_hash=1
        prefix="${prefix:1}"
    fi

    local matches=()
    while IFS= read -r line; do
        [[ -z "$line" ]] && continue
        if [[ "${line,,}" == "${prefix,,}"* ]]; then
            if [[ $has_hash -eq 1 ]]; then
                matches+=("#$line")
            else
                matches+=("$line")
            fi
        fi
    done <<< "$projects"
    COMPREPLY=("${matches[@]}")
}

complete -F _scribble scribble
`

func cmdCompletion(w io.Writer, args []string) error {
	shell := "bash"
	if len(args) > 0 {
		shell = args[0]
	}
	if shell != "bash" {
		return fmt.Errorf("unsupported shell %q (supported: bash)", shell)
	}
	_, err := io.WriteString(w, bashCompletionScript)
	return err
}
