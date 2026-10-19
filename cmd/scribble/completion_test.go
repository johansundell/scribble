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
			if !strings.Contains(out.String(), "complete -F _scribble scribble") {
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
	if !strings.Contains(buf.String(), "complete -F _scribble scribble") {
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
complete -p scribble >/dev/null 2>&1 || { echo "registration failed"; exit 1; }

# Mock scribble for completion callbacks
scribble() {
    if [[ "$1" == "ls" ]]; then
        echo "Groceries"
        echo "Shopping List"
        echo "Work Notes"
    elif [[ "$1" == "projects" ]]; then
        echo "home"
        echo "work"
    fi
}

# Helper to run _scribble with COMP_WORDS and COMP_CWORD
run_comp() {
    COMP_WORDS=("$@")
    COMP_CWORD=$(( ${#COMP_WORDS[@]} - 1 ))
    _scribble
}

# 1. Complete top-level subcommands
run_comp "scribble" "sh"
if [[ "${COMPREPLY[*]}" != "show" ]]; then
    echo "subcommand completion failed: expected 'show', got '${COMPREPLY[*]}'"
    exit 2
fi

# 2. Complete titles (unquoted, with space escaping)
run_comp "scribble" "show" "Shop"
if [[ "${COMPREPLY[*]}" != 'Shopping\ List' ]]; then
    echo "title completion failed: expected 'Shopping\\ List', got '${COMPREPLY[*]}'"
    exit 3
fi

# 3. Complete titles (case-insensitive)
run_comp "scribble" "show" "groc"
if [[ "${COMPREPLY[*]}" != 'Groceries' ]]; then
    echo "case-insensitive title completion failed: expected 'Groceries', got '${COMPREPLY[*]}'"
    exit 4
fi

# 4. Complete titles with quotes
run_comp "scribble" "edit" '"Shop'
if [[ "${COMPREPLY[*]}" != 'Shopping List' ]]; then
    echo "quoted title completion failed: expected 'Shopping List', got '${COMPREPLY[*]}'"
    exit 5
fi

# 5. Complete projects
run_comp "scribble" "projects" "ho"
if [[ "${COMPREPLY[*]}" != 'home' ]]; then
    echo "project completion failed: expected 'home', got '${COMPREPLY[*]}'"
    exit 6
fi

# 6. Complete projects with hash
run_comp "scribble" "projects" "#ho"
if [[ "${COMPREPLY[*]}" != '#home' ]]; then
    echo "project with hash completion failed: expected '#home', got '${COMPREPLY[*]}'"
    exit 7
fi

# 7. Complete completion shell argument
run_comp "scribble" "completion" "ba"
if [[ "${COMPREPLY[*]}" != 'bash' ]]; then
    echo "completion shell argument failed: expected 'bash', got '${COMPREPLY[*]}'"
    exit 8
fi

# 8. Complete help command argument
run_comp "scribble" "help" "ed"
if [[ "${COMPREPLY[*]}" != 'edit' ]]; then
    echo "help argument failed: expected 'edit', got '${COMPREPLY[*]}'"
    exit 9
fi
`

	cmd := exec.Command("bash", "-c", bashTest)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("bash completion behavior test failed: %v\nOutput: %s", err, string(out))
	}
}
