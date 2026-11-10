package app

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/halpworld/halpradio/pkg/plugin"
	"github.com/halpworld/halpradio/pkg/util"
)

// pluginPromptInput is where the plugin approval prompt reads the user's
// answer. A variable so tests can feed answers.
var pluginPromptInput io.Reader = os.Stdin

// errPluginApprovalDeclined is returned when the user does not approve a
// plugin's permissions (answered no, or no terminal and no --approve-sha256).
var errPluginApprovalDeclined = errors.New("plugin permissions not approved")

// errBareApproveFlag is returned for --approve without a hash: a blanket
// "yes" cannot say which binary it approves.
var errBareApproveFlag = errors.New("--approve needs the plugin's SHA-256: use --approve-sha256 <hex>")

// errApproveHashMismatch is returned when --approve-sha256 does not match
// the installed plugin.wasm.
var errApproveHashMismatch = errors.New("--approve-sha256 does not match the installed plugin binary")

// approveOptions is what the command line says about approval.
type approveOptions struct {
	// bare is set for --approve without a hash. It is refused.
	bare bool
	// sha256 is the hex digest from --approve-sha256. Approval happens only
	// when it equals the installed plugin.wasm's SHA-256.
	sha256 string
	// missingValue is set when --approve-sha256 had no value.
	missingValue bool
}

// splitApproveFlag removes --approve, --approve-sha256 <hex> and
// --approve-sha256=<hex> (one or two dashes) from args.
func splitApproveFlag(args []string) ([]string, approveOptions) {
	out := make([]string, 0, len(args))
	var opts approveOptions
	for i := 0; i < len(args); i++ {
		a := args[i]
		name, value, hasValue := strings.Cut(strings.TrimLeft(a, "-"), "=")
		if !strings.HasPrefix(a, "-") {
			out = append(out, a)
			continue
		}
		switch name {
		case "approve":
			opts.bare = true
		case "approve-sha256":
			if !hasValue {
				if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
					value = args[i+1]
					i++
				} else {
					opts.missingValue = true
				}
			}
			opts.sha256 = strings.ToLower(strings.TrimSpace(value))
		default:
			out = append(out, a)
		}
	}
	return out, opts
}

// pluginPromptWidth is the width rules are wrapped to in the CLI prompt.
const pluginPromptWidth = 76

// wrapRunes splits s into lines of at most width runes, never dropping a
// rune, so a long rule is shown in full.
func wrapRunes(s string, width int) []string {
	r := []rune(s)
	if width <= 0 || len(r) <= width {
		return []string{s}
	}
	var lines []string
	for len(r) > width {
		lines = append(lines, string(r[:width]))
		r = r[width:]
	}
	if len(r) > 0 {
		lines = append(lines, string(r))
	}
	return lines
}

// printPluginPermissions shows the permission prompt for one plugin: what it
// may do (every rule in full, warnings marked), why approval is needed, and
// what is new since the last approval.
func printPluginPermissions(out io.Writer, info plugin.PluginInfo) {
	m := info.Manifest
	fmt.Fprintf(out, "\nPlugin: %s v%s (id: %s)\n", safe(m.Name), safe(m.Version), safe(m.ID))
	if m.Author != "" {
		fmt.Fprintf(out, "Author: %s\n", safe(m.Author))
	}
	if info.WasmSHA256 != "" {
		fmt.Fprintf(out, "Binary SHA-256: %s\n", safe(info.WasmSHA256))
	}
	if info.ApprovalNote != "" {
		fmt.Fprintf(out, "Note: %s\n", safe(info.ApprovalNote))
	}
	fmt.Fprintln(out, "Permissions requested:")
	for _, line := range m.Permissions.Summary() {
		marker := "   - "
		if line.Warning {
			marker = "  !! "
		}
		// No length cap: a rule is never cut short, it wraps instead.
		text := "[" + util.SanitizeDisplay(line.Category, 0) + "] " + util.SanitizeDisplay(line.Text, 0)
		for i, part := range wrapRunes(text, pluginPromptWidth) {
			if i == 0 {
				fmt.Fprintf(out, "%s%s\n", marker, part)
			} else {
				fmt.Fprintf(out, "     %s\n", part)
			}
		}
	}
	if info.State.ApprovedPermissions != nil {
		changes := plugin.PermissionChanges(info.State.ApprovedPermissions, m.Permissions)
		if len(changes) > 0 {
			fmt.Fprintln(out, "New since you last approved:")
			for _, c := range changes {
				for i, part := range wrapRunes(util.SanitizeDisplay(c, 0), pluginPromptWidth) {
					if i == 0 {
						fmt.Fprintf(out, "   + %s\n", part)
					} else {
						fmt.Fprintf(out, "     %s\n", part)
					}
				}
			}
		}
	}
}

// printTwoStepHint shows how to approve without a terminal: review first,
// then approve with the hash that was shown.
func printTwoStepHint(out io.Writer, id, sha string) {
	if sha == "" {
		sha = "<sha256 shown by 'halpradio plugin approve " + safe(id) + "'>"
	}
	fmt.Fprintf(out, "To approve without a terminal, review the permissions above, then run\n  halpradio plugin approve %s --approve-sha256 %s\n", safe(id), safe(sha))
}

// readPromptAnswer flushes keys typed ahead of the prompt (when reading the
// real stdin) and reads one line.
func readPromptAnswer() string {
	if f, ok := pluginPromptInput.(*os.File); ok {
		flushTerminalInput(f)
	}
	answer, _ := bufio.NewReader(pluginPromptInput).ReadString('\n')
	return strings.ToLower(strings.TrimSpace(answer))
}

// confirmPluginApproval shows the plugin's permissions and approves exactly
// what was shown: on a terminal after the user types "yes", or with
// --approve-sha256 when it equals the installed binary's SHA-256. A bare
// --approve is refused. It returns errPluginApprovalDeclined when the user
// did not approve.
func confirmPluginApproval(out io.Writer, mgr *plugin.Manager, id string, opts approveOptions) error {
	info, ok := mgr.GetPlugin(id)
	if !ok {
		return fmt.Errorf("plugin %s is not installed", id)
	}
	printPluginPermissions(out, info)

	switch {
	case opts.sha256 != "" || opts.missingValue:
		if opts.missingValue || info.WasmSHA256 == "" || opts.sha256 != strings.ToLower(info.WasmSHA256) {
			fmt.Fprintln(out, "\nNot approved: --approve-sha256 does not match the installed plugin binary.")
			printTwoStepHint(out, id, info.WasmSHA256)
			return errApproveHashMismatch
		}
	case opts.bare:
		fmt.Fprintln(out, "\nNot approved: --approve alone is not accepted, because it cannot say which binary you reviewed.")
		printTwoStepHint(out, id, info.WasmSHA256)
		return errBareApproveFlag
	default:
		if !stdinIsTerminal() {
			fmt.Fprintln(out, "\nNot approved: stdin is not a terminal.")
			printTwoStepHint(out, id, info.WasmSHA256)
			return errPluginApprovalDeclined
		}
		fmt.Fprint(out, "\nType yes to approve these permissions (anything else declines): ")
		if readPromptAnswer() != "yes" {
			fmt.Fprintln(out, "Not approved. The plugin stays installed but will not run.")
			return errPluginApprovalDeclined
		}
	}

	if err := mgr.ApprovePermissionsFor(id, info); err != nil {
		if errors.Is(err, plugin.ErrApprovalStale) {
			fmt.Fprintln(out, "The plugin changed while you were reviewing it; nothing was approved. Run the command again.")
		} else {
			fmt.Fprintf(out, "Approval failed: %s\n", safe(err.Error()))
		}
		return err
	}
	fmt.Fprintf(out, "✓ Approved permissions for %s\n", safe(id))
	return nil
}

// RunPluginCLI handles plugin subcommands: list, install, approve, remove,
// enable, disable, update. It never runs plugin code except for a plugin the
// user has just approved and enabled.
func RunPluginCLI(args []string, out io.Writer) (bool, error) {
	if len(args) == 0 || IsHelpArg(args[0]) {
		PrintPluginHelp(out)
		return true, nil
	}
	args, approveFlag := splitApproveFlag(args)
	if len(args) == 0 {
		PrintPluginHelp(out)
		return true, nil
	}

	cfg, _ := util.LoadConfig()
	mgr := plugin.NewManager(cfg.PluginRegistryURL)
	mgr.SetPluginsDisabled(!cfg.PluginsEnabled)
	// Scan only: the CLI never starts installed plugins.
	_ = mgr.Scan()
	defer mgr.Close()

	disabledNote := func() {
		if w := cfg.LoadWarning(); w != "" {
			fmt.Fprintln(out, "Error: "+w)
			return
		}
		fmt.Fprintln(out, "Plugins are disabled (plugins_enabled: false in config.yaml). Set it to true to install, approve or enable plugins.")
	}
	// refuse is the error for install, approve, enable and update when
	// plugins are off: the unreadable-config error takes precedence.
	refuse := func() error {
		disabledNote()
		if err := errConfigUnreadable(cfg); err != nil {
			return fmt.Errorf("%w: %v", plugin.ErrPluginsDisabled, err)
		}
		return plugin.ErrPluginsDisabled
	}

	requireID := func(cmd string) (string, error) {
		if len(args) < 2 || IsHelpArg(args[1]) {
			fmt.Fprintf(out, "Error: plugin ID required. Usage: halpradio plugin %s <plugin-id>\n", cmd)
			return "", fmt.Errorf("plugin ID required")
		}
		return args[1], nil
	}

	cmd := args[0]
	switch cmd {
	case "list", "ls":
		installed := mgr.GetPlugins()
		if mgr.PluginsDisabled() {
			disabledNote()
		}
		fmt.Fprintln(out, "📦 INSTALLED PLUGINS:")
		if len(installed) == 0 {
			fmt.Fprintln(out, "  (No plugins installed yet. Run 'halpradio plugin install <id>')")
		} else {
			for _, p := range installed {
				status := "[Enabled]"
				if !p.State.PermissionsApproved {
					status = "[Perms Required]"
				} else if !p.State.Enabled {
					status = "[Disabled]"
				}
				perms := "Isolated"
				if len(p.Manifest.Permissions.Network) > 0 {
					perms = fmt.Sprintf("Net: %s", strings.Join(p.Manifest.Permissions.Network, ","))
				}
				fmt.Fprintf(out, "  • %-20s v%-6s %-16s %s (%s)\n", safe(p.Manifest.ID), safe(p.Manifest.Version), status, safe(p.Manifest.Name), safe(perms))
				if p.ApprovalNote != "" {
					fmt.Fprintf(out, "      note: %s\n", safe(p.ApprovalNote))
				}
				if p.LastError != "" {
					fmt.Fprintf(out, "      error: %s\n", safe(p.LastError))
				}
			}
		}

		fmt.Fprintln(out, "")
		fmt.Fprintln(out, "🌐 OFFICIAL REGISTRY:")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		regIndex, err := mgr.RegistryClient().FetchRegistry(ctx)
		if err != nil {
			fmt.Fprintf(out, "  (Registry fetch note: %s)\n", safe(err.Error()))
		} else {
			for _, rp := range regIndex.Plugins {
				isInst := false
				for _, ip := range installed {
					if ip.Manifest.ID == rp.ID {
						isInst = true
						break
					}
				}
				instTag := "[Available]"
				if isInst {
					instTag = "[Installed]"
				}
				fmt.Fprintf(out, "  • %-20s v%-6s %-12s %s by %s\n", safe(rp.ID), safe(rp.Version), instTag, safe(rp.Name), safe(rp.Author))
			}
		}
		return true, nil

	case "install", "add":
		targetID, err := requireID("install")
		if err != nil {
			return false, err
		}
		if mgr.PluginsDisabled() {
			return false, refuse()
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		regIndex, err := mgr.RegistryClient().FetchRegistry(ctx)
		if err != nil {
			fmt.Fprintf(out, "Failed fetching registry: %s\n", safe(err.Error()))
			return false, err
		}

		var targetPlugin *plugin.RegistryPlugin
		for i := range regIndex.Plugins {
			if regIndex.Plugins[i].ID == targetID {
				targetPlugin = &regIndex.Plugins[i]
				break
			}
		}
		if targetPlugin == nil {
			fmt.Fprintf(out, "Plugin %q not found in official registry.\n", targetID)
			return false, fmt.Errorf("plugin not found: %s", targetID)
		}

		fmt.Fprintf(out, "Installing %s (v%s)...\n", safe(targetPlugin.Name), safe(targetPlugin.Version))
		if err := mgr.InstallFromRegistry(ctx, *targetPlugin); err != nil {
			fmt.Fprintf(out, "Install error: %s\n", safe(err.Error()))
			return false, err
		}
		fmt.Fprintf(out, "✓ Installed %s.\n", safe(targetPlugin.Name))
		if err := confirmPluginApproval(out, mgr, targetPlugin.ID, approveFlag); err != nil {
			return false, err
		}
		if err := mgr.EnablePlugin(targetPlugin.ID); err != nil {
			fmt.Fprintf(out, "Enable error: %s\n", safe(err.Error()))
			return false, err
		}
		fmt.Fprintf(out, "✓ Enabled %s\n", safe(targetPlugin.Name))
		return true, nil

	case "approve":
		targetID, err := requireID("approve")
		if err != nil {
			return false, err
		}
		if mgr.PluginsDisabled() {
			return false, refuse()
		}
		if err := confirmPluginApproval(out, mgr, targetID, approveFlag); err != nil {
			return false, err
		}
		return true, nil

	case "enable":
		targetID, err := requireID("enable")
		if err != nil {
			return false, err
		}
		if mgr.PluginsDisabled() {
			return false, refuse()
		}
		info, ok := mgr.GetPlugin(targetID)
		if !ok {
			fmt.Fprintf(out, "Enable error: plugin %q is not installed\n", targetID)
			return false, fmt.Errorf("plugin %s not found", targetID)
		}
		if !info.State.PermissionsApproved {
			if err := confirmPluginApproval(out, mgr, targetID, approveFlag); err != nil {
				return false, err
			}
		}
		if err := mgr.EnablePlugin(targetID); err != nil {
			fmt.Fprintf(out, "Enable error: %s\n", safe(err.Error()))
			return false, err
		}
		fmt.Fprintf(out, "✓ Enabled plugin %s\n", safe(targetID))
		return true, nil

	case "disable":
		targetID, err := requireID("disable")
		if err != nil {
			return false, err
		}
		if err := mgr.DisablePlugin(targetID); err != nil {
			fmt.Fprintf(out, "Disable error: %s\n", safe(err.Error()))
			return false, err
		}
		fmt.Fprintf(out, "✓ Disabled plugin %s\n", safe(targetID))
		return true, nil

	case "remove", "rm", "uninstall":
		targetID, err := requireID("remove")
		if err != nil {
			return false, err
		}
		if err := mgr.UninstallPlugin(targetID); err != nil {
			fmt.Fprintf(out, "Uninstall error: %s\n", safe(err.Error()))
			return false, err
		}
		fmt.Fprintf(out, "✓ Removed plugin %s\n", safe(targetID))
		return true, nil

	case "update", "upgrade":
		if mgr.PluginsDisabled() {
			return false, refuse()
		}
		only := ""
		if len(args) > 1 && args[1] != "--all" && args[1] != "-all" {
			only = args[1]
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		regIndex, err := mgr.RegistryClient().FetchRegistry(ctx)
		if err != nil {
			fmt.Fprintf(out, "Failed fetching registry: %s\n", safe(err.Error()))
			return false, err
		}
		var failed, needApproval []string
		for _, inst := range mgr.GetPlugins() {
			if only != "" && inst.Manifest.ID != only {
				continue
			}
			for _, reg := range regIndex.Plugins {
				if reg.ID != inst.Manifest.ID {
					continue
				}
				fmt.Fprintf(out, "Updating %s to v%s...\n", safe(reg.Name), safe(reg.Version))
				if err := mgr.InstallFromRegistry(ctx, reg); err != nil {
					fmt.Fprintf(out, "  Update error for %s: %s\n", safe(reg.ID), safe(err.Error()))
					failed = append(failed, reg.ID)
					continue
				}
				if after, ok := mgr.GetPlugin(reg.ID); ok && !after.State.PermissionsApproved {
					needApproval = append(needApproval, reg.ID)
				}
			}
		}
		for _, id := range needApproval {
			fmt.Fprintf(out, "! %s needs approval again before it runs: halpradio plugin approve %s\n", safe(id), safe(id))
		}
		if len(failed) > 0 {
			fmt.Fprintf(out, "✗ %d plugin update(s) failed: %s\n", len(failed), safe(strings.Join(failed, ", ")))
			return false, fmt.Errorf("plugin update failed for %s", strings.Join(failed, ", "))
		}
		fmt.Fprintln(out, "✓ Plugins up to date!")
		return true, nil

	default:
		sugg := SuggestCommand(cmd, []string{"list", "install", "approve", "enable", "disable", "remove", "update"})
		if sugg != "" {
			fmt.Fprintf(out, "Unknown plugin command %q. Did you mean %q? Run 'halpradio plugin --help'\n", cmd, sugg)
		} else {
			fmt.Fprintf(out, "Unknown plugin command %q. Run 'halpradio plugin --help'\n", cmd)
		}
		return false, fmt.Errorf("unknown plugin command: %s", cmd)
	}
}
