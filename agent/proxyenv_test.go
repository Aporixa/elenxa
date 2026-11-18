package agent

import (
	"strings"
	"testing"
)

func TestProxyEnvEmptyIsAllowlistedBase(t *testing.T) {
	t.Setenv("ELENXA_TEST_SECRET", "s3cret")
	t.Setenv("PATH", "/usr/bin:/bin")
	env := proxyEnv("", "")
	if env == nil {
		t.Fatal("proxyEnv(\"\", \"\") = nil, want the complete allowlisted env")
	}
	var sawPath bool
	for _, e := range env {
		if strings.HasPrefix(e, "ELENXA_TEST_SECRET=") {
			t.Fatalf("non-allowlisted variable leaked to child env: %v", env)
		}
		if e == "PATH=/usr/bin:/bin" {
			sawPath = true
		}
	}
	if !sawPath {
		t.Fatalf("PATH missing from child env: %v", env)
	}
}

func TestSafeEnvOverridesInheritedProxy(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://old:1")
	env := SafeEnv("HTTPS_PROXY=http://new:2")
	n := 0
	for _, e := range env {
		if strings.HasPrefix(e, "HTTPS_PROXY=") {
			n++
			if e != "HTTPS_PROXY=http://new:2" {
				t.Fatalf("inherited proxy not overridden: %v", env)
			}
		}
	}
	if n != 1 {
		t.Fatalf("want exactly one HTTPS_PROXY, got %d: %v", n, env)
	}
}

func TestProxyEnvSetsAllProxyForSocks5(t *testing.T) {
	// Capture-off egress path: a socks5 proxy, no MITM CA. ALL_PROXY must be set
	// (curl reads socks5 only from there), and no CA vars should appear.
	env := proxyEnv("socks5://10.0.0.1:1080", "")
	has := func(prefix string) bool {
		for _, e := range env {
			if strings.HasPrefix(e, prefix) {
				return true
			}
		}
		return false
	}
	for _, want := range []string{"HTTP_PROXY=", "HTTPS_PROXY=", "ALL_PROXY=", "all_proxy="} {
		if !has(want) {
			t.Errorf("proxyEnv missing %s: %v", want, env)
		}
	}
	if has("SSL_CERT_FILE=") || has("CURL_CA_BUNDLE=") {
		t.Errorf("proxyEnv without CA must not inject CA vars: %v", env)
	}
}

func TestProxyEnvInjectsCAWhenRecording(t *testing.T) {
	env := proxyEnv("http://127.0.0.1:8788", "/data/ca.pem")
	has := func(prefix string) bool {
		for _, e := range env {
			if strings.HasPrefix(e, prefix) {
				return true
			}
		}
		return false
	}
	for _, want := range []string{"SSL_CERT_FILE=", "CURL_CA_BUNDLE=", "REQUESTS_CA_BUNDLE=", "NODE_EXTRA_CA_CERTS="} {
		if !has(want) {
			t.Errorf("proxyEnv with CA missing %s: %v", want, env)
		}
	}
}
