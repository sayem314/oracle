package auth

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"

	"github.com/thecodearcher/limen"
	sqladapter "github.com/thecodearcher/limen/adapters/sql"
	credentialpassword "github.com/thecodearcher/limen/plugins/credential-password"
)

const (
	usersTable         = limen.SchemaTableName("auth_users")
	sessionsTable      = limen.SchemaTableName("auth_sessions")
	verificationsTable = limen.SchemaTableName("auth_verifications")
)

const (
	roleAdmin = "admin"
	roleUser  = "user"

	roleField = "role"
)

// Auth is the seam oracle depends on. Limen is the only implementation for
// now, but keeping it behind an interface lets tests swap in fakes.
type Auth interface {
	Handler() http.Handler
	UserID(r *http.Request) (int64, error)
	HasUsers(ctx context.Context) (bool, error)
}

type limenAuth struct {
	l  *limen.Limen
	db *sql.DB
}

var _ Auth = (*limenAuth)(nil)

// Options configures the Limen-backed authenticator.
type Options struct {
	Secret []byte
	// CookieSecure sets the Secure attribute on the session cookie. Browsers
	// reject Secure cookies over plain HTTP, so leave it false for local dev
	// and enable it once the app is served behind TLS.
	CookieSecure bool
}

func New(dbConn *sql.DB, opts Options) (Auth, error) {
	a := &limenAuth{db: dbConn}

	schema := limen.NewDefaultSchemaConfig(
		limen.WithSchemaUser(
			limen.WithUserTableName(usersTable),
			limen.WithUserAdditionalFields(a.additionalUserFields),
		),
		limen.WithSchemaSession(
			limen.WithSessionTableName(sessionsTable),
		),
		limen.WithSchemaVerification(
			limen.WithVerificationTableName(verificationsTable),
		),
	)

	cfg := &limen.Config{
		Database: sqladapter.NewSQLite(dbConn),
		Secret:   opts.Secret,
		Schema:   schema,
		Session:  limen.NewDefaultSessionConfig(limen.WithBearerEnabled()),
		Email: limen.NewDefaultEmailConfig(
			limen.WithEmailVerification(limen.WithDisableEmailVerification()),
		),
		HTTP: limen.NewDefaultHTTPConfig(
			limen.WithHTTPCookieSecure(opts.CookieSecure),
		),
		Plugins: []limen.Plugin{credentialpassword.New()},
	}

	l, err := limen.New(cfg)
	if err != nil {
		return nil, fmt.Errorf("init limen: %w", err)
	}
	a.l = l

	return a, nil
}

func (a *limenAuth) Handler() http.Handler {
	return a.l.Handler()
}

func (a *limenAuth) UserID(r *http.Request) (int64, error) {
	session, err := a.l.GetSession(r)
	if err != nil {
		return 0, fmt.Errorf("validate session: %w", err)
	}
	if session == nil || session.User == nil {
		return 0, fmt.Errorf("validate session: no user")
	}
	return toInt64(session.User.ID)
}

func (a *limenAuth) HasUsers(ctx context.Context) (bool, error) {
	var count int64
	if err := a.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM auth_users").Scan(&count); err != nil {
		return false, fmt.Errorf("count users: %w", err)
	}
	return count > 0, nil
}

// additionalUserFields stamps a role on every created user. Sign-up is locked
// after the first account, so the users table is empty exactly when the first
// (admin) user is created.
func (a *limenAuth) additionalUserFields(_ *limen.AdditionalFieldsContext) (map[string]any, error) {
	hasUsers, err := a.HasUsers(context.Background())
	if err != nil {
		return nil, err
	}
	role := roleUser
	if !hasUsers {
		role = roleAdmin
	}
	return map[string]any{roleField: role}, nil
}

func toInt64(v any) (int64, error) {
	switch n := v.(type) {
	case int64:
		return n, nil
	case int:
		return int64(n), nil
	default:
		return 0, fmt.Errorf("unexpected user id type %T", v)
	}
}
