package use

import (
	"net/http"

	"github.com/krateo-platformops/plumbing/server/use/cors"
)

func CORS(opts cors.Options) func(http.Handler) http.Handler {
	c := cors.New(opts)
	return c.Handler
}
