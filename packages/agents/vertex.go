package agents

import (
	"context"
	"errors"
	"sync"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// DefaultVertexModel is the model used when drafting through Vertex AI.
const DefaultVertexModel = "google/gemini-3.5-flash"

// VertexMaxTokens leaves room for the model to think and still finish the JSON.
const VertexMaxTokens = 4096

// VertexBaseURL is Vertex AI's OpenAI-compatible endpoint for a project. The
// Gateway drafter speaks that dialect, so Vertex needs no new client: only a
// base URL and a bearer that is a Google access token.
func VertexBaseURL(project string) string {
	return "https://aiplatform.googleapis.com/v1/projects/" + project + "/locations/global/endpoints/openapi"
}

var (
	vertexOnce   sync.Once
	vertexSource oauth2.TokenSource
	vertexErr    error
)

// VertexToken returns an access token from Application Default Credentials. On
// Cloud Run that is the service's own service account, so no key is stored or
// configured; the token source refreshes itself and is shared across requests.
//
// Hosted use is the point of this: unlike a user's Aria Code login, a service
// account identity belongs to the service.
func VertexToken() (string, error) {
	vertexOnce.Do(func() {
		vertexSource, vertexErr = google.DefaultTokenSource(context.Background(), "https://www.googleapis.com/auth/cloud-platform")
	})
	if vertexErr != nil {
		return "", errors.New("Google credentials are not available for Vertex AI")
	}
	token, err := vertexSource.Token()
	if err != nil {
		return "", errors.New("could not obtain a Google access token for Vertex AI")
	}
	return token.AccessToken, nil
}
