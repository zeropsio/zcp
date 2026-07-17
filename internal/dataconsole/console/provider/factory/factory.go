// Package factory is the single provider-construction choke point: it turns
// a typed connection descriptor + the process's armed-writes posture into a
// provider.Provider. Both production (cmd/zcp/studio_console.go's
// per-family factories) and the conformance harness
// (internal/dataconsole/console/provider/conformance) route through this ONE
// function, so a provider's construction can never drift between what ships
// and what the e2e suite proves against a live service.
//
// This package is composition/reuse ONLY — the constructors themselves
// remain the ultimate posture owners. tabular.New forces ClickHouse
// non-editable (NoEdit, readonly=1 DSN) regardless of armed; document.New
// forces qdrant read-only regardless of armed; stream.New has no posture
// concept at all (messaging is read-only by nature). New never re-decides
// any of that — it only routes each descriptor to its one real constructor,
// uniformly.
package factory

import (
	"fmt"
	"net"

	"github.com/zeropsio/zcp/internal/dataconsole/console/provider"
	"github.com/zeropsio/zcp/internal/dataconsole/console/provider/document"
	"github.com/zeropsio/zcp/internal/dataconsole/console/provider/kv"
	"github.com/zeropsio/zcp/internal/dataconsole/console/provider/object"
	"github.com/zeropsio/zcp/internal/dataconsole/console/provider/stream"
	"github.com/zeropsio/zcp/internal/dataconsole/console/provider/tabular"
)

// New builds the provider for desc. armed is the write posture — true iff
// the launch ceiling permits arming (safety.Policy.ArmingPermitted()); each
// constructor turns that into ReadOnly: !armed, except StreamConn (messaging
// carries no write posture at all — armed is accepted for signature
// symmetry and ignored). desc must be one of the ConnectionDescriptor
// concrete types this package classifies (SQLConn, KVConn, ObjectConn,
// DocumentConn, StreamConn); any other value — including nil — is a caller
// bug reported as provider.ErrInvalid, never a nil-provider panic.
func New(desc provider.ConnectionDescriptor, armed bool) (provider.Provider, error) {
	switch d := desc.(type) {
	case provider.SQLConn:
		return tabular.New(tabular.Config{Conn: d, ReadOnly: !armed})
	case provider.KVConn:
		return kv.New(kv.Config{Addr: net.JoinHostPort(d.Host, d.Port), Password: d.Password, ReadOnly: !armed})
	case provider.ObjectConn:
		return object.New(object.Config{
			Endpoint:  d.Endpoint,
			AccessKey: d.AccessKey,
			SecretKey: d.SecretKey,
			Bucket:    d.Bucket,
			Secure:    d.Secure,
			ReadOnly:  !armed,
		})
	case provider.DocumentConn:
		return document.New(document.Config{
			Engine:   d.Engine,
			BaseURL:  d.BaseURL,
			User:     d.User,
			APIKey:   d.APIKey,
			ReadOnly: !armed,
		})
	case provider.StreamConn:
		return stream.New(stream.Config{
			Engine:   d.Engine,
			Addr:     net.JoinHostPort(d.Host, d.Port),
			User:     d.User,
			Password: d.Password,
		})
	default:
		return nil, fmt.Errorf("factory: unhandled connection descriptor %T: %w", desc, provider.ErrInvalid)
	}
}
