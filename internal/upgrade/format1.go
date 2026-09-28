package upgrade

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"go.yaml.in/yaml/v3"

	"github.com/pdutton/DockIt/internal/store"
)

// addTaskType upgrades format 1 to 2 by giving every task the type "task",
// written after its title.  Format 2 also added optional fields and
// enumeration values, which need nothing here.
//
// It edits the YAML nodes rather than decoding into model.Task, so it keeps
// working however that type changes later.
func addTaskType(root string) error {
	paths, err := filepath.Glob(filepath.Join(root, "projects", "*", "tasks", "*.yaml"))
	if err != nil {
		return err
	}
	for _, path := range paths {
		if err := addTaskTypeTo(path); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
	}
	return nil
}

func addTaskTypeTo(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return err
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return fmt.Errorf("not a YAML mapping")
	}
	m := doc.Content[0]

	// Keys and values alternate.  Insert after the title, or at the end if
	// there is none, which `dockit check` will then report.
	at := len(m.Content)
	for i := 0; i < len(m.Content); i += 2 {
		switch m.Content[i].Value {
		case "type":
			return nil // already done, by an earlier run that failed later
		case "title":
			at = i + 2
		}
	}
	kv := []*yaml.Node{
		{Kind: yaml.ScalarNode, Tag: "!!str", Value: "type"},
		{Kind: yaml.ScalarNode, Tag: "!!str", Value: "task"},
	}
	m.Content = append(m.Content[:at], append(kv, m.Content[at:]...)...)

	// Encode as the store does, so the file reads as if DockIt wrote it.
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return err
	}
	if err := enc.Close(); err != nil {
		return err
	}
	return store.WriteFileAtomic(path, buf.Bytes())
}
