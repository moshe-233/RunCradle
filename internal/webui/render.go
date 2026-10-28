package webui

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"fmt"
	"html/template"
	"sort"
	"strings"
	"time"

	"runcradle/internal/workflow"
)

//go:embed assets/*
var assets embed.FS

const MaxHTML = 32 << 20

type htmlBuffer struct{ data bytes.Buffer }

func (b *htmlBuffer) Write(p []byte) (int, error) {
	if len(p) > MaxHTML-b.data.Len() {
		return 0, fmt.Errorf("HTML export exceeds 32 MiB")
	}
	return b.data.Write(p)
}

type positioned struct {
	ID, Name, Label, ShortLabel string
	X, Y                        int
}
type connection struct {
	FromID, ToID, FromName, ToName string
	Path, Condition, Label         string
	X, Y                           int
}
type pageData struct {
	Title, Description, Revision, Generated, Policy string
	Style                                           template.CSS
	Script                                          template.JS
	Data                                            template.JS
	Nodes                                           []positioned
	Edges                                           []connection
	Width, Height                                   int
	Issues                                          []workflow.Issue
	Complete, RawEditOnly                           bool
	Source, UI                                      string
}

func Render(d workflow.Document, at time.Time) ([]byte, error) {
	if !d.Valid() {
		return nil, fmt.Errorf("cannot render an invalid document")
	}
	css, err := assets.ReadFile("assets/style.css")
	if err != nil {
		return nil, err
	}
	themeCSS, err := assets.ReadFile("assets/theme.css")
	if err != nil {
		return nil, err
	}
	css = append(themeCSS, css...)
	script, err := assets.ReadFile("assets/app.js")
	if err != nil {
		return nil, err
	}
	graphScript, err := assets.ReadFile("assets/graph.js")
	if err != nil {
		return nil, err
	}
	script = append(append(graphScript, byte('\n')), script...)
	themeScript, err := assets.ReadFile("assets/theme.js")
	if err != nil {
		return nil, err
	}
	script = append(append(themeScript, byte('\n')), script...)
	graphStyle, err := assets.ReadFile("assets/graph.css")
	if err != nil {
		return nil, err
	}
	css = append(css, graphStyle...)
	markup, err := assets.ReadFile("assets/index.html")
	if err != nil {
		return nil, err
	}
	data, err := displayData(d)
	if err != nil {
		return nil, err
	}
	if len(data) > MaxHTML {
		return nil, fmt.Errorf("HTML data exceeds 32 MiB")
	}
	hash := func(raw []byte) string { sum := sha256.Sum256(raw); return base64.StdEncoding.EncodeToString(sum[:]) }
	p := pageData{Title: d.View.Title, Description: d.View.Description, Revision: d.Revision, Generated: at.UTC().Format(time.RFC3339), Style: template.CSS(css), Script: template.JS(script), Data: template.JS(data), Source: string(d.Source), UI: string(d.UIRaw), Complete: d.View.Complete, RawEditOnly: d.View.RawEditOnly, Issues: d.Issues}
	p.Policy = "default-src 'none'; script-src 'sha256-" + hash(script) + "'; style-src 'sha256-" + hash(css) + "'; connect-src 'none'; img-src data:; base-uri 'none'; object-src 'none'; form-action 'none'"
	p.Nodes, p.Edges, p.Width, p.Height = layout(d.View)
	t, err := template.New("page").Parse(string(markup))
	if err != nil {
		return nil, err
	}
	var out htmlBuffer
	if err = t.Execute(&out, p); err != nil {
		return nil, err
	}
	return out.data.Bytes(), nil
}
func layout(view workflow.View) ([]positioned, []connection, int, int) {
	indegree := map[string]int{}
	children := map[string][]string{}
	rank := map[string]int{}
	for _, n := range view.Nodes {
		indegree[n.Name] = 0
	}
	for _, e := range view.Edges {
		indegree[e.To]++
		children[e.From] = append(children[e.From], e.To)
	}
	queue := []string{}
	for _, n := range view.Nodes {
		if indegree[n.Name] == 0 {
			queue = append(queue, n.Name)
		}
	}
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		for _, c := range children[n] {
			if rank[c] < rank[n]+1 {
				rank[c] = rank[n] + 1
			}
			indegree[c]--
			if indegree[c] == 0 {
				queue = append(queue, c)
			}
		}
	}
	// Stable topological layer ordering with parent barycenters.
	layers := map[int][]workflow.Node{}
	maxRank := 0
	for _, n := range view.Nodes {
		r := rank[n.Name]
		layers[r] = append(layers[r], n)
		if r > maxRank {
			maxRank = r
		}
	}
	row := map[string]int{}
	ordered := []workflow.Node{}
	for r := 0; r <= maxRank; r++ {
		layer := layers[r]
		score := func(name string) float64 {
			sum, count := 0, 0
			for _, e := range view.Edges {
				if e.To == name {
					sum += row[e.From]
					count++
				}
			}
			if count == 0 {
				return 0
			}
			return float64(sum) / float64(count)
		}
		sort.SliceStable(layer, func(i, j int) bool {
			a, b := score(layer[i].Name), score(layer[j].Name)
			if a == b {
				return layer[i].Name < layer[j].Name
			}
			return a < b
		})
		for i, n := range layer {
			row[n.Name] = i
			ordered = append(ordered, n)
		}
	}
	positions := map[string]positioned{}
	nodes := []positioned{}
	maxRows := 1
	for _, layer := range layers {
		if len(layer) > maxRows {
			maxRows = len(layer)
		}
	}
	width, height := 660, 220
	for _, n := range ordered {
		x, y := 40+rank[n.Name]*320, 50+row[n.Name]*190+(maxRows-len(layers[rank[n.Name]]))*95
		label := []rune(n.Label)
		if len(label) > 12 {
			label = append(label[:11], '…')
		}
		p := positioned{ID: n.ID, Name: n.Name, Label: n.Label, ShortLabel: string(label), X: x, Y: y}
		positions[n.Name] = p
		nodes = append(nodes, p)
		if x+280 > width {
			width = x + 280
		}
		if y+190 > height {
			height = y + 190
		}
	}
	edges := []connection{}
	for _, e := range view.Edges {
		a, b := positions[e.From], positions[e.To]
		x1, y1, x2, y2 := a.X+240, a.Y+72, b.X, b.Y+72
		mid := (x1 + x2) / 2
		edges = append(edges, connection{FromID: a.ID, ToID: b.ID, FromName: e.From, ToName: e.To, Path: fmt.Sprintf("M %d %d C %d %d %d %d %d %d", x1, y1, mid, y1, mid, y2, x2, y2), Condition: strings.TrimSpace(e.Condition), Label: conditionLabel(e.Condition), X: mid, Y: (y1+y2)/2 - 10})
	}
	return nodes, edges, width, height
}

func conditionLabel(condition string) string {
	switch condition {
	case "process_started":
		return "启动后"
	case "process_completed":
		return "结束后"
	case "process_completed_successfully":
		return "成功完成后"
	case "process_healthy":
		return "健康就绪后"
	case "process_log_ready":
		return "日志就绪后"
	}
	return "未显式指定"
}
