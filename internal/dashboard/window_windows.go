package dashboard

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"

	"github.com/lxn/walk"
	d "github.com/lxn/walk/declarative"

	"net-merge/internal/engine"
)

var (
	navy  = walk.RGB(13, 29, 49)
	mint  = walk.RGB(125, 251, 209)
	ice   = walk.RGB(235, 244, 255)
	ink   = walk.RGB(24, 40, 63)
	muted = walk.RGB(76, 99, 126)
	paper = walk.RGB(246, 249, 253)
	card  = walk.RGB(228, 239, 251)
)

func fill(color walk.Color) d.SolidColorBrush { return d.SolidColorBrush{Color: color} }

func font(size int, bold bool) d.Font { return d.Font{Family: "Segoe UI", PointSize: size, Bold: bold} }

func metric(label string, value **walk.Label) d.Composite {
	return d.Composite{
		StretchFactor: 1, Background: fill(card),
		Layout: d.VBox{Margins: d.Margins{Left: 14, Right: 14, Top: 7, Bottom: 7}, Spacing: 2},
		Children: []d.Widget{
			d.Label{Text: label, TextColor: muted, Font: font(9, true)},
			d.Label{AssignTo: value, Text: "0.0 Mbps", TextColor: ink, Font: font(19, true)},
		},
	}
}

type adapterTable struct {
	walk.TableModelBase
	rows []engine.SlotStats
}

func (m *adapterTable) RowCount() int { return len(m.rows) }

func (m *adapterTable) Value(row, column int) any {
	adapter := m.rows[row]
	switch column {
	case 0:
		return adapter.Name
	case 1:
		if adapter.State == "up" || adapter.State6 == "up" {
			return "Online"
		}
		return adapter.State
	case 2:
		return rate(adapter.LinkIn)
	case 3:
		return rate(adapter.LinkOut)
	case 4:
		return adapter.Active
	case 5:
		if adapter.LimitMbps > 0 {
			return fmt.Sprintf("%g", adapter.LimitMbps)
		}
		return "Unlimited"
	}
	return ""
}

func (m *adapterTable) replace(adapters []engine.SlotStats) {
	m.rows = m.rows[:0]
	for _, a := range adapters {
		if a.Present {
			m.rows = append(m.rows, a)
		}
	}
	m.PublishRowsReset()
}

func rate(bytesPerSecond float64) string { return fmt.Sprintf("%.1f", bytesPerSecond*8/1e6) }

// Run is the Windows-only UI process entry point.
func Run(version string) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	var window *walk.MainWindow
	var status, download, upload, count, errorLine *walk.Label
	var start, pause *walk.PushButton
	var smart, turnTaking *walk.RadioButton
	model := &adapterTable{}
	output := json.NewEncoder(os.Stdout)
	syncing := false
	send := func(action Command) func() {
		return func() {
			if !syncing {
				_ = output.Encode(action)
			}
		}
	}
	if err := (d.MainWindow{
		AssignTo: &window, Title: "NetMerge  |  Windows", Font: font(10, false),
		Size: d.Size{Width: 765, Height: 545}, MinSize: d.Size{Width: 765, Height: 525},
		Background: fill(paper), Layout: d.VBox{Margins: d.Margins{Left: 18, Right: 18, Top: 18, Bottom: 18}, Spacing: 12},
		Children: []d.Widget{
			d.Composite{
				Background: fill(navy), Layout: d.HBox{Margins: d.Margins{Left: 18, Right: 18, Top: 11, Bottom: 11}},
				Children: []d.Widget{
					d.Composite{Background: fill(navy), Layout: d.VBox{MarginsZero: true, Spacing: 3}, Children: []d.Widget{
						d.Label{Text: "NetMerge", TextColor: ice, Font: font(23, true)},
						d.Label{AssignTo: &status, Text: "Connecting to tray…", TextColor: mint, Font: font(10, true)},
					}},
					d.HSpacer{},
					d.Label{Text: version, TextColor: ice, Font: font(11, true)},
				},
			},
			d.Composite{Layout: d.HBox{MarginsZero: true, Spacing: 9}, Children: []d.Widget{
				metric("DOWNLOAD", &download), metric("UPLOAD", &upload), metric("LINKS ONLINE", &count),
			}},
			d.Label{Text: "Network adapters   /   Live link traffic", TextColor: ink, Font: font(11, true)},
			d.TableView{Model: model, MinSize: d.Size{Height: 125}, AlternatingRowBG: true, LastColumnStretched: true, Columns: []d.TableViewColumn{
				{Title: "Adapter", Width: 156}, {Title: "Status", Width: 108}, {Title: "Down Mbps", Width: 91},
				{Title: "Up Mbps", Width: 82}, {Title: "Flows", Width: 63}, {Title: "Cap Mbps", Width: 88},
			}},
			d.Composite{Layout: d.HBox{MarginsZero: true, Spacing: 9}, Children: []d.Widget{
				d.Label{Text: "Distribution", TextColor: ink, Font: font(10, true)},
				d.RadioButton{AssignTo: &smart, Text: "Smart", OnClicked: send(Smart), ToolTipText: "Weighted by estimated link capacity and how busy the links are."},
				d.RadioButton{AssignTo: &turnTaking, Text: "Round robin", OnClicked: send(RoundRobin), ToolTipText: "Alternates new connections across available links."},
				d.HSpacer{},
				d.PushButton{AssignTo: &start, Text: "Start", Enabled: false, OnClicked: send(Start)},
				d.PushButton{AssignTo: &pause, Text: "Pause", Enabled: false, OnClicked: send(Pause)},
				d.PushButton{Text: "Releases", OnClicked: send(Releases)},
			}},
			d.Label{AssignTo: &errorLine, TextColor: walk.RGB(165, 39, 44), EllipsisMode: d.EllipsisEnd},
		},
	}).Create(); err != nil {
		return err
	}

	go func() {
		input := json.NewDecoder(os.Stdin)
		for {
			var snapshot Snapshot
			if err := input.Decode(&snapshot); err != nil {
				window.Synchronize(func() { _ = window.Close() })
				return
			}
			window.Synchronize(func() {
				syncing = true
				defer func() { syncing = false }()
				model.replace(snapshot.Adapters)
				var down, up float64
				online := 0
				for _, adapter := range model.rows {
					down += adapter.LinkIn
					up += adapter.LinkOut
					if adapter.State == "up" || adapter.State6 == "up" {
						online++
					}
				}
				download.SetText(rate(down) + " Mbps")
				upload.SetText(rate(up) + " Mbps")
				count.SetText(fmt.Sprintf("%d / %d", online, len(model.rows)))
				text := "Paused"
				if snapshot.On {
					text = "Merging"
				} else if snapshot.Waiting {
					text = "Waiting for two online links"
				}
				status.SetText(text)
				start.SetEnabled(!snapshot.On)
				pause.SetEnabled(snapshot.On)
				smart.SetChecked(!snapshot.RoundRobin)
				turnTaking.SetChecked(snapshot.RoundRobin)
				errorLine.SetText(snapshot.Error)
			})
		}
	}()
	window.Run()
	return nil
}
