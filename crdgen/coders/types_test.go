package coders

import (
	"bytes"
	"io"
	"os"
	"testing"
)

func TestGenTypes(t *testing.T) {
	os.Setenv(EnvFormatCode, "1")

	const (
		preserveUnknownFields = `{
   "type": "object", 
   "additionalProperties": true,
   "x-kubernetes-preserve-unknown-fields": true
}`
	)

	specSchemaBytes, err := os.ReadFile("../testdata/enum.schema.json")
	if err != nil {
		t.Fatal(err)
	}

	// statusSchemaBytes, err := os.ReadFile("../../testdata/git.status.schema.json")
	// if err != nil {
	// 	t.Fatal(err)
	// }

	statusSchemaBytes := []byte(preserveUnknownFields)

	opts := Options{
		Group:        "test.krateo.io",
		Version:      "v1alpha1",
		Kind:         "Test",
		Categories:   []string{"krateo", "test"},
		SpecSchema:   specSchemaBytes,
		StatusSchema: statusSchemaBytes,
		Managed:      false,
	}

	dat, err := GenTypes(&opts)
	if err != nil {
		t.Fatal(err)
	}

	io.Copy(os.Stdout, bytes.NewReader(dat))
}
