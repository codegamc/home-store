package config

import "testing"

func TestValidateRequiresSecureCredentials(t *testing.T) {
	base := Config{DataDir: "/data", AccessKey: "access", SecretKey: "secret", Region: "us-east-1", InsecureHTTP: true}
	if err := base.Validate(); err != nil {
		t.Fatalf("valid development config: %v", err)
	}
	missingCredentials := base
	missingCredentials.SecretKey = ""
	if err := missingCredentials.Validate(); err == nil {
		t.Fatal("missing credentials were accepted")
	}
	missingTLS := base
	missingTLS.InsecureHTTP = false
	if err := missingTLS.Validate(); err == nil {
		t.Fatal("plaintext production config was accepted")
	}
	partialTLS := base
	partialTLS.InsecureHTTP = false
	partialTLS.TLSCertFile = "cert.pem"
	if err := partialTLS.Validate(); err == nil {
		t.Fatal("partial TLS configuration was accepted")
	}
	secure := base
	secure.InsecureHTTP = false
	secure.TLSCertFile = "cert.pem"
	secure.TLSKeyFile = "key.pem"
	if err := secure.Validate(); err != nil {
		t.Fatalf("complete TLS configuration: %v", err)
	}
}
