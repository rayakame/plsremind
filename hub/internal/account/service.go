package account

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/rayakame/plsremind/hub/internal/api"
	"github.com/rayakame/plsremind/hub/internal/core"
	"github.com/rayakame/plsremind/hub/internal/store"
	"github.com/rayakame/plsremind/hub/internal/store/sqlc"
)

const PasswordProvider = "password"

var usernamePattern = regexp.MustCompile(`^[a-z0-9_.-]{3,32}$`)

type NewUser struct {
	DisplayName string
	Timezone    string
	Role        string
	Locale      *string
}

// CreateUser inserts a user together with their inbox list.
func CreateUser(ctx context.Context, q *sqlc.Queries, now time.Time, p NewUser) (sqlc.User, error) {
	if p.Role == "" {
		p.Role = "user"
	}
	if p.Timezone == "" {
		p.Timezone = "UTC"
	}
	user, err := q.CreateUser(ctx, sqlc.CreateUserParams{
		ID:          uuid.Must(uuid.NewV7()),
		DisplayName: p.DisplayName,
		Role:        p.Role,
		Timezone:    p.Timezone,
		Locale:      p.Locale,
		CreatedAt:   now,
	})
	if err != nil {
		return sqlc.User{}, err
	}
	_, err = q.CreateList(ctx, sqlc.CreateListParams{
		ID:      uuid.Must(uuid.NewV7()),
		UserID:  user.ID,
		Name:    "Inbox",
		IsInbox: true,
		Now:     now,
	})
	return user, err
}

// AddPassword gives the user a username and password to log in with.
func AddPassword(ctx context.Context, q *sqlc.Queries, now time.Time, userID uuid.UUID, username, password string) error {
	hash, err := core.HashPassword(password)
	if err != nil {
		return err
	}
	_, err = q.CreateIdentity(ctx, sqlc.CreateIdentityParams{
		ID:           uuid.Must(uuid.NewV7()),
		UserID:       userID,
		Provider:     PasswordProvider,
		ExternalID:   username,
		DisplayName:  username,
		PasswordHash: &hash,
		CreatedAt:    now,
	})
	switch {
	case store.ViolatedConstraint(err) == "identities_one_password_idx":
		return api.Conflict("password_exists", "This account already has a password.")
	case store.IsUniqueViolation(err):
		return api.Conflict("username_taken", "This username is already in use.")
	}
	return err
}

func NormalizeUsername(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func checkUsername(fields map[string]string, username string) {
	if !usernamePattern.MatchString(username) {
		fields["username"] = "Must be 3 to 32 characters: a-z, 0-9, underscore, dot or dash."
	}
}

func checkPassword(fields map[string]string, field, password string) {
	if n := utf8.RuneCountInString(password); n < 8 || n > 256 {
		fields[field] = "Must be 8 to 256 characters."
	}
}

func checkDisplayName(fields map[string]string, name string) {
	if n := utf8.RuneCountInString(name); n < 1 || n > 64 {
		fields["display_name"] = "Must be 1 to 64 characters."
	}
}

// ValidateTimezone accepts IANA time zone names.
func ValidateTimezone(tz string) error {
	if tz == "" || tz == "Local" {
		return api.Field("timezone", "Must be an IANA time zone name such as Europe/Berlin.")
	}
	if _, err := time.LoadLocation(tz); err != nil {
		return api.Field("timezone", "Must be an IANA time zone name such as Europe/Berlin.")
	}
	return nil
}

// checkTimezone requires a name that both Go and the database know: the database
// computes due times with it.
var localePattern = regexp.MustCompile(`^[A-Za-z]{2,8}(-[A-Za-z0-9]{1,8})*$`)

const maxLocaleLen = 35

// checkLocale accepts anything shaped like a language tag. Which languages a
// client can actually speak is the client's business.
func checkLocale(fields map[string]string, locale string) {
	if len(locale) > maxLocaleLen || !localePattern.MatchString(locale) {
		fields["locale"] = `Must be a language tag such as "de" or "en-GB".`
	}
}

const maxPreferencesLen = 16 << 10

func checkPreferences(fields map[string]string, raw json.RawMessage) {
	var object map[string]json.RawMessage
	if len(raw) > maxPreferencesLen || json.Unmarshal(raw, &object) != nil || object == nil {
		fields["preferences"] = "Must be a JSON object of at most 16 kB."
	}
}

func checkTimezone(ctx context.Context, q *sqlc.Queries, fields map[string]string, tz string) {
	err := ValidateTimezone(tz)
	if err == nil {
		_, err = q.ProbeTimezone(ctx, tz)
	}
	if err != nil {
		fields["timezone"] = "Must be an IANA time zone name such as Europe/Berlin."
	}
}

// dummyHash keeps login timing the same for unknown usernames.
var dummyHash, _ = core.HashPassword("plsremind-dummy-password")

// Authenticate returns the user for a username and password, or invalid_credentials.
func Authenticate(ctx context.Context, q *sqlc.Queries, username, password string) (sqlc.User, error) {
	invalid := api.NewError(401, "invalid_credentials", "Wrong username or password.")
	identity, err := q.GetIdentity(ctx, sqlc.GetIdentityParams{Provider: PasswordProvider, ExternalID: NormalizeUsername(username)})
	if store.IsNotFound(err) {
		core.VerifyPassword(dummyHash, password)
		return sqlc.User{}, invalid
	}
	if err != nil {
		return sqlc.User{}, err
	}
	if identity.PasswordHash == nil || !core.VerifyPassword(*identity.PasswordHash, password) {
		return sqlc.User{}, invalid
	}
	return q.GetUser(ctx, identity.UserID)
}
