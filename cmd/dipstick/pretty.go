package main

import (
	"fmt"
	"io"
	"math"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"golang.org/x/term"

	"github.com/mattwalters/dipstick"
)

// RenderOptions configures the pretty TTY renderer.
type RenderOptions struct {
	Width         int
	Color         *bool
	Unicode       *bool
	ReferenceTime time.Time
}

type fdValuer interface {
	Fd() uintptr
}

func isTerminal(w io.Writer) bool {
	if f, ok := w.(fdValuer); ok {
		return term.IsTerminal(int(f.Fd()))
	}
	return false
}

func getTerminalWidth(w io.Writer) int {
	if f, ok := w.(fdValuer); ok {
		width, _, err := term.GetSize(int(f.Fd()))
		if err == nil && width > 0 {
			return width
		}
	}
	return 80
}

func detectRenderOptions(w io.Writer) RenderOptions {
	opts := RenderOptions{
		Width: getTerminalWidth(w),
	}

	noColor := os.Getenv("NO_COLOR") != ""
	clicolorForce := os.Getenv("CLICOLOR_FORCE")
	clicolor := os.Getenv("CLICOLOR")
	termEnv := strings.ToLower(os.Getenv("TERM"))
	ciEnv := os.Getenv("CI") != ""

	colorEnabled := true
	if noColor {
		colorEnabled = false
	} else if clicolorForce != "" && clicolorForce != "0" {
		colorEnabled = true
	} else if clicolor == "0" || termEnv == "dumb" {
		colorEnabled = false
	} else if ciEnv && clicolorForce == "" {
		colorEnabled = false
	} else if !isTerminal(w) {
		colorEnabled = false
	}
	opts.Color = &colorEnabled

	unicodeEnabled := true
	if termEnv == "dumb" {
		unicodeEnabled = false
	} else {
		lang := strings.ToUpper(os.Getenv("LANG") + " " + os.Getenv("LC_ALL") + " " + os.Getenv("LC_CTYPE"))
		if lang != " " && !strings.Contains(lang, "UTF-8") && !strings.Contains(lang, "UTF8") {
			if strings.Contains(lang, " C ") || strings.Contains(lang, " POSIX ") || lang == "C" || lang == "POSIX" {
				unicodeEnabled = false
			}
		}
	}
	opts.Unicode = &unicodeEnabled

	return opts
}

type row struct {
	provider string
	label    string
	value    string
}

// RenderPretty renders a dipstick.Report to w with styling and layout.
func RenderPretty(w io.Writer, rep *dipstick.Report, opts RenderOptions) error {
	if rep == nil {
		return nil
	}

	width := opts.Width
	if width <= 0 {
		width = 80
	}

	colorEnabled := true
	if opts.Color != nil {
		colorEnabled = *opts.Color
	}

	unicodeEnabled := true
	if opts.Unicode != nil {
		unicodeEnabled = *opts.Unicode
	}

	refTime := opts.ReferenceTime
	if refTime.IsZero() {
		if !rep.GeneratedAt.IsZero() {
			refTime = rep.GeneratedAt
		} else {
			refTime = time.Now()
		}
	}

	renderer := lipgloss.NewRenderer(w)
	if !colorEnabled {
		renderer.SetColorProfile(termenv.Ascii)
	} else {
		renderer.SetColorProfile(termenv.ANSI256)
	}

	// Styles
	providerStyle := renderer.NewStyle().Bold(true).Foreground(lipgloss.Color("255"))
	labelStyle := renderer.NewStyle().Foreground(lipgloss.Color("245"))
	normalMeterStyle := renderer.NewStyle().Foreground(lipgloss.Color("39")) // Accessible cyan/blue
	warnMeterStyle := renderer.NewStyle().Foreground(lipgloss.Color("214"))  // Amber / yellow
	critMeterStyle := renderer.NewStyle().Foreground(lipgloss.Color("197"))  // Magenta / Coral
	unfilledMeterStyle := renderer.NewStyle().Foreground(lipgloss.Color("238"))
	bracketStyle := renderer.NewStyle().Foreground(lipgloss.Color("240"))
	percentStyle := renderer.NewStyle().Foreground(lipgloss.Color("252"))
	resetStyle := renderer.NewStyle().Foreground(lipgloss.Color("244"))
	detailStyle := renderer.NewStyle().Foreground(lipgloss.Color("244"))
	dashStyle := renderer.NewStyle().Foreground(lipgloss.Color("240"))
	emptyNoticeStyle := renderer.NewStyle().Foreground(lipgloss.Color("245"))

	if len(rep.Providers) == 0 && len(rep.Errors) == 0 {
		_, err := fmt.Fprintln(w, emptyNoticeStyle.Render(" No providers reported usage."))
		return err
	}

	isNarrow := width < 60
	meterSegments := 10
	if isNarrow {
		meterSegments = 5
	}

	var rows []row

	// Process provider reports
	for _, p := range rep.Providers {
		pName := string(p.Provider)
		firstRow := true

		// Identity
		if p.Identity != nil {
			var parts []string
			if p.Identity.Plan != "" {
				parts = append(parts, p.Identity.Plan)
			}
			if p.Identity.Organization != "" {
				parts = append(parts, p.Identity.Organization)
			}
			if p.Identity.Email != "" {
				parts = append(parts, p.Identity.Email)
			} else if p.Identity.AccountID != "" {
				parts = append(parts, p.Identity.AccountID)
			}

			if len(parts) > 0 {
				sep := " · "
				if !unicodeEnabled {
					sep = " * "
				}
				joined := strings.Join(parts, sep)
				idLabel := "plan"
				if p.Identity.Plan == "" {
					idLabel = "account"
				}

				name := ""
				if firstRow {
					name = pName
					firstRow = false
				}
				rows = append(rows, row{
					provider: name,
					label:    idLabel,
					value:    detailStyle.Render(joined),
				})
			}
		}

		// Windows
		for _, win := range p.Windows {
			name := ""
			if firstRow {
				name = pName
				firstRow = false
			}

			if win.UsedPercent != nil {
				pct := *win.UsedPercent
				if pct < 0 {
					pct = 0
				}
				if pct > 100 {
					pct = 100
				}

				// Meter Bar
				k := int(math.Round(pct * float64(meterSegments) / 100.0))
				if k < 0 {
					k = 0
				}
				if k > meterSegments {
					k = meterSegments
				}

				activeStyle := normalMeterStyle
				if pct >= 90.0 {
					activeStyle = critMeterStyle
				} else if pct >= 70.0 {
					activeStyle = warnMeterStyle
				}

				var barStr string
				if unicodeEnabled {
					filled := strings.Repeat("▓", k)
					unfilled := strings.Repeat("░", meterSegments-k)
					var fPart, uPart string
					if k > 0 {
						fPart = activeStyle.Render(filled)
					}
					if meterSegments-k > 0 {
						uPart = unfilledMeterStyle.Render(unfilled)
					}
					barStr = fPart + uPart
				} else {
					filled := strings.Repeat("#", k)
					unfilled := strings.Repeat("-", meterSegments-k)
					var fPart, uPart string
					if k > 0 {
						fPart = activeStyle.Render(filled)
					}
					if meterSegments-k > 0 {
						uPart = unfilledMeterStyle.Render(unfilled)
					}
					barStr = bracketStyle.Render("[") + fPart + uPart + bracketStyle.Render("]")
				}

				pctStr := percentStyle.Render(fmt.Sprintf("%3d%%", int(math.Round(pct))))
				resetStr := formatReset(win.ResetsAt, refTime)
				if resetStr != "" {
					resetStr = resetStyle.Render(resetStr)
				}

				var val string
				if isNarrow {
					if resetStr != "" {
						val = fmt.Sprintf("%s %s  %s", barStr, pctStr, resetStr)
					} else {
						val = fmt.Sprintf("%s %s", barStr, pctStr)
					}
				} else {
					if resetStr != "" {
						val = fmt.Sprintf("%s  %s   %s", barStr, pctStr, resetStr)
					} else {
						val = fmt.Sprintf("%s  %s", barStr, pctStr)
					}
				}

				rows = append(rows, row{
					provider: name,
					label:    win.Label,
					value:    val,
				})
			} else {
				dash := "—"
				if !unicodeEnabled {
					dash = "-"
				}
				val := fmt.Sprintf("%s %s", dashStyle.Render(dash), detailStyle.Render("usage unavailable"))
				rows = append(rows, row{
					provider: name,
					label:    win.Label,
					value:    val,
				})
			}
		}

		// Tokens
		if p.Tokens != nil && (p.Tokens.TotalTokens != nil || p.Tokens.InputTokens != nil) {
			name := ""
			if firstRow {
				name = pName
				firstRow = false
			}
			tokStr := formatTokens(p.Tokens)
			if tokStr != "" {
				rows = append(rows, row{
					provider: name,
					label:    "tokens",
					value:    detailStyle.Render(tokStr),
				})
			}
		}

		// If a provider had no identity, windows, or tokens
		if firstRow {
			dash := "—"
			if !unicodeEnabled {
				dash = "-"
			}
			rows = append(rows, row{
				provider: pName,
				label:    dash,
				value:    detailStyle.Render("no usage data reported"),
			})
		}
	}

	// Process provider errors
	for _, pErr := range rep.Errors {
		dash := "—"
		if !unicodeEnabled {
			dash = "-"
		}
		msg := pErr.Detail
		if pErr.Reason != "" {
			if msg != "" {
				msg = fmt.Sprintf("%s (%s)", msg, pErr.Reason)
			} else {
				msg = fmt.Sprintf("(%s)", pErr.Reason)
			}
		}
		rows = append(rows, row{
			provider: string(pErr.Provider),
			label:    dash,
			value:    detailStyle.Render(msg),
		})
	}

	// Calculate column widths
	providerColWidth := 8
	labelColWidth := 8
	for _, r := range rows {
		if r.provider != "" {
			pw := lipgloss.Width(r.provider) + 2
			if pw > providerColWidth {
				providerColWidth = pw
			}
		}
		if r.label != "" {
			lw := lipgloss.Width(r.label) + 2
			if lw > labelColWidth {
				labelColWidth = lw
			}
		}
	}

	for _, r := range rows {
		var pPart, lPart string
		if r.provider != "" {
			pStyled := providerStyle.Render(r.provider)
			pad := providerColWidth - lipgloss.Width(pStyled)
			if pad < 0 {
				pad = 0
			}
			pPart = pStyled + strings.Repeat(" ", pad)
		} else {
			pPart = strings.Repeat(" ", providerColWidth)
		}

		if r.label != "" {
			var lStyled string
			if r.label == "—" || r.label == "-" {
				lStyled = dashStyle.Render(r.label)
			} else {
				lStyled = labelStyle.Render(r.label)
			}
			pad := labelColWidth - lipgloss.Width(lStyled)
			if pad < 0 {
				pad = 0
			}
			lPart = lStyled + strings.Repeat(" ", pad)
		} else {
			lPart = strings.Repeat(" ", labelColWidth)
		}

		line := fmt.Sprintf(" %s%s%s", pPart, lPart, r.value)
		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
	}

	return nil
}

func formatReset(resetsAt *time.Time, now time.Time) string {
	if resetsAt == nil || resetsAt.IsZero() {
		return ""
	}
	target := *resetsAt
	if target.Before(now) {
		return "resets now"
	}

	d := target.Sub(now)
	if d < 24*time.Hour {
		h := int(d.Hours())
		m := int(d.Minutes()) % 60
		if h > 0 && m > 0 {
			return fmt.Sprintf("resets in %dh %dm", h, m)
		}
		if h > 0 {
			return fmt.Sprintf("resets in %dh", h)
		}
		if m > 0 {
			return fmt.Sprintf("resets in %dm", m)
		}
		s := int(d.Seconds())
		return fmt.Sprintf("resets in %ds", s)
	}

	loc := now.Location()
	return fmt.Sprintf("resets %s", target.In(loc).Format("Mon 15:04"))
}

func formatTokens(t *dipstick.TokenUsage) string {
	if t == nil {
		return ""
	}
	var totalStr string
	if t.TotalTokens != nil {
		totalStr = formatCount(*t.TotalTokens)
	}
	if t.InputTokens != nil && t.OutputTokens != nil {
		inStr := formatCount(*t.InputTokens)
		outStr := formatCount(*t.OutputTokens)
		if totalStr != "" {
			return fmt.Sprintf("%s total (%s in / %s out)", totalStr, inStr, outStr)
		}
		return fmt.Sprintf("%s in / %s out", inStr, outStr)
	}
	if totalStr != "" {
		return fmt.Sprintf("%s total", totalStr)
	}
	return ""
}

func formatCount(n int64) string {
	if n >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000.0)
	}
	if n >= 1_000 {
		return fmt.Sprintf("%dk", n/1_000)
	}
	return fmt.Sprintf("%d", n)
}
