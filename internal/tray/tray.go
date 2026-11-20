// Package tray is the notification-area icon: green/grey for on/off, live speeds on hover, and
// a menu to pause, change the IPv6 mode, start at sign-in or quit.
package tray

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode/utf16"

	"fyne.io/systray"

	"net-merge/internal/engine"
	"net-merge/internal/icon"
	"net-merge/internal/logx"
	"net-merge/internal/platform"
	"net-merge/internal/state"
	"net-merge/internal/tun"
	"net-merge/internal/update"
)

const trayRows = 4 // adapters shown in the menu; extra rows stay hidden

// Run shows the tray icon and blocks until Quit, and closes ready once the icon exists. With
// startOn it merges as soon as it can (at once, or, with autoPause, once two connections work).
func Run(t *tun.Tun, sched *engine.Scheduler, bal *engine.Balancer, stateDir, version string, startOn, autoPause bool, mode string, ready chan struct{}) {
	ui := &trayUI{tun: t, sched: sched, bal: bal, stateDir: stateDir, version: version, startOn: startOn,
		iconOn: icon.Tray(true), iconOff: icon.Tray(false), ready: ready}
	ui.ap.enabled = autoPause
	ui.mode = mode
	ui.languagePath = filepath.Join(stateDir, "ui-language")
	if pref, err := os.ReadFile(ui.languagePath); err == nil {
		ui.persian = strings.TrimSpace(string(pref)) == "fa"
	} else {
		ui.persian = runtime.GOOS == "windows"
	}
	exit := func() { t.Close(); state.Clear(stateDir) }
	systray.Run(ui.onReady, exit)
	exit() // macOS's Run returns on Quit without calling onExit; both are idempotent
}

type trayUI struct {
	tun          *tun.Tun
	bal          *engine.Balancer
	sched        *engine.Scheduler
	stateDir     string
	version      string
	startOn      bool
	mode         string
	persian      bool
	languagePath string
	iconOn       []byte
	iconOff      []byte
	ready        chan struct{} // closed once the tray icon exists

	ap autoPauser

	limitClicks chan limitClick
	rowNames    [trayRows]string
	limitUnlim  [trayRows]*systray.MenuItem
	limitPreset [trayRows][]*systray.MenuItem

	header, toggleItem, v6Item, autoItem, signInItem *systray.MenuItem
	versionItem                                      *systray.MenuItem
	rows                                             []*systray.MenuItem
	linkDetails                                      [trayRows]*systray.MenuItem
	lastDetails                                      [trayRows]string
	routeLine                                        *systray.MenuItem
	totalLine                                        *systray.MenuItem
	lastRouteText                                    string
	lastTotalText                                    string
	lastOn                                           *bool
	lastHeader                                       string
	lastTip                                          string
	lastRows                                         [trayRows]string
	lastErr                                          string
	busyUntil                                        time.Time
}

func (u *trayUI) onReady() {
	if u.ready != nil {
		close(u.ready)
	}
	systray.SetIcon(u.iconOff)
	systray.SetTooltip("net-merge")
	u.header = systray.AddMenuItem("net-merge", "")
	u.header.Disable()
	u.totalLine = systray.AddMenuItem("Traffic", "")
	u.totalLine.Disable()
	u.routeLine = systray.AddMenuItem("Game route", "")
	u.routeLine.Disable()
	u.routeLine.Hide()
	type menuLabel struct {
		item    *systray.MenuItem
		english string
		persian string
	}
	var labels []menuLabel
	localize := func(item *systray.MenuItem, en, fa string) {
		labels = append(labels, menuLabel{item, en, fa})
		item.SetTitle(u.text(en, fa))
	}
	systray.AddSeparator()
	u.limitClicks = make(chan limitClick, trayRows)
	for i := 0; i < trayRows; i++ {
		r := systray.AddMenuItem("", "Click to cap this adapter's speed (download and upload)")
		u.rows = append(u.rows, r)
		addLimit := func(mbitPerSec float64, title string) *systray.MenuItem {
			it := r.AddSubMenuItemCheckbox(title, limitTip(mbitPerSec), false)
			go func() {
				for range it.ClickedCh {
					u.limitClicks <- limitClick{row: i, mbitPerSec: mbitPerSec}
				}
			}()
			return it
		}
		u.limitUnlim[i] = addLimit(0, "Unlimited")
		localize(u.limitUnlim[i], "Unlimited", "بدون محدودیت")
		for _, mbitPerSec := range limitPresets {
			u.limitPreset[i] = append(u.limitPreset[i], addLimit(mbitPerSec, limitText(mbitPerSec)))
		}
		u.linkDetails[i] = r.AddSubMenuItem("Link statistics", "")
		u.linkDetails[i].Disable()
		// Add the children while the row is still in the menu: on Windows a hidden item is
		// removed from the menu, so converting it to a submenu afterwards fails and only the
		// entries added later (when they are first checked) would appear.
		r.Hide()
	}
	systray.AddSeparator()
	u.v6Item = systray.AddMenuItemCheckbox("Use all adapters (IPv6 only if every adapter has it)", "Ticked: apps are pushed to IPv4 so every adapter is used. Unticked: IPv6 is allowed and funnels onto the adapters that have it.", u.bal.V6Mode() == engine.V6Auto)
	u.autoItem = systray.AddMenuItemCheckbox("Pause when there is only one connection", "Pauses merging by itself when fewer than two connections have internet, and starts it again when a second one works. Pausing or starting by hand always wins.", u.ap.enabled)
	distribution := systray.AddMenuItem("Connection distribution", "Applies to new TCP and UDP flows")
	smart := distribution.AddSubMenuItemCheckbox("Smart (weighted)", "Uses the original scheduler", u.mode == "smart")
	roundRobin := distribution.AddSubMenuItemCheckbox("Round robin", "Alternates new flows across available adapters", u.mode == "round-robin")
	game := distribution.AddSubMenuItemCheckbox("Game mode", "Low-latency UDP path and automatic headroom for bulk traffic", u.mode == "game")
	setMode := func(mode string) {
		u.mode = mode
		u.sched.SetDistribution(mode)
		for name, item := range map[string]*systray.MenuItem{"smart": smart, "round-robin": roundRobin, "game": game} {
			if name == mode {
				item.Check()
			} else {
				item.Uncheck()
			}
		}
	}
	u.signInItem = systray.AddMenuItemCheckbox("Start at sign-in", "Starts net-merge when you sign in, with no password or admin prompt.", platform.AutostartEnabled())
	u.toggleItem = systray.AddMenuItem("Start merging", "")
	u.versionItem = systray.AddMenuItem(versionLabel(u.version), "The version of net-merge that is running (the same as net-merge version). Click to open the release page on GitHub.")
	quit := systray.AddMenuItem("Quit", "Turns system-wide mode off and exits")
	language := systray.AddMenuItem(u.text("فارسی", "English"), "")
	localize(u.v6Item, "Use all adapters (IPv6 only if every adapter has it)", "استفاده از همهٔ شبکه‌ها (IPv6 در صورت پشتیبانی همه)")
	localize(u.autoItem, "Pause when there is only one connection", "توقف خودکار با یک اتصال")
	localize(distribution, "Connection distribution", "حالت توزیع اتصال")
	localize(smart, "Smart (weighted)", "هوشمند (وزنی)")
	localize(roundRobin, "Round robin", "نوبتی")
	localize(game, "Game mode", "حالت بازی")
	localize(u.signInItem, "Start at sign-in", "شروع با ورود به ویندوز")
	localize(quit, "Quit", "خروج")
	localize(language, "فارسی", "English")
	go u.watchUpdates()

	go func() {
		u.ap.start(u.startOn)
		switch {
		case !u.startOn:
		case u.ap.enabled:
			// Merging starts when two connections work. The adapter is made now, before a phone is
			// plugged in, because on macOS making it later would drop that phone's link.
			if err := u.tun.Warm(); err != nil {
				logx.Printf("warning: %v\n", err)
			}
		default:
			u.turn(true)
		}
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		for {
			u.refresh()
			select {
			case <-language.ClickedCh:
				pref := u.text("fa", "en")
				if err := os.WriteFile(u.languagePath, []byte(pref), 0o600); err != nil {
					u.lastErr = err.Error()
					break
				}
				u.persian = !u.persian
				for _, label := range labels {
					label.item.SetTitle(u.text(label.english, label.persian))
				}
				u.lastOn = nil
			case <-u.toggleItem.ClickedCh:
				u.manual(!u.tun.IsOn())
			case <-u.autoItem.ClickedCh:
				if u.autoItem.Checked() {
					u.autoItem.Uncheck()
				} else {
					u.autoItem.Check()
				}
				u.ap.setEnabled(u.autoItem.Checked(), u.tun.IsOn())
			case <-smart.ClickedCh:
				if u.mode != "smart" {
					setMode("smart")
				}
			case <-roundRobin.ClickedCh:
				if u.mode != "round-robin" {
					setMode("round-robin")
				}
			case <-game.ClickedCh:
				if u.mode != "game" {
					setMode("game")
				}
			case <-u.signInItem.ClickedCh:
				on := !u.signInItem.Checked()
				u.lastErr = ""
				if err := platform.SetAutostart(on); err != nil {
					u.lastErr = "start at sign-in: " + err.Error()
					logx.Printf("error: %s\n", u.lastErr)
				} else if on {
					u.signInItem.Check()
				} else {
					u.signInItem.Uncheck()
				}
			case <-u.v6Item.ClickedCh:
				if u.v6Item.Checked() {
					u.v6Item.Uncheck()
					u.bal.SetV6Mode(engine.V6On)
				} else {
					u.v6Item.Check()
					u.bal.SetV6Mode(engine.V6Auto)
				}
			case <-u.versionItem.ClickedCh:
				if err := platform.OpenURL(update.ReleasesURL); err != nil {
					logx.Printf("error: opening the release page: %v\n", err)
				}
			case c := <-u.limitClicks:
				if c.name != "" {
					u.applyLimit(c.name, c.mbitPerSec)
				} else {
					u.applyLimit(u.rowNames[c.row], c.mbitPerSec)
				}
			case <-quit.ClickedCh:
				systray.Quit()
				return
			case <-tick.C:
				if req, ok := state.TakeLimit(u.stateDir); ok { // `net-merge limit ...` from a terminal
					u.applyLimit(req.Adapter, req.Mbps)
				}
				switch { // `net-merge on|off|quit` from a terminal
				case state.TakeMarker(u.stateDir, "quit"):
					systray.Quit()
					return
				case state.TakeMarker(u.stateDir, "off"):
					u.manual(false)
				case state.TakeMarker(u.stateDir, "on"):
					u.manual(true)
				}
				switch u.ap.tick(time.Now(), u.tun.IsOn(), u.usable(), u.movingFlows()) {
				case autoPause:
					u.turn(false)
				case autoResume:
					if u.turn(true); !u.tun.IsOn() {
						u.ap.resumeFailed()
					}
				}
			}
		}
	}()
}

// manual is a switch by the user (menu or `net-merge on|off`), which pausing by itself must respect.
func (u *trayUI) manual(on bool) {
	u.ap.userSet(on, u.usable())
	u.turn(on)
}

// usable is how many connections reach the internet right now.
func (u *trayUI) usable() (n int) {
	for _, s := range u.sched.Snapshot() {
		if s.Present && (s.State == "up" || s.State6 == "up") {
			n++
		}
	}
	return n
}

// movingFlows is how many connections net-merge is relaying while data is really moving. Idle
// keep-alive connections are always open, so they don't count and don't hold back a pause.
func (u *trayUI) movingFlows() (n int64) {
	var rate float64
	for _, s := range u.sched.Snapshot() {
		if s.Present {
			n += s.Active
			rate += s.RateIn + s.RateOut
		}
	}
	if rate < quietRate {
		return 0
	}
	return n
}

func (u *trayUI) turn(on bool) {
	u.lastErr = ""
	if !on {
		u.tun.Off()
		state.Clear(u.stateDir)
		return
	}
	u.setHeader("Turning on...")
	if err := u.tun.On(); err != nil {
		u.lastErr = err.Error()
		logx.Printf("error: %v\n", err)
	}
}

func (u *trayUI) setHeader(s string) {
	if s != u.lastHeader {
		u.header.SetTitle(s)
		u.lastHeader = s
	}
}

func (u *trayUI) text(en, fa string) string {
	if u.persian {
		return fa
	}
	return en
}

func (u *trayUI) refresh() {
	on := u.tun.IsOn()
	if u.lastOn == nil || *u.lastOn != on {
		if on {
			systray.SetIcon(u.iconOn)
			u.toggleItem.SetTitle(u.text("Pause merging", "توقف ادغام"))
		} else {
			systray.SetIcon(u.iconOff)
			u.toggleItem.SetTitle(u.text("Start merging", "شروع ادغام"))
		}
		u.lastOn = &on
	}

	// Speeds are each adapter's real traffic from the OS's counters, i.e. every app (qBittorrent binds
	// to adapters directly and never passes through net-merge), and they are shown even when paused.
	var live []engine.SlotStats
	var down, up float64
	for _, s := range u.sched.Snapshot() {
		if s.Present {
			live = append(live, s)
			down += s.LinkIn
			up += s.LinkOut
		}
	}
	state := u.text(stateWord(on), "متوقف")
	if on {
		state = u.text(state, "ادغام فعال")
	}
	if !on && u.ap.enabled && u.ap.waiting && !u.ap.manualOff {
		state = u.text("Paused: one connection", "متوقف: یک اتصال")
	}

	if u.lastErr != "" {
		u.setHeader("Error: " + shorten(u.lastErr, 70))
	} else {
		u.setHeader(state)
	}
	total := u.text("Total", "مجموع") + "  |  " + ltr(speedText(down, up))
	if total != u.lastTotalText {
		u.totalLine.SetTitle(total)
		u.lastTotalText = total
	}
	route := u.sched.LatestGameRoute()
	routeText := ""
	if on && u.mode == "game" && !route.At.IsZero() && time.Since(route.At) <= 30*time.Minute {
		basis := u.text("general probe", "سنجش عمومی")
		if route.Basis == "destination" {
			basis = u.text("game destination", "مقصد بازی")
		}
		routeText = u.text("Game", "مسیر بازی") + "  |  " + ltr(route.Adapter) + "  |  " + basis
	}
	if routeText != u.lastRouteText {
		if routeText == "" {
			u.routeLine.Hide()
		} else {
			u.routeLine.SetTitle(routeText)
			u.routeLine.Show()
		}
		u.lastRouteText = routeText
	}

	for i, r := range u.rows {
		if i >= len(live) {
			if u.lastRows[i] != "" {
				r.Hide()
				u.lastRows[i] = ""
				u.rowNames[i] = ""
			}
			continue
		}
		s := live[i]
		u.rowNames[i] = s.Name
		txt := ltr(shorten(s.Name, 32)) + "  |  " + ltr(speedText(s.LinkIn, s.LinkOut))
		status := u.text("Online", "وصل")
		if s.State != "up" && s.State6 != "up" {
			status = u.text("Checking", "در حال سنجش")
			if s.State == "no internet" || s.State6 == "no internet" {
				status = u.text("Offline", "قطع")
			}
		}
		details := []string{status}
		if s.ProbeRTT > 0 {
			details = append(details, ltr(fmt.Sprintf("RTT %d ms / Jitter %d ms", s.ProbeRTT.Milliseconds(), s.ProbeJitter.Milliseconds())))
		}
		if s.ProbeFail >= 0.05 {
			details = append(details, u.text("Probe errors", "خطای سنجش")+" "+ltr(fmt.Sprintf("%.0f%%", 100*s.ProbeFail)))
		}
		if s.GameDownMbps > 0 || s.GameUpMbps > 0 {
			details = append(details, u.text("Automatic cap", "سقف خودکار")+" "+ltr(fmt.Sprintf("↓%.1f ↑%.1f Mbps", s.GameDownMbps, s.GameUpMbps)))
		}
		if s.LimitMbps > 0 {
			details = append(details, u.text("Manual cap", "سقف دستی")+" "+ltr(limitText(s.LimitMbps)))
		}
		detail := strings.Join(details, "  |  ")
		if detail != u.lastDetails[i] {
			u.linkDetails[i].SetTitle(detail)
			u.lastDetails[i] = detail
		}
		if txt != u.lastRows[i] {
			r.SetTitle(txt)
			r.Show()
			u.lastRows[i] = txt
		}
		u.showLimit(i, s.LimitMbps)
	}

	// Hover tooltip: one line per adapter, so speeds are visible without opening the menu.
	tip := trayTooltip(state, down, up, live)
	if tip != u.lastTip {
		systray.SetTooltip(tip)
		u.lastTip = tip
	}
}

func ltr(value string) string {
	// Explicit direction prevents Windows from moving units and arrow/number pairs
	// when an adapter name and a Persian label appear in the same native menu row.
	return "\u200e\u202a" + strings.ReplaceAll(value, "&", "&&") + "\u202c\u200e"
}

func speedText(down, up float64) string {
	return fmt.Sprintf("↓%s  ↑%s Mbps", mbps(down), mbps(up))
}

// Windows 11 shows only the first 64 characters of a tray tooltip (measured: the text stopped mid-
// line at exactly 64), although the API accepts 128. So the hover text drops detail until it fits.
const tooltipMax = 63

// stateWord is how the tray names the two states.
func stateWord(on bool) string {
	if on {
		return "Merging"
	}
	return "Paused"
}

// versionLabel is the line above Quit, so a bug report has the version without a terminal.
// Release builds pass "0.3.3" and get "v0.3.3"; a build from source says "dev".
func versionLabel(v string) string {
	if v = strings.TrimSpace(v); v == "" {
		v = "dev"
	}
	return withV(v)
}

// updateLabel replaces the version line once GitHub has a newer release.
func updateLabel(latest string) string { return "Update to " + withV(latest) }

// withV adds the "v" that releases are tagged with, unless the build says "dev" or has one already.
func withV(v string) string {
	if v == "" || v == "dev" || strings.HasPrefix(v, "v") {
		return v
	}
	return "v" + v
}

// trayTooltip lists each adapter's speed and a Total line, e.g.
//
//	Merging
//	Eth 3 ↓16 ↑0.2
//	Eth 4 ↓79 ↑7.0
//	Total ↓95 ↑7.2 Mbps
//
// It drops detail (the app name, then per-adapter upload, then the unit) until it fits; every
// adapter keeps its download speed and the total is always shown.
func trayTooltip(state string, down, up float64, live []engine.SlotStats) string {
	build := func(head string, withUp bool, unit string) string {
		lines := []string{head}
		for _, s := range live {
			l := shortName(s.Name) + " ↓" + mbps(s.LinkIn)
			if withUp {
				l += " ↑" + mbps(s.LinkOut)
			}
			if s.State == "no internet" && s.State6 != "up" {
				l += " (no net)"
			}
			lines = append(lines, l)
		}
		lines = append(lines, "Total ↓"+mbps(down)+" ↑"+mbps(up)+unit)
		return strings.Join(lines, "\n")
	}
	leanest := build(state, false, "")
	for _, t := range []string{
		build("net-merge · "+state, true, " Mbps"),
		build(state, true, " Mbps"),
		build(state, false, " Mbps"),
		leanest,
	} {
		if utf16Len(t) <= tooltipMax {
			return t
		}
	}
	return shorten(leanest, tooltipMax)
}

// shortName keeps adapter names short enough for the tooltip ("Ethernet 3" -> "Eth 3").
var shortNamer = strings.NewReplacer("Ethernet", "Eth", "Wi-Fi", "WiFi", "Local Area Connection", "LAN", "Cellular", "Cell")

func shortName(n string) string { return shortNamer.Replace(n) }

func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		n += utf16.RuneLen(r)
	}
	return n
}

// mbps formats bytes/s as Mbit/s compactly: one decimal below 10, whole numbers above.
func mbps(bytesPerSec float64) string {
	m := bytesPerSec * 8 / 1e6
	if m < 9.95 {
		return fmt.Sprintf("%.1f", m)
	}
	return fmt.Sprintf("%.0f", m)
}

// shorten cuts s to at most n UTF-16 units (what Windows counts), ending in an ellipsis.
func shorten(s string, n int) string {
	if utf16Len(s) <= n {
		return s
	}
	units := 0
	for i, r := range s {
		w := utf16.RuneLen(r)
		if units+w > n-1 {
			return s[:i] + "…"
		}
		units += w
	}
	return s
}

// limitPresets are the caps offered in each adapter's submenu, in Mbit/s.
var limitPresets = []float64{1, 2, 5, 10, 20, 50, 100}

type limitClick struct {
	row        int
	name       string // set by `net-merge limit`, which knows the adapter by name, not row
	mbitPerSec float64
}

func limitLabel(mbitPerSec float64) string {
	if mbitPerSec == math.Trunc(mbitPerSec) {
		return fmt.Sprintf("%.0f", mbitPerSec)
	}
	return fmt.Sprintf("%.1f", mbitPerSec)
}

// limitText is a cap as the menu and status line show it, e.g. "12.5 Mbit/s".
func limitText(mbitPerSec float64) string {
	return limitLabel(mbitPerSec) + " Mbit/s"
}

func limitTip(mbitPerSec float64) string {
	if mbitPerSec <= 0 {
		return "No cap: the adapter may use its full speed."
	}
	return "Cap this adapter at " + limitText(mbitPerSec) +
		" for downloads and uploads, across all of its flows. Takes effect immediately."
}

// showLimit ticks the cap the adapter shown in row is using.
func (u *trayUI) showLimit(row int, mbitPerSec float64) {
	tick := func(it *systray.MenuItem, want bool) {
		if it != nil && it.Checked() != want {
			if want {
				it.Check()
			} else {
				it.Uncheck()
			}
		}
	}
	tick(u.limitUnlim[row], mbitPerSec <= 0)
	for i, it := range u.limitPreset[row] {
		tick(it, math.Abs(limitPresets[i]-mbitPerSec) < 0.01)
	}
}

// applyLimit caps the adapter with this name; 0 removes the cap. It runs on the menu goroutine,
// so the checkmarks can be updated right away instead of on the next refresh.
func (u *trayUI) applyLimit(name string, mbitPerSec float64) {
	if name == "" {
		return
	}
	u.sched.SetLimit(name, mbitPerSec)
	if err := state.SaveLimits(u.stateDir, u.sched.Limits()); err != nil {
		u.lastErr = "speed limit: " + err.Error()
		logx.Printf("error: %v\n", err)
	}
	for i, n := range u.rowNames {
		if n == name {
			u.showLimit(i, mbitPerSec)
		}
	}
}
