package app

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/halpworld/halpradio/pkg/util"
)

// cliStderr receives warnings that must not mix with command output.
// Tests replace it to capture them.
var cliStderr io.Writer = os.Stderr

// isInfoOnlyArgs reports whether args only ask for help or the version, so
// no config warning is printed for them.
func isInfoOnlyArgs(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "help", "version", "-version", "--version", "-v":
		return true
	}
	return IsHelpArg(args[0])
}

// warnUnreadableConfig prints the fail-closed warning to stderr when
// config.yaml exists but could not be read.
func warnUnreadableConfig() {
	cfg, _ := util.LoadConfig()
	if w := cfg.LoadWarning(); w != "" {
		fmt.Fprintln(cliStderr, "Warning: "+w)
	}
}

// errConfigUnreadable returns the error a command that would save the
// config must fail with, or nil when the config was read normally.
func errConfigUnreadable(cfg util.Config) error {
	if cfg.LoadError() == nil {
		return nil
	}
	return errors.New(cfg.LoadWarning())
}

// saveCLIConfig saves cfg, or reports why it cannot be saved. A config that
// could not be read is never overwritten.
func saveCLIConfig(cfg util.Config, out io.Writer) error {
	if err := errConfigUnreadable(cfg); err != nil {
		fmt.Fprintf(out, "Error: %s\n", err.Error())
		return err
	}
	if err := util.SaveConfig(cfg); err != nil {
		fmt.Fprintf(out, "Error: could not save config: %s\n", safe(err.Error()))
		return err
	}
	return nil
}
