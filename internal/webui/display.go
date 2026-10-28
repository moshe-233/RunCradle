package webui

import (
	"encoding/json"

	"runcradle/internal/workflow"
)

// Display is the same exact-text projection used by static exports and online views.
func Display(d workflow.Document) (json.RawMessage, error) { return displayData(d) }

type nodeDisplay struct {
	ID              string            `json:"id"`
	Name            string            `json:"name"`
	Label           string            `json:"label"`
	DeclarationText map[string]string `json:"declaration_text"`
	workbenchNode
}
type inputDisplay struct {
	Name     string             `json:"name"`
	Label    string             `json:"label"`
	Type     string             `json:"type"`
	Default  *string            `json:"default_text,omitempty"`
	Enum     []string           `json:"enum_text,omitempty"`
	Bindings []workflow.Binding `json:"bindings"`
}
type presetDisplay struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Values string `json:"values_text"`
}

// Browser data contains display text, never numbers that JS could round.
func displayData(d workflow.Document) ([]byte, error) {
	payload := struct {
		View struct {
			workflow.View
			Nodes []nodeDisplay `json:"nodes"`
		} `json:"view"`
		Metadata struct {
			Inputs  []inputDisplay  `json:"inputs"`
			Presets []presetDisplay `json:"presets"`
		} `json:"metadata"`
		Source   string `json:"source"`
		UISource string `json:"ui_source"`
		Graph    struct {
			Nodes  []positioned `json:"nodes"`
			Edges  []connection `json:"edges"`
			Width  int          `json:"width"`
			Height int          `json:"height"`
		} `json:"graph"`
	}{Source: string(d.Source), UISource: string(d.UIRaw)}
	payload.View.View = d.View
	payload.Graph.Nodes, payload.Graph.Edges, payload.Graph.Width, payload.Graph.Height = layout(d.View)
	payload.View.Nodes = []nodeDisplay{}
	var formatErr error
	text := func(value any) string {
		if s, ok := value.(string); ok {
			return s
		}
		raw, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			formatErr = err
		}
		return string(raw)
	}
	for _, node := range d.View.Nodes {
		fields := map[string]string{}
		for key, value := range node.Declaration {
			fields[key] = text(value)
		}
		payload.View.Nodes = append(payload.View.Nodes, nodeDisplay{ID: node.ID, Name: node.Name, Label: node.Label, DeclarationText: fields, workbenchNode: projectNode(d, node)})
	}
	if d.Metadata != nil {
		for _, input := range d.Metadata.Inputs {
			in := inputDisplay{Name: input.Name, Label: input.Label, Type: input.Type, Bindings: input.Bindings}
			if len(input.Default) > 0 {
				value := text(input.Default)
				in.Default = &value
			}
			for _, value := range input.Enum {
				in.Enum = append(in.Enum, text(value))
			}
			payload.Metadata.Inputs = append(payload.Metadata.Inputs, in)
		}
		for _, preset := range d.Metadata.Presets {
			payload.Metadata.Presets = append(payload.Metadata.Presets, presetDisplay{preset.ID, preset.Label, text(preset.Values)})
		}
	}
	if formatErr != nil {
		return nil, formatErr
	}
	return json.Marshal(payload)
}
