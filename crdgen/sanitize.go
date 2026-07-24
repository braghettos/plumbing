package crdgen

import (
	"bytes"

	"gopkg.in/yaml.v3"
)

// sanitizeCRD makes controller-gen's output structurally valid for the API server without
// reformatting it. controller-gen can emit an object-shaped node (has properties /
// additionalProperties schema) with no `type`, or a completely empty `{}` node — from rich source
// JSON Schemas (e.g. loft/vcluster) the API server rejects both with "type: Required value: must
// not be empty for specified object fields".
//
// This walks the generated CRD's openAPIV3Schema as a yaml.Node (order- and style-preserving) and,
// tightly scoped, only INSERTS the missing bits:
//   - a `type: object` on any node that has properties (or an additionalProperties schema) but no
//     type, and
//   - `type: object` + `x-kubernetes-preserve-unknown-fields: true` on a genuinely empty/opaque node.
//
// It descends only through real schema positions (properties, items, additionalProperties,
// allOf/anyOf/oneOf); nothing else is touched. On any parse/marshal failure it returns the input
// unchanged, so it can never make output worse.
func sanitizeCRD(dat []byte) []byte {
	var doc yaml.Node
	if err := yaml.Unmarshal(dat, &doc); err != nil {
		return dat
	}
	root := documentRoot(&doc)
	if root == nil {
		return dat
	}
	changed := false
	if versions := mapValue(mapValue(root, "spec"), "versions"); versions != nil && versions.Kind == yaml.SequenceNode {
		for _, v := range versions.Content {
			if oa := mapValue(mapValue(v, "schema"), "openAPIV3Schema"); oa != nil {
				if sanitizeNode(oa) {
					changed = true
				}
			}
		}
	}
	// If the CRD was already structurally valid, return it byte-for-byte — never
	// re-serialize (and thus never reformat) a CRD we didn't need to fix.
	if !changed {
		return dat
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return dat
	}
	_ = enc.Close()
	out := buf.Bytes()
	// Preserve a leading document separator if controller-gen emitted one.
	if bytes.HasPrefix(bytes.TrimLeft(dat, " \t\n"), []byte("---")) &&
		!bytes.HasPrefix(bytes.TrimLeft(out, " \t\n"), []byte("---")) {
		out = append([]byte("---\n"), out...)
	}
	return out
}

func documentRoot(doc *yaml.Node) *yaml.Node {
	if doc.Kind == yaml.DocumentNode && len(doc.Content) > 0 {
		return doc.Content[0]
	}
	if doc.Kind == yaml.MappingNode {
		return doc
	}
	return nil
}

// mapValue returns the value node for key in a mapping node, or nil.
func mapValue(n *yaml.Node, key string) *yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}

func mapHas(n *yaml.Node, key string) bool { return mapValue(n, key) != nil }

func scalar(value, tag string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Value: value, Tag: tag}
}

func mapPut(n *yaml.Node, key string, value *yaml.Node) {
	n.Content = append(n.Content, scalar(key, "!!str"), value)
}

// sanitizeNode fixes n in place and reports whether it (or any descendant) changed.
func sanitizeNode(n *yaml.Node) bool {
	if n == nil || n.Kind != yaml.MappingNode {
		return false
	}

	changed := false
	hasType := mapHas(n, "type")
	props := mapValue(n, "properties")
	ap := mapValue(n, "additionalProperties")
	items := mapValue(n, "items")

	hasProps := props != nil && props.Kind == yaml.MappingNode && len(props.Content) > 0
	hasAPSchema := ap != nil && ap.Kind == yaml.MappingNode // a schema, not a bool

	// Object-shaped node missing an explicit type -> object.
	if !hasType && (hasProps || hasAPSchema) {
		mapPut(n, "type", scalar("object", "!!str"))
		hasType = true
		changed = true
	}

	// Genuinely empty/opaque node -> open object.
	if !hasType && !hasProps && !hasAPSchema && items == nil &&
		!mapHas(n, "x-kubernetes-preserve-unknown-fields") && !mapHas(n, "$ref") &&
		!mapHas(n, "allOf") && !mapHas(n, "anyOf") && !mapHas(n, "oneOf") &&
		!mapHas(n, "enum") && !mapHas(n, "format") {
		mapPut(n, "type", scalar("object", "!!str"))
		mapPut(n, "x-kubernetes-preserve-unknown-fields", scalar("true", "!!bool"))
		changed = true
	}

	// Descend only through real schema positions.
	if hasProps {
		for i := 1; i < len(props.Content); i += 2 {
			changed = sanitizeNode(props.Content[i]) || changed
		}
	}
	if items != nil {
		if items.Kind == yaml.MappingNode {
			changed = sanitizeNode(items) || changed
		}
		if items.Kind == yaml.SequenceNode {
			for _, it := range items.Content {
				changed = sanitizeNode(it) || changed
			}
		}
	}
	if hasAPSchema {
		changed = sanitizeNode(ap) || changed
	}
	for _, key := range []string{"allOf", "anyOf", "oneOf"} {
		if arr := mapValue(n, key); arr != nil && arr.Kind == yaml.SequenceNode {
			for _, e := range arr.Content {
				changed = sanitizeNode(e) || changed
			}
		}
	}
	return changed
}
