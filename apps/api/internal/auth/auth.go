package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"time"

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
	RoleAdmin = "admin"
	RoleUser  = "user"

	roleField = "role"
)

// UserInfo is the safe projection of an auth_users row: no password hash.
type UserInfo struct {
	ID        int64
	Email     string
	Role      string
	CreatedAt time.Time
}

// Error is an auth failure that carries the HTTP status it maps to, keeping
// Limen's error types behind the interface.
type Error struct {
	Status  int
	Message string
}

func (e *Error) Error() string { return e.Message }

func toAuthError(err error) error {
	var le *limen.LimenError
	if errors.As(err, &le) {
		status := le.Status()
		if status == http.StatusUnprocessableEntity {
			status = http.StatusBadRequest
		}
		return &Error{Status: status, Message: le.Error()}
	}
	return err
}

// Auth is the seam oracle depends on. Limen is the only implementation for
// now, but keeping it behind an interface lets tests swap in fakes.
type Auth interface {
	Handler() http.Handler
	UserID(r *http.Request) (int64, error)
	HasUsers(ctx context.Context) (bool, error)
	Role(ctx context.Context, userID int64) (string, error)
	CreateUser(ctx context.Context, email, password string) (UserInfo, error)
	ListUsers(ctx context.Context) ([]UserInfo, error)
	ResetPassword(ctx context.Context, userID int64, newPassword string) (UserInfo, error)
	DeleteUser(ctx context.Context, userID int64) error
}

type limenAuth struct {
	l  *limen.Limen
	cp credentialpassword.API
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
	a.cp = credentialpassword.Use(l)

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

func (a *limenAuth) Role(ctx context.Context, userID int64) (string, error) {
	var role string
	err := a.db.QueryRowContext(ctx, "SELECT role FROM auth_users WHERE id = ?", userID).Scan(&role)
	if err != nil {
		return "", fmt.Errorf("read role: %w", err)
	}
	return role, nil
}

// CreateUser registers a user through Limen so the password is hashed and
// validated by the credential-password plugin. The role is passed explicitly
// (and wins over the sign-up hook) so admin-created users are never stamped
// admin.
func (a *limenAuth) CreateUser(ctx context.Context, email, password string) (UserInfo, error) {
	res, err := a.cp.SignUpWithCredentialAndPassword(ctx, &limen.User{
		Email:    email,
		Password: &password,
	}, map[string]any{roleField: RoleUser})
	if err != nil {
		return UserInfo{}, toAuthError(err)
	}
	id, err := toInt64(res.User.ID)
	if err != nil {
		return UserInfo{}, err
	}
	return a.getUser(ctx, id)
}

func (a *limenAuth) ListUsers(ctx context.Context) ([]UserInfo, error) {
	rows, err := a.db.QueryContext(ctx,
		"SELECT id, email, role, created_at FROM auth_users ORDER BY id")
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var users []UserInfo
	for rows.Next() {
		var u UserInfo
		if err := rows.Scan(&u.ID, &u.Email, &u.Role, &u.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	return users, nil
}

// DeleteUser removes the user; sessions, messages, and Limen auth rows cascade.
func (a *limenAuth) DeleteUser(ctx context.Context, userID int64) error {
	res, err := a.db.ExecContext(ctx, "DELETE FROM auth_users WHERE id = ?", userID)
	if err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// ResetPassword sets a new password for a user without knowing the old one:
// the plugin's reset flow generates a token in-process (no email involved,
// verification is disabled) and applies it. Existing sessions are revoked
// because a reset is usually a response to a lost or leaked password.
func (a *limenAuth) ResetPassword(ctx context.Context, userID int64, newPassword string) (UserInfo, error) {
	u, err := a.getUser(ctx, userID)
	if err != nil {
		return UserInfo{}, err
	}

	verification, err := a.cp.RequestPasswordReset(ctx, u.Email)
	if err != nil {
		return UserInfo{}, toAuthError(err)
	}
	if err := a.cp.ResetPassword(ctx, verification.Value, newPassword); err != nil {
		return UserInfo{}, toAuthError(err)
	}

	if _, err := a.db.ExecContext(ctx, "DELETE FROM auth_sessions WHERE user_id = ?", userID); err != nil {
		return UserInfo{}, fmt.Errorf("revoke sessions: %w", err)
	}
	return a.getUser(ctx, userID)
}

func (a *limenAuth) getUser(ctx context.Context, id int64) (UserInfo, error) {
	var u UserInfo
	err := a.db.QueryRowContext(ctx,
		"SELECT id, email, role, created_at FROM auth_users WHERE id = ?", id,
	).Scan(&u.ID, &u.Email, &u.Role, &u.CreatedAt)
	if err != nil {
		return UserInfo{}, fmt.Errorf("read user: %w", err)
	}
	return u, nil
}

// additionalUserFields stamps a role on every created user. Sign-up is locked
// after the first account, so the users table is empty exactly when the first
// (admin) user is created.
func (a *limenAuth) additionalUserFields(_ *limen.AdditionalFieldsContext) (map[string]any, error) {
	hasUsers, err := a.HasUsers(context.Background())
	if err != nil {
		return nil, err
	}
	role := RoleUser
	if !hasUsers {
		role = RoleAdmin
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
