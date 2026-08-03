package kubeconfig

import (
	"encoding/json"
	"testing"

	"github.com/krateo-platformops/plumbing/endpoints"
)

func TestMarshal(t *testing.T) {
	tests := []struct {
		name      string
		endpoint  *endpoints.Endpoint
		expectErr bool
	}{
		{
			name: "Valid Endpoint",
			endpoint: &endpoints.Endpoint{
				ServerURL:                "https://kubernetes.example.com",
				Username:                 "admin",
				CertificateAuthorityData: "ca-data",
				ClientCertificateData:    "cert-data",
				ClientKeyData:            "key-data",
			},
			expectErr: false,
		},
		{
			name: "Missing Certificate Authority Data",
			endpoint: &endpoints.Endpoint{
				ServerURL:                "https://kubernetes.example.com",
				Username:                 "admin",
				CertificateAuthorityData: "",
				ClientCertificateData:    "cert-data",
				ClientKeyData:            "key-data",
			},
			expectErr: false,
		},
		{
			name: "Empty Endpoint",
			endpoint: &endpoints.Endpoint{
				ServerURL:                "",
				Username:                 "",
				CertificateAuthorityData: "",
				ClientCertificateData:    "",
				ClientKeyData:            "",
			},
			expectErr: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data, err := Marshal(tc.endpoint)

			if tc.expectErr {
				if err == nil {
					t.Errorf("expected an error but got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			// Verifica che l'output sia JSON valido
			var parsed map[string]interface{}
			if err := json.Unmarshal(data, &parsed); err != nil {
				t.Errorf("output is not valid JSON: %v", err)
			}
		})
	}
}
