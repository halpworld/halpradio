package components

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/halpworld/halpradio/pkg/plugin"
	"github.com/halpworld/halpradio/pkg/theme"
	"github.com/halpworld/halpradio/pkg/util"
)

// ApprovalMinRuleLines is the fewest rule lines the permission prompt must
// be able to show; below that it refuses to take an approval.
const ApprovalMinRuleLines = 3

// ApprovalTooSmallText is shown instead of the rules when the terminal is
// too small to review them.
const ApprovalTooSmallText = "window too small to review permissions"

// PermissionApprovalInput is everything the permission prompt draws.
type PermissionApprovalInput struct {
	Info plugin.PluginInfo
	// Scroll is the first visible line of the rule list.
	Scroll int
	// Armed is set after "y": Enter approves, any other key cancels.
	Armed bool
	// Hint is a feedback line from Update ("scroll to the end first").
	Hint   string
	Width  int
	Height int
}

// PermissionApprovalLayout describes the scrolling rule region for a given
// plugin and terminal size. Update uses it to clamp scrolling and to know
// when the last rule has been on screen; View uses the same numbers.
type PermissionApprovalLayout struct {
	// Lines is the scroll region, one terminal line each.
	Lines []string
	// Visible is how many region lines fit on screen.
	Visible int
	// MaxScroll is the largest valid Scroll.
	MaxScroll int
	// TooSmall is set when the pinned parts plus ApprovalMinRuleLines do
	// not fit. No approval may be taken then.
	TooSmall bool
}

// EndVisible reports whether the last region line is on screen at scroll.
func (l PermissionApprovalLayout) EndVisible(scroll int) bool {
	return !l.TooSmall && scroll+l.Visible >= len(l.Lines)
}

// ClampScroll keeps scroll inside the region.
func (l PermissionApprovalLayout) ClampScroll(scroll int) int {
	if scroll > l.MaxScroll {
		scroll = l.MaxScroll
	}
	if scroll < 0 {
		scroll = 0
	}
	return scroll
}

// permissionDanger ranks a prompt line: wildcards first, then rules that
// reach this computer or the local network, then the rest.
func permissionDanger(l plugin.PermissionLine) int {
	switch {
	case l.Rule == "*" || strings.HasPrefix(l.Rule, "*."):
		return 0
	case l.Warning:
		return 1
	}
	return 2
}

// OrderPermissionLines returns the prompt lines most dangerous first
// (wildcards, then private/loopback ranges, then the rest), keeping the
// manifest order within each group. A manifest cannot hide a dangerous rule
// behind a long list of harmless ones.
func OrderPermissionLines(lines []plugin.PermissionLine) []plugin.PermissionLine {
	out := append([]plugin.PermissionLine(nil), lines...)
	sort.SliceStable(out, func(i, j int) bool { return permissionDanger(out[i]) < permissionDanger(out[j]) })
	return out
}

// approvalGeometry holds the fixed parts of the prompt.
type approvalGeometry struct {
	boxWidth  int
	textWidth int
	padY      int
	top       string
	topH      int
	bottomH   int
}

const approvalBottomLines = 4 // blank, hint, approve keys, deny/scroll keys

func approvalGeometryFor(p plugin.PluginInfo, width, height int, th theme.Theme) approvalGeometry {
	g := approvalGeometry{boxWidth: 66}
	if width < 70 {
		g.boxWidth = width - 4
	}
	g.textWidth = g.boxWidth - 6
	if g.textWidth < 10 {
		g.textWidth = 10
	}
	if height >= 40 {
		g.padY = 1
	}
	clean := func(s string, n int) string { return util.SanitizeDisplay(s, n) }
	muted := lipgloss.NewStyle().Foreground(th.Muted).Width(g.textWidth)
	sha := clean(p.WasmSHA256, 64)
	if len(sha) > 12 {
		sha = sha[:12] + "…"
	}
	if sha == "" {
		sha = "unknown"
	}
	top := []string{
		lipgloss.NewStyle().Bold(true).Foreground(th.Secondary).Render("🛡️  SECURITY PERMISSION APPROVAL"),
		lipgloss.NewStyle().Bold(true).Foreground(th.Primary).Width(g.textWidth).Render(fmt.Sprintf("Plugin: %s (v%s)", clean(p.Manifest.Name, 60), clean(p.Manifest.Version, 30))),
		muted.Render("ID: " + clean(p.Manifest.ID, 64) + " | Binary SHA-256: " + sha),
		muted.Render(truncate("Author: "+clean(p.Manifest.Author, 60), g.textWidth)),
		lipgloss.NewStyle().Bold(true).Foreground(th.Foreground).Render("Requested capabilities (most sensitive first):"),
	}
	g.top = lipgloss.JoinVertical(lipgloss.Left, top...)
	g.topH = lipgloss.Height(g.top)
	g.bottomH = approvalBottomLines
	return g
}

// approvalRegion renders the scrolling part: every rule in full (wrapped,
// never cut), then notes, then the sandbox reminder.
func approvalRegion(p plugin.PluginInfo, textWidth int, th theme.Theme) []string {
	clean := func(s string) string { return util.SanitizeDisplay(s, 0) }
	warn := lipgloss.NewStyle().Foreground(th.Favorite).Bold(true).Width(textWidth)
	plain := lipgloss.NewStyle().Foreground(th.Foreground).Width(textWidth)
	muted := lipgloss.NewStyle().Foreground(th.Muted).Width(textWidth)

	var blocks []string
	for _, line := range OrderPermissionLines(p.Manifest.Permissions.Summary()) {
		text := fmt.Sprintf("[%s] %s", clean(line.Category), clean(line.Text))
		if line.Warning || permissionDanger(line) == 0 {
			blocks = append(blocks, warn.Render("!! "+text))
		} else {
			blocks = append(blocks, plain.Render(" - "+text))
		}
	}
	if p.ApprovalNote != "" {
		blocks = append(blocks, "", warn.Render("Note: "+clean(p.ApprovalNote)))
	}
	if p.State.ApprovedPermissions != nil {
		if changes := plugin.PermissionChanges(p.State.ApprovedPermissions, p.Manifest.Permissions); len(changes) > 0 {
			blocks = append(blocks, "", lipgloss.NewStyle().Bold(true).Foreground(th.Favorite).Render("New since you last approved:"))
			for _, c := range changes {
				blocks = append(blocks, warn.Render(" + "+clean(c)))
			}
		}
	}
	blocks = append(blocks, "", muted.Italic(true).Render("Plugins run inside a WebAssembly sandbox and can only use the access listed above. Approval covers this exact binary; any update needs approval again."))

	var lines []string
	for _, b := range blocks {
		lines = append(lines, strings.Split(b, "\n")...)
	}
	return lines
}

// LayoutPermissionApproval computes the scroll region for a terminal of
// width x height.
func LayoutPermissionApproval(p plugin.PluginInfo, width, height int, th theme.Theme) PermissionApprovalLayout {
	g := approvalGeometryFor(p, width, height, th)
	region := approvalRegion(p, g.textWidth, th)
	// Border (2) + padding + pinned parts + the two "more" markers.
	avail := height - 2 - 2*g.padY - g.topH - g.bottomH - 2
	l := PermissionApprovalLayout{Lines: region}
	if avail < ApprovalMinRuleLines || g.textWidth < 20 {
		l.TooSmall = true
		return l
	}
	l.Visible = min(avail, len(region))
	l.MaxScroll = len(region) - l.Visible
	return l
}

// RenderPermissionApproval draws the permission prompt. The plugin identity
// is pinned at the top and the action keys at the bottom; the rules scroll
// between them. The output never has more lines than in.Height.
func RenderPermissionApproval(in PermissionApprovalInput, th theme.Theme) string {
	width, height := in.Width, in.Height
	g := approvalGeometryFor(in.Info, width, height, th)
	l := LayoutPermissionApproval(in.Info, width, height, th)

	keyStyle := lipgloss.NewStyle().Foreground(th.Playing).Bold(true)
	denyStyle := lipgloss.NewStyle().Foreground(th.Favorite)
	hintStyle := lipgloss.NewStyle().Foreground(th.Highlight).Bold(true)
	muted := lipgloss.NewStyle().Foreground(th.Muted)
	fit := func(s string) string { return truncate(s, g.textWidth) }

	if l.TooSmall {
		lines := []string{
			fit("🛡️  Plugin: " + util.SanitizeDisplay(in.Info.Manifest.Name, 60)),
			hintStyle.Render(fit(ApprovalTooSmallText)),
			denyStyle.Render(fit("[ n / Esc ] Deny")),
		}
		if height < len(lines) {
			lines = lines[:max(height, 0)]
		}
		return PlaceOverlay(strings.Join(lines, "\n"), width, height)
	}

	scroll := l.ClampScroll(in.Scroll)
	above := scroll
	below := len(l.Lines) - scroll - l.Visible
	markerAbove, markerBelow := "", ""
	if above > 0 {
		markerAbove = muted.Render(fmt.Sprintf("↑ %d more above", above))
	}
	if below > 0 {
		markerBelow = hintStyle.Render(fmt.Sprintf("↓ %d more below (j / PgDn)", below))
	}

	hint := in.Hint
	switch {
	case in.Armed:
		hint = "Press Enter to approve, any other key cancels"
	case hint == "" && below > 0:
		hint = "Scroll to the end to review every permission"
	}

	parts := []string{g.top, markerAbove}
	parts = append(parts, l.Lines[scroll:scroll+l.Visible]...)
	parts = append(parts,
		markerBelow,
		"",
		hintStyle.Render(fit(hint)),
		keyStyle.Render(fit("[ y ] then [ Enter ] Approve Permissions & Enable")),
		denyStyle.Render(fit("[ n / Esc ] Keep Disabled (Deny)   [ j / k / PgDn / PgUp ] Scroll")),
	)

	box := lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(th.Secondary).
		Padding(g.padY, 2).
		Width(g.boxWidth).
		Render(strings.Join(parts, "\n"))
	return PlaceOverlay(box, width, height)
}

// RenderPermissionApprovalModal draws the prompt at the top of its rule list.
func RenderPermissionApprovalModal(p plugin.PluginInfo, width int, height int, th theme.Theme) string {
	return RenderPermissionApproval(PermissionApprovalInput{Info: p, Width: width, Height: height}, th)
}
