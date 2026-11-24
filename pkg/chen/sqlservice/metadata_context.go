package sqlservice

import "context"

type consoleCatalogLifetime struct{}
type catalogRequestLifetime struct{}

// WithCatalogRequestLifetime lets resource-tree queries share the HTTP request's
// deadline and cancellation across metadata lookups.
func WithCatalogRequestLifetime(ctx context.Context) context.Context {
	return context.WithValue(ctx, catalogRequestLifetime{}, true)
}

// WithConsoleCatalogLifetime lets MySQL finish a statement on the pinned console
// when the AI request expires. The engine interrupts the query separately.
func WithConsoleCatalogLifetime(request, console context.Context) context.Context {
	return context.WithValue(request, consoleCatalogLifetime{}, console)
}

// CatalogQueryContext checks the request before dispatch. Once dispatched,
// canceling a MySQL driver context would discard the console and its transaction.
func CatalogQueryContext(ctx context.Context) context.Context {
	if lifetime, ok := ctx.Value(consoleCatalogLifetime{}).(context.Context); ok && ctx.Err() == nil {
		return lifetime
	}
	return ctx
}

func catalogReadContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx.Value(catalogRequestLifetime{}) != nil || ctx.Value(consoleCatalogLifetime{}) != nil {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, catalogTimeout)
}

// MetadataContext bounds metadata work without shortening an existing operation
// deadline for drivers whose cancellation discards the physical connection.
// Table metadata requires several network round trips; expiring a separate
// three-second budget can otherwise destroy a console after a successful read.
// Callers without an operation deadline retain the standard catalog bound.
func MetadataContext(ctx context.Context, dialect Dialect) (context.Context, context.CancelFunc) {
	if dialect == Dameng || dialect == DB2 {
		if _, bounded := ctx.Deadline(); bounded {
			return context.WithCancel(ctx)
		}
	}
	return catalogReadContext(ctx)
}
