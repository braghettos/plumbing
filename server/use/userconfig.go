package use

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	xcontext "github.com/krateo-platformops/plumbing/context"
	"github.com/krateo-platformops/plumbing/endpoints"
	"github.com/krateo-platformops/plumbing/http/response"
	"github.com/krateo-platformops/plumbing/jwtutil"
	"github.com/krateo-platformops/plumbing/kubeutil"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/rest"
)

func UserConfig(signingKey, authnNS string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		fn := func(wri http.ResponseWriter, req *http.Request) {
			authHeader := req.Header.Get("Authorization")
			if authHeader == "" {
				response.Unauthorized(wri, fmt.Errorf("missing authorization header"))
				return
			}

			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
				response.Unauthorized(wri, fmt.Errorf("invalid authorization header format"))
				return
			}

			userInfo, err := jwtutil.Validate(signingKey, parts[1])
			if err != nil {
				if errors.Is(err, jwtutil.ErrTokenExpired) {
					response.Unauthorized(wri, err)
				} else {
					response.Unauthorized(wri, err)
				}
				return
			}

			sarc, err := rest.InClusterConfig()
			if err != nil {
				response.InternalError(wri, fmt.Errorf("unable to create in cluster config: %w", err))
				return
			}

			ep, err := endpoints.FromSecret(context.Background(), sarc,
				fmt.Sprintf("%s-clientconfig",
					kubeutil.MakeDNS1123Compatible(userInfo.Username)), authnNS)
			if err != nil {
				if apierrors.IsNotFound(err) {
					response.Unauthorized(wri, err)
					return
				}
				response.InternalError(wri, err)
				return
			}

			ctx := xcontext.BuildContext(req.Context(),
				xcontext.WithAccessToken(parts[1]),
				xcontext.WithUserInfo(userInfo),
				xcontext.WithUserConfig(ep),
			)

			next.ServeHTTP(wri, req.WithContext(ctx))
		}

		return http.HandlerFunc(fn)
	}
}
