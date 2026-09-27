// Command dockit is the DockIt server and its maintenance commands.
package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"runtime/debug"
	"strings"

	"github.com/pdutton/DockIt/internal/auth"
	"github.com/pdutton/DockIt/internal/model"
	"github.com/pdutton/DockIt/internal/store"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = ""

// oneTimePasswordLen is the length of the admin password printed by init.
const oneTimePasswordLen = 20

type env struct {
	stdin          io.Reader
	stdout, stderr io.Writer
	getenv         func(string) string
}

type command struct {
	name    string
	summary string
	run     func(e *env, args []string) error
}

var commands = []command{
	{"init", "Create a new dataset and its first admin; empty directory only", runInit},
	{"unlock", "Remove a stale lock file after confirming no instance is running", runUnlock},
	{"version", "Print the build version and supported dataset formats", runVersion},
}

func main() {
	e := &env{os.Stdin, os.Stdout, os.Stderr, os.Getenv}
	os.Exit(run(e, os.Args[1:]))
}

// errUsage means the command line was wrong; usage has already been printed.
var errUsage = errors.New("usage")

func run(e *env, args []string) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		usage(e.stderr)
		return 2
	}
	for _, c := range commands {
		if c.name == args[0] {
			err := c.run(e, args[1:])
			switch {
			case err == nil:
				return 0
			case errors.Is(err, errUsage), errors.Is(err, flag.ErrHelp):
				return 2
			default:
				fmt.Fprintf(e.stderr, "dockit %s: %v\n", c.name, err)
				return 1
			}
		}
	}
	fmt.Fprintf(e.stderr, "dockit: unknown command %q\n\n", args[0])
	usage(e.stderr)
	return 2
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "Usage: dockit <command> [flags] [dataset dir]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Commands:")
	for _, c := range commands {
		fmt.Fprintf(w, "  %-8s %s\n", c.name, c.summary)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "The dataset directory defaults to $DOCKIT_DATA.")
	fmt.Fprintln(w, "Run 'dockit <command> -h' for a command's flags.")
}

func newFlagSet(e *env, name, args string) *flag.FlagSet {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(e.stderr)
	flags.Usage = func() {
		fmt.Fprintf(e.stderr, "Usage: dockit %s [flags] %s\n", name, args)
		flags.PrintDefaults()
	}
	return flags
}

// dataDir returns the dataset directory from the single positional argument,
// falling back to $DOCKIT_DATA.
func dataDir(e *env, flags *flag.FlagSet) (string, error) {
	switch flags.NArg() {
	case 0:
		if d := e.getenv("DOCKIT_DATA"); d != "" {
			return d, nil
		}
		fmt.Fprintln(e.stderr, "no dataset directory given and DOCKIT_DATA is not set")
	case 1:
		return flags.Arg(0), nil
	default:
		fmt.Fprintln(e.stderr, "too many arguments")
	}
	flags.Usage()
	return "", errUsage
}

func runInit(e *env, args []string) error {
	flags := newFlagSet(e, "init", "<dir>")
	id := flags.String("admin", "", "user ID of the first admin (required)")
	name := flags.String("name", "", "display name of the first admin (required)")
	email := flags.String("email", "", "email address of the first admin (required)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	dir, err := dataDir(e, flags)
	if err != nil {
		return err
	}
	for _, check := range []error{
		model.CheckUserID(*id),
		model.CheckUserName(*name),
		model.CheckEmail(*email),
	} {
		if check != nil {
			var fe *model.FieldError
			if errors.As(check, &fe) {
				flagName := map[string]string{"id": "admin"}[fe.Field]
				if flagName == "" {
					flagName = fe.Field
				}
				return fmt.Errorf("-%s: %s", flagName, fe.Message)
			}
			return check
		}
	}

	now := model.Now()
	password := auth.GeneratePassword(oneTimePasswordLen)
	user := &model.User{
		ID:       *id,
		Version:  1,
		Name:     *name,
		Email:    *email,
		Role:     model.RoleAdmin,
		Active:   true,
		Created:  now,
		Modified: now,
	}
	secrets := &model.Auth{
		User:               *id,
		Password:           auth.HashPassword(password),
		MustChangePassword: true,
	}
	if err := store.Init(dir, user, secrets); err != nil {
		return err
	}

	fmt.Fprintf(e.stdout, "Created DockIt dataset in %s\n", dir)
	fmt.Fprintf(e.stdout, "Admin user: %s\n", *id)
	fmt.Fprintf(e.stdout, "One-time password: %s\n", password)
	fmt.Fprintln(e.stdout, "This password is shown only once and must be changed at first login.")
	return nil
}

func runUnlock(e *env, args []string) error {
	flags := newFlagSet(e, "unlock", "<dir>")
	yes := flags.Bool("yes", false, "do not ask for confirmation")
	if err := flags.Parse(args); err != nil {
		return err
	}
	dir, err := dataDir(e, flags)
	if err != nil {
		return err
	}
	if _, err := store.ReadMeta(dir); err != nil {
		return err
	}

	info, raw, err := store.ReadLock(dir)
	if errors.Is(err, fs.ErrNotExist) {
		fmt.Fprintln(e.stdout, "Dataset is not locked.")
		return nil
	}
	fmt.Fprintln(e.stdout, "The dataset is locked by:")
	printLock(e.stdout, info, raw)

	if !*yes {
		fmt.Fprint(e.stdout, "Remove the lock only if that instance is no longer running. Remove it? [y/N] ")
		line, _ := bufio.NewReader(e.stdin).ReadString('\n')
		if a := strings.ToLower(strings.TrimSpace(line)); a != "y" && a != "yes" {
			fmt.Fprintln(e.stdout, "Lock left in place.")
			return nil
		}
	}
	if err := store.Unlock(dir); err != nil {
		return err
	}
	fmt.Fprintln(e.stdout, "Lock removed.")
	return nil
}

func printLock(w io.Writer, info *store.LockInfo, raw []byte) {
	if info == nil {
		fmt.Fprintf(w, "  (unreadable lock file)\n%s\n", raw)
		return
	}
	fmt.Fprintf(w, "  host:     %s\n", info.Host)
	fmt.Fprintf(w, "  pid:      %d\n", info.PID)
	fmt.Fprintf(w, "  started:  %s\n", info.Started.Format("2006-01-02T15:04:05Z07:00"))
	fmt.Fprintf(w, "  instance: %s\n", info.Instance)
}

func runVersion(e *env, args []string) error {
	flags := newFlagSet(e, "version", "")
	if err := flags.Parse(args); err != nil {
		return err
	}
	fmt.Fprintf(e.stdout, "dockit %s\n", buildVersion())
	if model.FormatMin == model.FormatCurrent {
		fmt.Fprintf(e.stdout, "dataset format %d\n", model.FormatCurrent)
	} else {
		fmt.Fprintf(e.stdout, "dataset format %d (upgrades from %d)\n", model.FormatCurrent, model.FormatMin)
	}
	return nil
}

func buildVersion() string {
	if version != "" {
		return version
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return "dev"
}
