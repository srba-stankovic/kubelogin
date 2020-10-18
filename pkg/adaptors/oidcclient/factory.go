// Package oidcclient provides a client of OpenID Connect.
package oidcclient

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"

	gooidc "github.com/coreos/go-oidc"
	"github.com/google/wire"
	"github.com/int128/kubelogin/pkg/adaptors/clock"
	"github.com/int128/kubelogin/pkg/adaptors/logger"
	"github.com/int128/kubelogin/pkg/adaptors/oidcclient/logging"
	"github.com/int128/kubelogin/pkg/oidc"
	"golang.org/x/oauth2"
	"golang.org/x/xerrors"
)

var Set = wire.NewSet(
	wire.Struct(new(Factory), "*"),
	wire.Bind(new(FactoryInterface), new(*Factory)),
)

type FactoryInterface interface {
	New(ctx context.Context, p oidc.Provider) (Interface, error)
}

type Factory struct {
	Clock  clock.Interface
	Logger logger.Interface
}

// New returns an instance of adaptors.Interface with the given configuration.
func (f *Factory) New(ctx context.Context, p oidc.Provider) (Interface, error) {
	var tlsConfig tls.Config
	tlsConfig.InsecureSkipVerify = p.SkipTLSVerify
	tlsConfig.Renegotiation = tls.RenegotiateFreelyAsClient
	if p.CertPool != nil {
		p.CertPool.SetRootCAs(&tlsConfig)
	}
	baseTransport := &http.Transport{
		TLSClientConfig: &tlsConfig,
		Proxy:           http.ProxyFromEnvironment,
	}
	loggingTransport := &logging.Transport{
		Base:   baseTransport,
		Logger: f.Logger,
	}
	httpClient := &http.Client{
		Transport: loggingTransport,
	}

	ctx = context.WithValue(ctx, oauth2.HTTPClient, httpClient)
	provider, err := gooidc.NewProvider(ctx, p.IssuerURL)
	if err != nil {
		return nil, xerrors.Errorf("oidc discovery error: %w", err)
	}
	supportedPKCEMethods, err := extractSupportedPKCEMethods(provider)
	if err != nil {
		return nil, xerrors.Errorf("could not determine supported PKCE methods: %w", err)
	}
	return &client{
		httpClient: httpClient,
		provider:   provider,
		oauth2Config: oauth2.Config{
			Endpoint:     provider.Endpoint(),
			ClientID:     p.ClientID,
			ClientSecret: p.ClientSecret,
			Scopes:       append(p.ExtraScopes, gooidc.ScopeOpenID),
		},
		clock:                f.Clock,
		logger:               f.Logger,
		supportedPKCEMethods: supportedPKCEMethods,
	}, nil
}

func extractSupportedPKCEMethods(provider *gooidc.Provider) ([]string, error) {
	var d struct {
		CodeChallengeMethodsSupported []string `json:"code_challenge_methods_supported"`
	}
	if err := provider.Claims(&d); err != nil {
		return nil, fmt.Errorf("invalid discovery document: %w", err)
	}
	return d.CodeChallengeMethodsSupported, nil
}
