package e2e

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	xcontext "github.com/krateoplatformops/plumbing/context"
	"github.com/krateoplatformops/plumbing/env"
	"github.com/krateoplatformops/plumbing/jwtutil"
	"github.com/krateoplatformops/plumbing/kubeconfig"
	"github.com/krateoplatformops/plumbing/logger"
	"github.com/krateoplatformops/plumbing/signup"

	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/types"
	"sigs.k8s.io/yaml"
)

func Logger(traceId string) types.StepFunc {
	handler := logger.NewHandler(env.True("DEBUG"), os.Stderr)

	return func(ctx context.Context, _ *testing.T, _ *envconf.Config) context.Context {
		return xcontext.BuildContext(ctx,
			xcontext.WithTraceId(traceId),
			xcontext.WithLogger(slog.New(handler)),
		)
	}
}

type SignUpOptions struct {
	Username   string
	Groups     []string
	JWTSignKey string
	Namespace  string
	Duration   time.Duration
}

func SignUp(opts SignUpOptions) types.StepFunc {
	if opts.Duration == 0 {
		opts.Duration = time.Minute * 15
	}

	return func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
		accessToken, err := jwtutil.CreateToken(jwtutil.CreateTokenOptions{
			Username:   opts.Username,
			Groups:     opts.Groups,
			SigningKey: opts.JWTSignKey,
			Duration:   opts.Duration,
		})
		if err != nil {
			t.Fatal(err)
		}

		dat, err := os.ReadFile(cfg.KubeconfigFile())
		if err != nil {
			t.Fatal(err)
		}

		in := kubeconfig.KubeConfig{}
		if err := yaml.Unmarshal(dat, &in); err != nil {
			t.Fatal(err)
		}

		ep, err := signup.Do(context.TODO(), signup.Options{
			RestConfig:   cfg.Client().RESTConfig(),
			CAData:       in.Clusters[0].Cluster.CertificateAuthorityData,
			ServerURL:    in.Clusters[0].Cluster.Server, //"https://kubernetes.default.svc",
			CertDuration: opts.Duration,
			Namespace:    opts.Namespace,
			Username:     opts.Username,
			UserGroups:   opts.Groups,
		})
		if err != nil {
			t.Fatal(err)
		}

		return xcontext.BuildContext(ctx,
			xcontext.WithAccessToken(accessToken),
			xcontext.WithUserInfo(jwtutil.UserInfo{
				Username: opts.Username,
				Groups:   opts.Groups,
			}),
			xcontext.WithUserConfig(ep),
		)
	}
}
