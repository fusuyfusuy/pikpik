package api

import "context"

// ContextWithRole injects a role into ctx the same way AuthMiddleware does.
// Exported for external tests (package api_test) that need to simulate an
// authenticated request context without exercising the full auth stack.
func ContextWithRole(ctx context.Context, role string) context.Context {
	return context.WithValue(ctx, contextKeyRole, role)
}
