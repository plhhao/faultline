package config

import "go.yaml.in/yaml/v3"

// Encode emits a standalone config with normalized paths and no optional nulls.
func Encode(c Config) ([]byte, error) {
	var node yaml.Node
	if err := node.Encode(c); err != nil {
		return nil, err
	}
	var prune func(*yaml.Node)
	prune = func(n *yaml.Node) {
		if n.Kind == yaml.MappingNode {
			var children []*yaml.Node
			for i := 0; i < len(n.Content); i += 2 {
				if n.Content[i+1].Tag == "!!null" {
					continue
				}
				prune(n.Content[i+1])
				children = append(children, n.Content[i], n.Content[i+1])
			}
			n.Content = children
		} else {
			for _, child := range n.Content {
				prune(child)
			}
		}
	}
	prune(&node)
	return yaml.Marshal(&node)
}
