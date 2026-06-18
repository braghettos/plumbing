package helm

import "testing"

func TestRESTClientGetterUsesProvidedNamespace(t *testing.T) {
	getter := NewRESTClientGetterWithCachedClients("demo-namespace", nil, nil, nil)

	ns, _, err := getter.ToRawKubeConfigLoader().Namespace()
	if err != nil {
		t.Fatalf("expected namespace lookup to succeed: %v", err)
	}

	if ns != "demo-namespace" {
		t.Fatalf("expected namespace %q, got %q", "demo-namespace", ns)
	}
}
