package helm

import (
	"bytes"
	"fmt"
	"io"
	"testing"
	"time"

	"helm.sh/helm/v3/pkg/kube"
	corev1 "k8s.io/api/core/v1"
)

type recordingPostRenderer struct {
	wantInput string
	output    string
	called    bool
}

func (r *recordingPostRenderer) Run(renderedManifests *bytes.Buffer) (*bytes.Buffer, error) {
	r.called = true

	if got := renderedManifests.String(); got != r.wantInput {
		return nil, fmt.Errorf("unexpected input to user postrenderer: got %q want %q", got, r.wantInput)
	}

	return bytes.NewBufferString(r.output), nil
}

type capturingKubeClient struct {
	wantInput string
	seenInput string
	called    bool
}

func (c *capturingKubeClient) Create(resources kube.ResourceList) (*kube.Result, error) {
	panic("not used")
}

func (c *capturingKubeClient) Wait(resources kube.ResourceList, timeout time.Duration) error {
	panic("not used")
}

func (c *capturingKubeClient) WaitWithJobs(resources kube.ResourceList, timeout time.Duration) error {
	panic("not used")
}

func (c *capturingKubeClient) Delete(resources kube.ResourceList) (*kube.Result, []error) {
	panic("not used")
}

func (c *capturingKubeClient) WatchUntilReady(resources kube.ResourceList, timeout time.Duration) error {
	panic("not used")
}

func (c *capturingKubeClient) Update(original, target kube.ResourceList, force bool) (*kube.Result, error) {
	panic("not used")
}

func (c *capturingKubeClient) Build(reader io.Reader, validate bool) (kube.ResourceList, error) {
	c.called = true

	if validate {
		return nil, fmt.Errorf("expected validate to be false")
	}

	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}

	c.seenInput = string(data)
	if c.wantInput != "" && c.seenInput != c.wantInput {
		return nil, fmt.Errorf("unexpected input to validator: got %q want %q", c.seenInput, c.wantInput)
	}

	return nil, nil
}

func (c *capturingKubeClient) WaitAndGetCompletedPodPhase(name string, timeout time.Duration) (corev1.PodPhase, error) {
	panic("not used")
}

func (c *capturingKubeClient) IsReachable() error {
	return nil
}

func TestWithDuplicateResourceValidation_ChainsUserPostRenderer(t *testing.T) {
	userInput := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: example\n"
	renderedOutput := userInput + "---\napiVersion: v1\nkind: Secret\nmetadata:\n  name: rendered\n"

	userRenderer := &recordingPostRenderer{
		wantInput: userInput,
		output:    renderedOutput,
	}
	kubeClient := &capturingKubeClient{
		wantInput: renderedOutput,
	}

	postRenderer := withDuplicateResourceValidation(userRenderer, kubeClient)
	result, err := postRenderer.Run(bytes.NewBufferString(userInput))
	if err != nil {
		t.Fatalf("Run() returned error: %v", err)
	}

	if !userRenderer.called {
		t.Fatal("expected user postrenderer to be called")
	}
	if !kubeClient.called {
		t.Fatal("expected validator to be called")
	}
	if got := result.String(); got != renderedOutput {
		t.Fatalf("unexpected output buffer: got %q want %q", got, renderedOutput)
	}
}
