package utils

import (
	"bytes"
	"strings"
	"testing"
)

func TestLabelsPostRender_Traceparent(t *testing.T) {
	const tp = "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"

	in := bytes.NewBufferString("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: x\n")
	out, err := (&LabelsPostRender{CompositionName: "comp"}).
		WithTraceparent(tp, "vendor=state").
		Run(in)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	s := out.String()
	if !strings.Contains(s, "krateo.io/traceparent: "+tp) {
		t.Fatalf("traceparent annotation missing:\n%s", s)
	}
	if !strings.Contains(s, "krateo.io/tracestate: vendor=state") {
		t.Fatalf("tracestate annotation missing:\n%s", s)
	}

	// Additive: without WithTraceparent there is no traceparent annotation.
	in2 := bytes.NewBufferString("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: y\n")
	out2, err := (&LabelsPostRender{CompositionName: "comp"}).Run(in2)
	if err != nil {
		t.Fatalf("Run(no traceparent): %v", err)
	}
	if strings.Contains(out2.String(), "krateo.io/traceparent") {
		t.Fatalf("traceparent must be absent when unset:\n%s", out2.String())
	}
}
