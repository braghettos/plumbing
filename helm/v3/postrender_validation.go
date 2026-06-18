package helm

import (
	"bytes"
	"fmt"

	helmconfig "github.com/krateoplatformops/plumbing/helm"
	"helm.sh/helm/v3/pkg/kube"
	"k8s.io/cli-runtime/pkg/resource"
)

type postRendererChain struct {
	renderers []helmconfig.PostRenderer
}

func (c postRendererChain) Run(renderedManifests *bytes.Buffer) (*bytes.Buffer, error) {
	current := renderedManifests
	for _, renderer := range c.renderers {
		if renderer == nil {
			continue
		}

		modified, err := renderer.Run(current)
		if err != nil {
			return modified, err
		}
		if modified == nil {
			return nil, fmt.Errorf("post renderer returned a nil manifest buffer")
		}
		current = modified
	}

	return current, nil
}

type duplicateResourceValidator struct {
	kubeClient kube.Interface
}

func (v duplicateResourceValidator) Run(renderedManifests *bytes.Buffer) (*bytes.Buffer, error) {
	if v.kubeClient == nil || renderedManifests == nil {
		return renderedManifests, nil
	}

	resources, err := v.kubeClient.Build(bytes.NewReader(renderedManifests.Bytes()), false)
	if err != nil {
		return renderedManifests, fmt.Errorf("failed to build rendered manifests: %w", err)
	}

	seen := make(map[string]struct{}, len(resources))
	err = resources.Visit(func(info *resource.Info, err error) error {
		if err != nil {
			return err
		}
		if info == nil || info.Object == nil {
			return nil
		}

		key := renderedResourceKey(info)
		if _, exists := seen[key]; exists {
			return fmt.Errorf("rendered manifest contains duplicate resource %s", key)
		}
		seen[key] = struct{}{}
		return nil
	})
	if err != nil {
		return renderedManifests, err
	}

	return renderedManifests, nil
}

func withDuplicateResourceValidation(renderer helmconfig.PostRenderer, kubeClient kube.Interface) helmconfig.PostRenderer {
	validator := duplicateResourceValidator{kubeClient: kubeClient}
	if renderer == nil {
		return validator
	}

	return postRendererChain{renderers: []helmconfig.PostRenderer{renderer, validator}}
}

func renderedResourceKey(info *resource.Info) string {
	gvk := info.Object.GetObjectKind().GroupVersionKind()
	return fmt.Sprintf("%s/%s/%s/%s", gvk.GroupVersion().String(), gvk.Kind, info.Namespace, info.Name)
}
