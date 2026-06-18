package helm

import (
	"context"
	"fmt"

	helmconfig "github.com/krateoplatformops/plumbing/helm"
	"github.com/krateoplatformops/plumbing/helm/getter"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/release"
)

const (
	suppportedChartType = "application"
)

func (c *client) loadChart(ctx context.Context, chartRef string, opts []getter.Option) (*chart.Chart, error) {
	chartReader, _, err := getter.Get(ctx, chartRef, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to get chart from %s: %w", chartRef, err)
	}

	ch, err := loader.LoadArchive(chartReader)
	if err != nil {
		return nil, fmt.Errorf("failed to load chart: %w", err)
	}

	return ch, nil
}

func (c *client) checkDependencies(ctx context.Context, ch *chart.Chart, chartRef string, cfg *helmconfig.ActionConfig) (*chart.Chart, error) {
	if ch.Metadata.Dependencies == nil {
		return ch, nil
	}

	if err := action.CheckDependencies(ch, ch.Metadata.Dependencies); err != nil {
		if !cfg.DependencyUpdate {
			return nil, fmt.Errorf("missing dependencies: %w", err)
		}
		// Reload chart with updated dependencies
		reloaded, err := c.loadChart(ctx, chartRef, c.buildGetterOpts(cfg))
		if err != nil {
			return nil, fmt.Errorf("failed to reload chart after dependency update: %w", err)
		}
		return reloaded, nil
	}
	return ch, nil
}

func checkChartType(ch *chart.Chart) error {
	if ch.Metadata.Type != suppportedChartType && ch.Metadata.Type != "" {
		return fmt.Errorf("chart type mismatch: expected %s, got %s", suppportedChartType, ch.Metadata.Type)
	}
	return nil
}

func toWrapperRelease(rel *release.Release) *helmconfig.Release {
	return &helmconfig.Release{
		Name:         rel.Name,
		Namespace:    rel.Namespace,
		Revision:     rel.Version,
		ChartVersion: rel.Chart.Metadata.Version,
		Status:       helmconfig.Status(rel.Info.Status),
		Manifest:     rel.Manifest,
	}
}
