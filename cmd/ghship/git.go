package main

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const gitHelp = `ghship git ROLE -- GIT_ARGUMENTS...
Verifies the current project's author/reviewer/dispatcher identity, then invokes
Git with ephemeral gh credentials. No persistent helper/global account changes,
no exported token, no shell interpolation of user arguments. Native exit preserved.
Run inside the target Git working tree; an existing binding must match origin.
Explicit overrides use the origin host, or github.com if unbound without origin.
Ambient Git override variables are removed; Git global options must keep the
same worktree and Git directory. Native commands can still address other resources;
the caller checks the intended operation.
Use git directly for local operations; use this for authenticated push/fetch.
Example: ghship git author -- push -u origin HEAD
`

func roleGit(args []string, in io.Reader, out io.Writer) error {
	if len(args) < 3 || args[1] != "--" {
		return usage("use ghship git ROLE -- GIT_ARGUMENTS...")
	}
	if e := validateRoleGitTarget(args[2:], cleanRoleEnv()); e != nil {
		return e
	}
	env, e := roleEnvironment(args[0])
	if e != nil {
		return e
	}
	host := "github.com"
	for _, v := range env {
		if strings.HasPrefix(v, "GH_HOST=") {
			host = strings.TrimPrefix(v, "GH_HOST=")
		}
	}
	a := append([]string{"-c", "credential.helper=", "-c", "credential.helper=!gh auth git-credential", "-c", "url.https://" + host + "/.insteadOf=git@" + host + ":"}, args[2:]...)
	c := exec.CommandContext(operationContext, "git", a...)
	c.Env = env
	c.Stdin = in
	c.Stdout = out
	c.Stderr = os.Stderr
	if e = c.Run(); e != nil {
		var exit *exec.ExitError
		if errors.As(e, &exit) {
			return cliError{exit.ExitCode(), errors.New("git command failed")}
		}
		return e
	}
	return nil
}

// Check Git's global options with rev-parse before using the role identity.
// Native subcommand arguments are not rewritten or interpreted here.
func validateRoleGitTarget(args, env []string) error {
	prefix := []string{}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "-") {
			break
		}
		switch arg {
		case "--version", "-v", "--help", "-h", "--html-path", "--man-path", "--info-path", "--exec-path":
			return nil // Git exits after printing this information.
		case "--":
			return usage("invalid Git global options")
		}
		prefix = append(prefix, arg)
		switch arg {
		case "-C", "-c", "--git-dir", "--work-tree", "--namespace", "--config-env", "--super-prefix":
			if i+1 == len(args) {
				return usage(arg + " requires a value")
			}
			i++
			prefix = append(prefix, args[i])
		}
	}
	if len(prefix) == 0 {
		return nil
	}
	root, err := currentProjectRoot(env)
	if err != nil {
		return err
	}
	originalDir, err := output(env, "git", "rev-parse", "--absolute-git-dir")
	if err != nil {
		return err
	}
	originalDir, err = filepath.EvalSymlinks(originalDir)
	if err != nil {
		return err
	}
	probe := append(append([]string{}, prefix...), "rev-parse", "--show-toplevel")
	targetRoot, err := output(env, "git", probe...)
	if err != nil {
		return errors.New("Git options do not select the current working tree")
	}
	targetRoot, err = filepath.EvalSymlinks(targetRoot)
	if err != nil || !sameRoleDirectory(targetRoot, root) {
		return errors.New("Git options select a different working tree")
	}
	probe = append(append([]string{}, prefix...), "rev-parse", "--absolute-git-dir")
	targetDir, err := output(env, "git", probe...)
	if err != nil {
		return errors.New("Git options do not select the current repository")
	}
	targetDir, err = filepath.EvalSymlinks(targetDir)
	if err != nil || !sameRoleDirectory(targetDir, originalDir) {
		return errors.New("Git options select a different repository")
	}
	return nil
}

func sameRoleDirectory(a, b string) bool {
	left, leftErr := os.Stat(a)
	right, rightErr := os.Stat(b)
	return leftErr == nil && rightErr == nil && left.IsDir() && right.IsDir() && os.SameFile(left, right)
}
