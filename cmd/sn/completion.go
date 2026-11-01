package main

import (
	"fmt"
	"io"
)

const bashCompletionScript = `# bash completion for sn                                     -*- shell-script -*-
#
# To load completions in your current shell session:
#   source <(sn completion)
#
# To install completions permanently:
#   # For current user:
#   mkdir -p ~/.local/share/bash-completion/completions
#   sn completion > ~/.local/share/bash-completion/completions/sn
#
#   # Or system-wide (requires root):
#   sudo sn completion > /etc/bash_completion.d/sn

_sn() {
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

    local commands="new show edit append ls search links backlinks projects pin unpin rename autolink export import rm history diff restore deleted undelete completion version help"

    if [[ $cword -eq 1 ]]; then
        COMPREPLY=( $(compgen -W "$commands" -- "$cur") )
        return 0
    fi

    local cmd="${words[1]}"
    case "$cmd" in
        show|restore)
            if _sn_at_first_arg; then
                _sn_complete_titles "$cur" "$(sn ls 2>/dev/null)"
            fi
            ;;
        edit|append|links|backlinks|autolink|rm|history|diff|rename|pin|unpin)
            if [[ $cword -eq 2 ]]; then
                _sn_complete_titles "$cur" "$(sn ls 2>/dev/null)"
            fi
            ;;
        undelete)
            if [[ $cword -eq 2 ]]; then
                # Drop the date and time each line of deleted starts with.
                _sn_complete_titles "$cur" "$(sn deleted 2>/dev/null | sed 's/^[^ ]* [^ ]*  //' | sort -u)"
            fi
            ;;
        projects)
            if [[ $cword -eq 2 ]]; then
                _sn_complete_projects "$cur"
            fi
            ;;
        import|export)
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

# _sn_at_first_arg succeeds when the word being completed is the first
# argument of show or restore, the commands that take flags. Flags before it
# are skipped, with the value of -r; after --, a word that starts with - is an
# argument too.
_sn_at_first_arg() {
    local i args=0 dashdash=0
    for (( i=2; i<cword; i++ )); do
        if [[ $dashdash -eq 0 ]]; then
            case "${words[i]}" in
                --) dashdash=1; continue ;;
                # -r=3 arrives as -r, =, 3.
                =) (( i++ )); continue ;;
                -r|--r) [[ "${words[i+1]}" == "=" ]] || (( i++ )); continue ;;
                -*) continue ;;
            esac
        fi
        (( args++ ))
    done
    [[ $i -eq $cword && $args -eq 0 ]] || return 1
    [[ $dashdash -eq 1 || "$cur" != -* ]]
}

# _sn_complete_titles completes cur from titles, one per line.
_sn_complete_titles() {
    local cur="$1"
    local titles="$2"
    [[ -z "$titles" ]] && return 0

    local prefix="$cur"
    local quoted=0
    if [[ "$cur" == \"* || "$cur" == \'* ]]; then
        quoted=1
        prefix="${cur:1}"
    fi

    local matches=()
    while IFS= read -r line; do
        line="${line#📌 }" # sn ls marks pinned notes
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

_sn_complete_projects() {
    local cur="$1"
    local projects
    projects="$(sn projects 2>/dev/null)" || return 0
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

complete -F _sn sn
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
