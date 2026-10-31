package main

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestCmdCompletion(t *testing.T) {
	cases := map[string]struct {
		args    []string
		wantErr string
	}{
		"default bash":      {nil, ""},
		"explicit bash":     {[]string{"bash"}, ""},
		"unsupported shell": {[]string{"zsh"}, `unsupported shell "zsh" (supported: bash)`},
		"unsupported fish":  {[]string{"fish"}, `unsupported shell "fish" (supported: bash)`},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var out bytes.Buffer
			err := cmdCompletion(&out, tc.args)
			if tc.wantErr != "" {
				if err == nil || err.Error() != tc.wantErr {
					t.Fatalf("got error %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.Contains(out.String(), "complete -F _sn sn") {
				t.Errorf("expected bash completion script, got %q", out.String())
			}
		})
	}
}

func TestRunCompletionWithoutToken(t *testing.T) {
	// completion must work without SCRIBBLE_TOKEN set (e.g. when sourced in .bashrc).
	orig := os.Getenv("SCRIBBLE_TOKEN")
	defer os.Setenv("SCRIBBLE_TOKEN", orig)
	os.Unsetenv("SCRIBBLE_TOKEN")

	var buf bytes.Buffer
	if err := runWith(&buf, []string{"completion"}); err != nil {
		t.Fatalf("run(completion) failed without token: %v", err)
	}
	if !strings.Contains(buf.String(), "complete -F _sn sn") {
		t.Errorf("expected bash completion script, got %q", buf.String())
	}

	buf.Reset()
	if err := runWith(&buf, []string{"completion", "bash"}); err != nil {
		t.Fatalf("run(completion, bash) failed without token: %v", err)
	}
	if err := runWith(&buf, []string{"completion", "unknown"}); err == nil {
		t.Fatal("run(completion, unknown) should have failed")
	}
}

func TestBashCompletionSyntaxAndBehavior(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not found in PATH")
	}

	var scriptBuf bytes.Buffer
	if err := cmdCompletion(&scriptBuf, nil); err != nil {
		t.Fatal(err)
	}
	script := scriptBuf.String()

	// Verify syntax using bash -n
	syntaxCheck := exec.Command("bash", "-n")
	syntaxCheck.Stdin = strings.NewReader(script)
	if out, err := syntaxCheck.CombinedOutput(); err != nil {
		t.Fatalf("bash completion syntax error: %v\nOutput: %s", err, string(out))
	}

	// Test shell execution and completion behaviors inside bash
	bashTest := `
` + script + `

# Verify registration
complete -p sn >/dev/null 2>&1 || { echo "registration failed"; exit 1; }

# Mock sn for completion callbacks
sn() {
    if [[ "$1" == "ls" ]]; then
        echo "Groceries"
        echo "Shopping List"
        echo "Work Notes"
        echo "-todo"
    elif [[ "$1" == "deleted" ]]; then
        echo "2026-10-20 12:00  Old Plans"
        echo "2026-10-19 09:30  Old Plans"
        echo "2026-10-18 08:15  Recipes"
    elif [[ "$1" == "projects" ]]; then
        echo "home"
        echo "work"
    fi
}

# Helper to run _sn with COMP_WORDS and COMP_CWORD
run_comp() {
    COMP_WORDS=("$@")
    COMP_CWORD=$(( ${#COMP_WORDS[@]} - 1 ))
    _sn
}

# 1. Complete top-level subcommands
run_comp "sn" "sh"
if [[ "${COMPREPLY[*]}" != "show" ]]; then
    echo "subcommand completion failed: expected 'show', got '${COMPREPLY[*]}'"
    exit 2
fi

# 2. Complete titles (unquoted, with space escaping)
run_comp "sn" "show" "Shop"
if [[ "${COMPREPLY[*]}" != 'Shopping\ List' ]]; then
    echo "title completion failed: expected 'Shopping\\ List', got '${COMPREPLY[*]}'"
    exit 3
fi

# 3. Complete titles (case-insensitive)
run_comp "sn" "show" "groc"
if [[ "${COMPREPLY[*]}" != 'Groceries' ]]; then
    echo "case-insensitive title completion failed: expected 'Groceries', got '${COMPREPLY[*]}'"
    exit 4
fi

# 4. Complete titles with quotes
run_comp "sn" "edit" '"Shop'
if [[ "${COMPREPLY[*]}" != 'Shopping List' ]]; then
    echo "quoted title completion failed: expected 'Shopping List', got '${COMPREPLY[*]}'"
    exit 5
fi

# 5. Complete projects
run_comp "sn" "projects" "ho"
if [[ "${COMPREPLY[*]}" != 'home' ]]; then
    echo "project completion failed: expected 'home', got '${COMPREPLY[*]}'"
    exit 6
fi

# 6. Complete projects with hash
run_comp "sn" "projects" "#ho"
if [[ "${COMPREPLY[*]}" != '#home' ]]; then
    echo "project with hash completion failed: expected '#home', got '${COMPREPLY[*]}'"
    exit 7
fi

# 7. Complete completion shell argument
run_comp "sn" "completion" "ba"
if [[ "${COMPREPLY[*]}" != 'bash' ]]; then
    echo "completion shell argument failed: expected 'bash', got '${COMPREPLY[*]}'"
    exit 8
fi

# 8. Complete help command argument
run_comp "sn" "help" "ed"
if [[ "${COMPREPLY[*]}" != 'edit' ]]; then
    echo "help argument failed: expected 'edit', got '${COMPREPLY[*]}'"
    exit 9
fi

# 9. Flags before the title are skipped
run_comp "sn" "restore" "-y" "Shop"
if [[ "${COMPREPLY[*]}" != 'Shopping\ List' ]]; then
    echo "title after -y failed: expected 'Shopping\\ List', got '${COMPREPLY[*]}'"
    exit 10
fi
run_comp "sn" "show" "-r" "3" "groc"
if [[ "${COMPREPLY[*]}" != 'Groceries' ]]; then
    echo "title after -r 3 failed: expected 'Groceries', got '${COMPREPLY[*]}'"
    exit 11
fi

# 10. No titles for the value of -r, a flag being typed, or a later argument
for words in "show -r G" "show -" "restore Groceries G"; do
    run_comp sn $words
    if [[ ${#COMPREPLY[@]} -ne 0 ]]; then
        echo "expected no completion for '$words', got '${COMPREPLY[*]}'"
        exit 12
    fi
done

# 11. After --, a word starting with - is a title
run_comp "sn" "show" "--" "-t"
if [[ "${COMPREPLY[*]}" != '-todo' ]]; then
    echo "title after -- failed: expected '-todo', got '${COMPREPLY[*]}'"
    exit 13
fi

# 12. Commands without flags take a title that starts with -
run_comp "sn" "edit" "-t"
if [[ "${COMPREPLY[*]}" != '-todo' ]]; then
    echo "edit title starting with - failed: expected '-todo', got '${COMPREPLY[*]}'"
    exit 15
fi

# 13. -r=3 arrives as -r, =, 3
run_comp "sn" "show" "-r" "=" "3" "groc"
if [[ "${COMPREPLY[*]}" != 'Groceries' ]]; then
    echo "title after -r=3 failed: expected 'Groceries', got '${COMPREPLY[*]}'"
    exit 16
fi

# 14. undelete completes deleted titles, once each
run_comp "sn" "undelete" "old"
if [[ "${COMPREPLY[*]}" != 'Old\ Plans' ]]; then
    echo "undelete completion failed: expected 'Old\\ Plans', got '${COMPREPLY[*]}'"
    exit 14
fi
`

	cmd := exec.Command("bash", "-c", bashTest)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("bash completion behavior test failed: %v\nOutput: %s", err, string(out))
	}
}
