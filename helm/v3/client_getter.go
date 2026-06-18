package helm

import (
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

type RESTClientGetter struct {
	namespace     string
	kubeConfig    []byte
	restConfig    *rest.Config
	cachedClients *CachedClients

	opts []RESTClientOption
}

// RESTClientOption is a function that can be used to set the RESTClientOptions of a HelmClient.
type RESTClientOption func(*rest.Config)

// Timeout specifies the timeout for a RESTClient as a RESTClientOption.
// The default (if unspecified) is 32 seconds.
// See [1] for reference.
// [^1]: https://github.com/kubernetes/client-go/blob/c6bd30b9ec5f668df191bc268c6f550c37726edb/discovery/discovery_client.go#L52
func Timeout(d time.Duration) RESTClientOption {
	return func(r *rest.Config) {
		r.Timeout = d
	}
}

// NewRESTClientGetter returns a RESTClientGetter using the provided 'namespace', 'kubeConfig' and 'restConfig'.
//
// source: https://github.com/helm/helm/issues/6910#issuecomment-601277026
func NewRESTClientGetter(namespace string, kubeConfig []byte, restConfig *rest.Config, opts ...RESTClientOption) *RESTClientGetter {
	return &RESTClientGetter{
		namespace:  namespace,
		kubeConfig: kubeConfig,
		restConfig: restConfig,
		opts:       opts,
	}
}

// NewRESTClientGetterWithCachedClients returns a RESTClientGetter that reuses the provided
// cached discovery and mapper clients when they are available.
func NewRESTClientGetterWithCachedClients(namespace string, kubeConfig []byte, restConfig *rest.Config, cachedClients *CachedClients, opts ...RESTClientOption) *RESTClientGetter {
	return &RESTClientGetter{
		namespace:     namespace,
		kubeConfig:    kubeConfig,
		restConfig:    restConfig,
		cachedClients: cachedClients,
		opts:          opts,
	}
}

// ToRESTConfig returns a REST config build from a given kubeconfig
func (c *RESTClientGetter) ToRESTConfig() (*rest.Config, error) {
	if c.restConfig != nil {
		return c.restConfig, nil
	}

	return clientcmd.RESTConfigFromKubeConfig(c.kubeConfig)

}

// ToDiscoveryClient returns a CachedDiscoveryInterface that can be used as a discovery client.
func (c *RESTClientGetter) ToDiscoveryClient() (discovery.CachedDiscoveryInterface, error) {
	if c.cachedClients != nil && c.cachedClients.discoveryClient != nil {
		return c.cachedClients.discoveryClient, nil
	}

	config, err := c.ToRESTConfig()
	if err != nil {
		return nil, err
	}

	for _, fn := range c.opts {
		fn(config)
	}

	discoveryClient, _ := discovery.NewDiscoveryClientForConfig(config)
	return memory.NewMemCacheClient(discoveryClient), nil
}

func (c *RESTClientGetter) ToRESTMapper() (meta.RESTMapper, error) {
	if c.cachedClients != nil && c.cachedClients.mapper != nil {
		expander := restmapper.NewShortcutExpander(c.cachedClients.mapper, c.cachedClients.discoveryClient, nil)
		return expander, nil
	}

	discoveryClient, err := c.ToDiscoveryClient()
	if err != nil {
		return nil, err
	}

	mapper := restmapper.NewDeferredDiscoveryRESTMapper(discoveryClient)
	expander := restmapper.NewShortcutExpander(mapper, discoveryClient, nil)
	return expander, nil
}

func (c *RESTClientGetter) ToRawKubeConfigLoader() clientcmd.ClientConfig {
	// Load the configuration from the bytes provided in the struct.
	config, err := clientcmd.Load(c.kubeConfig)
	if err != nil {
		// Since the interface signature does not allow returning an error,
		// we return an empty config which will likely fail on subsequent calls.
		config = clientcmdapi.NewConfig()
	}

	overrides := &clientcmd.ConfigOverrides{ClusterDefaults: clientcmd.ClusterDefaults}
	overrides.Context.Namespace = c.namespace

	// NewDefaultClientConfig creates a client config from the in-memory config object
	// and the specific overrides (like the namespace).
	return clientcmd.NewDefaultClientConfig(*config, overrides)
}

// CachedClients holds cached discovery and mapper clients for improved performance
// when watching for CRD changes.
type CachedClients struct {
	mapper          *restmapper.DeferredDiscoveryRESTMapper
	discoveryClient discovery.CachedDiscoveryInterface
}

// NewCachedClients creates a new CachedClients instance from a REST config.
func NewCachedClients(cfg *rest.Config) (CachedClients, error) {
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return CachedClients{}, err
	}
	cachedDiscovery := memory.NewMemCacheClient(discoveryClient)
	mapper := restmapper.NewDeferredDiscoveryRESTMapper(cachedDiscovery)

	return CachedClients{
		mapper:          mapper,
		discoveryClient: cachedDiscovery,
	}, nil
}
