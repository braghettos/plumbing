package coders

import (
	"regexp"
	"slices"
	"strings"

	"github.com/krateoplatformops/plumbing/crdgen/schemas"
	"github.com/krateoplatformops/plumbing/ptr"
)

func schemaAsType(s *schemas.Schema) *schemas.Type {
	if s == nil {
		return nil
	}

	// se lo schema embedda l'oggetto (casi normali), duplichiamo quello
	if s.ObjectAsType != nil {
		return deepCopyType((*schemas.Type)(s.ObjectAsType))
	}

	t := &schemas.Type{
		Type:                 s.Type,
		Properties:           s.Properties,
		Items:                s.Items,
		Enum:                 s.Enum,
		Format:               s.Format,
		Required:             s.Required,
		AdditionalProperties: s.AdditionalProperties,
		AllOf:                s.AllOf,
		OneOf:                s.OneOf,
		AnyOf:                s.AnyOf,
		Default:              s.Default,
		Minimum:              s.Minimum,
		Maximum:              s.Maximum,
		MultipleOf:           s.MultipleOf,
		MaxLength:            s.MaxLength,
		MinLength:            s.MinLength,
		Pattern:              s.Pattern,
		Definitions:          s.Definitions,
		Ref:                  s.Ref,
	}

	return deepCopyType(t)
}

func deepCopyBool(b *bool) *bool {
	if b == nil {
		return nil
	}
	v := *b
	return &v
}

// deepCopyType esegue una copia ricorsiva di *schemas.Type
func deepCopyType(t *schemas.Type) *schemas.Type {
	if t == nil {
		return nil
	}

	// copia superficiale iniziale (copia i valori scalari)
	c := *t

	// copy TypeList (slice di string)
	if t.Type != nil {
		c.Type = make(schemas.TypeList, len(t.Type))
		copy(c.Type, t.Type)
	}

	// copy Required []string
	if t.Required != nil {
		c.Required = make([]string, len(t.Required))
		copy(c.Required, t.Required)
	}

	// copy Enum []interface{} (shallow copy degli elementi interface)
	if t.Enum != nil {
		c.Enum = make([]interface{}, len(t.Enum))
		copy(c.Enum, t.Enum)
	}

	// copy Properties map[string]*Type
	if t.Properties != nil {
		c.Properties = make(map[string]*schemas.Type, len(t.Properties))
		for k, v := range t.Properties {
			c.Properties[k] = deepCopyType(v)
		}
	}

	// copy Definitions map[string]*Type
	if t.Definitions != nil {
		c.Definitions = make(schemas.Definitions, len(t.Definitions))
		for k, v := range t.Definitions {
			c.Definitions[k] = deepCopyType(v)
		}
	}

	// copy DependentSchemas map[string]*Type
	if t.DependentSchemas != nil {
		c.DependentSchemas = make(map[string]*schemas.Type, len(t.DependentSchemas))
		for k, v := range t.DependentSchemas {
			c.DependentSchemas[k] = deepCopyType(v)
		}
	}

	// copy DependentRequired map[string][]string
	if t.DependentRequired != nil {
		c.DependentRequired = make(map[string][]string, len(t.DependentRequired))
		for k, vs := range t.DependentRequired {
			c.DependentRequired[k] = append([]string(nil), vs...)
		}
	}

	// copy AdditionalProperties
	if t.AdditionalProperties != nil {
		c.AdditionalProperties = &schemas.AdditionalProperties{}
		c.AdditionalProperties.IsBool = t.AdditionalProperties.IsBool
		c.AdditionalProperties.Bool = t.AdditionalProperties.Bool
		c.AdditionalProperties.Type = deepCopyType(t.AdditionalProperties.Type)
	}

	// copy Items
	if t.Items != nil {
		c.Items = deepCopyType(t.Items)
	}

	// copy AllOf / AnyOf / OneOf slices
	if len(t.AllOf) > 0 {
		c.AllOf = make([]*schemas.Type, len(t.AllOf))
		for i, v := range t.AllOf {
			c.AllOf[i] = deepCopyType(v)
		}
	}
	if len(t.AnyOf) > 0 {
		c.AnyOf = make([]*schemas.Type, len(t.AnyOf))
		for i, v := range t.AnyOf {
			c.AnyOf[i] = deepCopyType(v)
		}
	}
	if len(t.OneOf) > 0 {
		c.OneOf = make([]*schemas.Type, len(t.OneOf))
		for i, v := range t.OneOf {
			c.OneOf[i] = deepCopyType(v)
		}
	}

	return &c
}

func resolveRefDefs(t *schemas.Type, defs schemas.Definitions, visited map[string]bool) (*schemas.Type, error) {
	if t == nil {
		return nil, nil
	}

	if t.Ref != "" {
		refName := strings.TrimPrefix(t.Ref, "#/$defs/")
		if visited[refName] {
			return t, nil
		}
		visited[refName] = true

		if resolved, ok := defs[refName]; ok {
			return resolveRefDefs(resolved, defs, visited)
		}
		// fallback
		return t, nil
	}

	if len(t.AllOf) > 0 {
		resolvedTypes := make([]*schemas.Type, len(t.AllOf))
		for i, sub := range t.AllOf {
			r, err := resolveRefDefs(sub, defs, visited)
			if err != nil {
				return nil, err
			}
			resolvedTypes[i] = r
		}
		return schemas.AllOf(resolvedTypes, defs)
	}

	if t.Properties != nil {
		newProps := make(map[string]*schemas.Type, len(t.Properties))
		for k, v := range t.Properties {
			r, err := resolveRefDefs(v, defs, visited)
			if err != nil {
				return nil, err
			}
			newProps[k] = r
		}
		t.Properties = newProps
	}

	return t, nil
}

func isRequired(schema *schemas.Type, key string) bool {
	if schema == nil {
		return false
	}

	if slices.Contains(schema.Type, "null") {
		return true
	}

	for _, el := range schema.Required {
		if strings.EqualFold(key, el) {
			return true
		}
	}

	return false
}

func isNullable(schema *schemas.Type) bool {
	if schema == nil {
		return false
	}

	return slices.Contains(schema.Type, "null")
}

func mustPreserveUnknownFields(schema *schemas.Type) bool {
	if schema == nil {
		return false
	}

	if schema.Type.Equals(schemas.TypeList{"object"}) &&
		len(schema.Properties) == 0 {
		return true
	}

	if schema.AdditionalProperties != nil {
		return schema.AdditionalProperties.IsTrue()
	}

	return ptr.Deref(schema.PreserveUnknownFields, false)
}

// jsonSchemaToGoType converte un JSON Schema type/format in un tipo Go compatibile CRD
func jsonSchemaToGoType(t *schemas.Type) string {
	switch {
	case t.Type.Equals(schemas.TypeList{"string"}):
		switch t.Format {
		case "date-time":
			return "metav1.Time"
		case "duration":
			return "metav1.Duration"
		case "quantity":
			return "resource.Quantity"
		default:
			return "string"
		}

	case t.Type.Equals(schemas.TypeList{"boolean"}):
		return "bool"

	case t.Type.Equals(schemas.TypeList{"integer"}):
		// se lo schema specifica format=int64 → usa int64
		if t.Format == "int64" {
			return "int64"
		}
		return "int32"

	case t.Type.Equals(schemas.TypeList{"number"}):
		return "float64"

	case t.Type.Equals(schemas.TypeList{"array"}):
		if t.Items != nil {
			itemType := jsonSchemaToGoType(t.Items)
			return "[]" + itemType
		}
		return "[]runtime.RawExtension"

	case t.Type.Equals(schemas.TypeList{"object"}):
		// if t.AdditionalProperties != nil {
		// 	valType := jsonSchemaToGoType(t.AdditionalProperties.Schema)
		// 	return "map[string]" + valType
		// }
		if len(t.Properties) > 0 {
			// struct → deve essere costruita altrove (es. buildStruct)
			// qui ritorniamo un placeholder
			return "runtime.RawExtension"
		}
		return "runtime.RawExtension"
	}

	// fallback → JSON arbitrario
	return "runtime.RawExtension"
}

var nonAlphaNum = regexp.MustCompile(`[^a-zA-Z0-9]+`)

// exportedName converts a JSON-style key into a valid Go exported field name.
func exportedName(name string) string {
	if name == "" {
		return ""
	}

	// Replace non-alphanumeric separators (e.g., -, _, space, .) with a single underscore
	name = nonAlphaNum.ReplaceAllString(name, "_")

	parts := strings.Split(name, "_")
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + strings.ToLower(p[1:])
	}

	return strings.Join(parts, "")
}

// For go package and k8s version must complain to this pattern:
//
// [a-z]([-a-z0-9]*[a-z0-9])?
//
// Go package folders allow only underscore char ('_')
// K8s CRD version allow only dash char ('-')
func normalizeVersion(ver string, replaceChar rune) string {
	ver = strings.ToLower(ver)

	// Sostituisce tutti i caratteri non alfanumerici con replaceChar
	re := regexp.MustCompile(`[^a-z0-9]+`)
	ver = re.ReplaceAllString(ver, string(replaceChar))

	// Rimuove caratteri speciali all'inizio e alla fine
	ver = strings.Trim(ver, string(replaceChar))

	// Assicura che inizi con una lettera
	if len(ver) > 0 && ver[0] >= '0' && ver[0] <= '9' {
		ver = "v" + ver
	}

	return ver
}
