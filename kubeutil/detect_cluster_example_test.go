package kubeutil_test

import (
	"fmt"
	"os"

	"github.com/krateoplatformops/plumbing/kubeutil"
	"k8s.io/client-go/rest"
)

func ExampleDetectClusterName_fromEnv() {
	prev := os.Getenv(kubeutil.ClusterNameEnvVar)
	_ = os.Setenv(kubeutil.ClusterNameEnvVar, "dev-eu-1")
	defer func() {
		if prev == "" {
			_ = os.Unsetenv(kubeutil.ClusterNameEnvVar)
			return
		}
		_ = os.Setenv(kubeutil.ClusterNameEnvVar, prev)
	}()

	cluster := kubeutil.DetectClusterName(&rest.Config{Host: "https://10.0.0.1:6443"})
	fmt.Println(cluster)

	// Output:
	// dev-eu-1
}

func ExampleDetectClusterName_fromRestConfig() {
	_ = os.Unsetenv(kubeutil.ClusterNameEnvVar)

	cluster := kubeutil.DetectClusterName(&rest.Config{Host: "https://123.45.67.89:6443"})
	fmt.Println(cluster)

	// Output:
	// 123.45.67.89
}
