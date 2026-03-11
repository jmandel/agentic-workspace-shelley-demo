package manager

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/golang-jwt/jwt/v5"
)

const managerWebSocketAuthTimeout = 5 * time.Second

const (
	managerRuntimeSubject     = "workspace-manager"
	managerRuntimeDisplayName = "Workspace Manager"
)

type tokenValidator interface {
	ValidateToken(context.Context, string) (requestPrincipal, error)
}

type requestPrincipal struct {
	Subject     string
	DisplayName string
	Email       string
}

type requestPrincipalContextKey struct{}

type jwtClaims struct {
	Name              string `json:"name,omitempty"`
	PreferredUsername string `json:"preferred_username,omitempty"`
	Email             string `json:"email,omitempty"`
	jwt.RegisteredClaims
}

type noneJWTTokenValidator struct{}

func (noneJWTTokenValidator) ValidateToken(_ context.Context, raw string) (requestPrincipal, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return requestPrincipal{}, errors.New("missing token")
	}

	claims := jwtClaims{}
	token, err := jwt.ParseWithClaims(raw, &claims, func(token *jwt.Token) (any, error) {
		if token.Method.Alg() != jwt.SigningMethodNone.Alg() {
			return nil, errors.New("unsupported jwt signing algorithm")
		}
		return jwt.UnsafeAllowNoneSignatureType, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodNone.Alg()}))
	if err != nil {
		return requestPrincipal{}, err
	}
	if !token.Valid {
		return requestPrincipal{}, errors.New("invalid token")
	}
	if strings.TrimSpace(claims.Subject) == "" {
		return requestPrincipal{}, errors.New("jwt subject is required")
	}

	return requestPrincipal{
		Subject:     strings.TrimSpace(claims.Subject),
		DisplayName: firstNonEmpty(strings.TrimSpace(claims.Name), strings.TrimSpace(claims.PreferredUsername), strings.TrimSpace(claims.Email), strings.TrimSpace(claims.Subject)),
		Email:       strings.TrimSpace(claims.Email),
	}, nil
}

func (m *Manager) authenticateRequest(r *http.Request) (requestPrincipal, bool, error) {
	token := extractBearerToken(r)
	if token == "" {
		return requestPrincipal{}, false, nil
	}
	principal, err := m.tokenValidator.ValidateToken(r.Context(), token)
	if err != nil {
		return requestPrincipal{}, false, err
	}
	return principal, true, nil
}

func withRequestPrincipal(r *http.Request, principal requestPrincipal) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), requestPrincipalContextKey{}, principal))
}

func requestPrincipalFromContext(ctx context.Context) (requestPrincipal, bool) {
	principal, ok := ctx.Value(requestPrincipalContextKey{}).(requestPrincipal)
	return principal, ok
}

func managerRuntimePrincipal(ctx context.Context) requestPrincipal {
	if principal, ok := requestPrincipalFromContext(ctx); ok {
		return principal
	}
	return requestPrincipal{
		Subject:     managerRuntimeSubject,
		DisplayName: managerRuntimeDisplayName,
	}
}

func applyWorkspaceRuntimeIdentity(req *http.Request, ctx context.Context) {
	principal := managerRuntimePrincipal(ctx)
	req.Header.Del("Authorization")
	req.Header.Set(workspaceHeaderSubject, principal.Subject)
	req.Header.Set(workspaceHeaderDisplayName, principal.DisplayName)
}

func extractBearerToken(r *http.Request) string {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if value, ok := strings.CutPrefix(auth, "Bearer "); ok {
		return strings.TrimSpace(value)
	}
	if value, ok := strings.CutPrefix(auth, "bearer "); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func (m *Manager) authenticateWebSocket(ctx context.Context, conn *websocket.Conn, r *http.Request) (requestPrincipal, error) {
	if principal, ok, err := m.authenticateRequest(r); err != nil {
		return requestPrincipal{}, managerWebSocketAuthError(ctx, conn, "invalid authorization token")
	} else if ok {
		if err := managerWriteAuthenticated(ctx, conn, principal); err != nil {
			return requestPrincipal{}, err
		}
		return principal, nil
	}

	readCtx, cancel := context.WithTimeout(ctx, managerWebSocketAuthTimeout)
	defer cancel()

	var authMessage struct {
		Type  string `json:"type"`
		Token string `json:"token,omitempty"`
	}
	if err := wsjson.Read(readCtx, conn, &authMessage); err != nil {
		if ctx.Err() != nil {
			return requestPrincipal{}, ctx.Err()
		}
		return requestPrincipal{}, managerWebSocketAuthError(ctx, conn, "authorization required")
	}
	if authMessage.Type != "authenticate" {
		return requestPrincipal{}, managerWebSocketAuthError(ctx, conn, "authenticate must be the first websocket message")
	}

	principal, err := m.tokenValidator.ValidateToken(ctx, strings.TrimSpace(authMessage.Token))
	if err != nil {
		return requestPrincipal{}, managerWebSocketAuthError(ctx, conn, "invalid authorization token")
	}
	if err := managerWriteAuthenticated(ctx, conn, principal); err != nil {
		return requestPrincipal{}, err
	}
	return principal, nil
}

func managerWriteAuthenticated(ctx context.Context, conn *websocket.Conn, principal requestPrincipal) error {
	return wsjson.Write(ctx, conn, map[string]any{
		"type": "authenticated",
		"actor": map[string]string{
			"id":          principal.Subject,
			"displayName": principal.DisplayName,
		},
	})
}

func managerWebSocketAuthError(ctx context.Context, conn *websocket.Conn, message string) error {
	_ = wsjson.Write(ctx, conn, map[string]any{
		"type": "error",
		"data": message,
	})
	_ = conn.Close(websocket.StatusPolicyViolation, message)
	return errors.New(message)
}
