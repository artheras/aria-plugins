package accounts

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

type Login struct {
	Access   *Access
	OAuth    oauth2.Config
	Verifier *oidc.IDTokenVerifier
}

func NewLogin(ctx context.Context, a *Access, clientID, clientSecret, publicURL string) (*Login, error) {
	origin, err := url.Parse(publicURL)
	if err != nil {
		return nil, errors.New("invalid account origin")
	}
	local := origin.Scheme == "http" && (origin.Hostname() == "localhost" || origin.Hostname() == "127.0.0.1")
	secure := func(s string) bool {
		u, e := url.Parse(s)
		return e == nil && u.Host != "" && u.User == nil && (u.Scheme == "https" || local && u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1"))
	}
	if a == nil || clientID == "" || clientSecret == "" || !secure(a.Issuer) {
		return nil, errors.New("configure HTTPS OIDC issuer, client ID and client secret")
	}
	ctx = oidc.ClientContext(ctx, &http.Client{Timeout: 10 * time.Second})
	provider, err := oidc.NewProvider(ctx, a.Issuer)
	if err != nil {
		return nil, errors.New("OIDC discovery unavailable")
	}
	endpoint := provider.Endpoint()
	var discovery struct {
		JWKS string `json:"jwks_uri"`
	}
	if provider.Claims(&discovery) != nil || !secure(endpoint.AuthURL) || !secure(endpoint.TokenURL) || !secure(discovery.JWKS) {
		return nil, errors.New("OIDC endpoints must use HTTPS")
	}
	return &Login{Access: a, OAuth: oauth2.Config{ClientID: clientID, ClientSecret: clientSecret, Endpoint: endpoint, RedirectURL: strings.TrimRight(publicURL, "/") + "/api/auth/callback", Scopes: []string{oidc.ScopeOpenID}}, Verifier: provider.Verifier(&oidc.Config{ClientID: clientID, SupportedSigningAlgs: []string{"RS256"}})}, nil
}
func (l *Login) Exchange(ctx context.Context, code, verifier, nonce string) (Principal, time.Duration, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	ctx = oidc.ClientContext(ctx, client)
	ctx = context.WithValue(ctx, oauth2.HTTPClient, client)
	token, err := l.OAuth.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return Principal{}, 0, ErrDenied
	}
	raw, ok := token.Extra("id_token").(string)
	if !ok {
		return Principal{}, 0, ErrDenied
	}
	id, err := l.Verifier.Verify(ctx, raw)
	if err != nil || id.Nonce != nonce || nonce == "" || id.Subject == "" || len(id.Subject) > 255 {
		return Principal{}, 0, ErrDenied
	}
	var claims struct {
		AuthorizedParty string `json:"azp"`
	}
	if id.Claims(&claims) != nil || (len(id.Audience) > 1 || claims.AuthorizedParty != "") && claims.AuthorizedParty != l.OAuth.ClientID {
		return Principal{}, 0, ErrDenied
	}
	p, err := l.Access.Subject(id.Subject)
	if err != nil {
		return Principal{}, 0, err
	}
	ttl := min(time.Until(id.Expiry), 8*time.Hour)
	if ttl <= 0 {
		return Principal{}, 0, ErrDenied
	}
	return p, ttl, nil
}
