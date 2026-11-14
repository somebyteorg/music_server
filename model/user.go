package model

import (
	"context"
	"time"

	"github.com/deluan/rest"
)

type User struct {
	ID           string     `structs:"id" json:"id"`
	UserName     string     `structs:"user_name" json:"userName"`
	CanDownload  bool       `structs:"is_can_down" db:"is_can_down" json:"canDownload"`
	Name         string     `structs:"name" json:"name"`
	Email        string     `structs:"email" json:"email"`
	IsAdmin      bool       `structs:"is_admin" json:"isAdmin"`
	LastLoginAt  *time.Time `structs:"last_login_at" json:"lastLoginAt"`
	LastAccessAt *time.Time `structs:"last_access_at" json:"lastAccessAt"`
	CreatedAt    time.Time  `structs:"created_at" json:"createdAt"`
	UpdatedAt    time.Time  `structs:"updated_at" json:"updatedAt"`
	// Smart-playlist criteria JSON; matching songs are not sent to external scrobblers
	ScrobbleFilter string `structs:"scrobble_filter" json:"scrobbleFilter"`

	// Library associations (many-to-many relationship)
	Libraries Libraries `structs:"-" json:"libraries,omitempty"`
	// Background jobs operate across libraries independently of a user's selection.
	AllLibraries bool `structs:"-" json:"-"`

	// Plaintext credential used for Subsonic token verification; never serialized.
	Password string `structs:"-" json:"-"`
	// Bumped on password change to invalidate every issued token for this user.
	TokenEpoch int `structs:"-" json:"-"`
	// This is used to set or change a password when calling Put. If it is empty, the password is not changed.
	// It is received from the UI with the name "password"
	NewPassword string `structs:"password,omitempty" json:"password,omitempty"` //nolint:gosec
	// If changing the password, this is also required
	CurrentPassword string `structs:"current_password,omitempty" json:"currentPassword,omitempty"`
}

func (u User) HasLibraryAccess(libraryID int) bool {
	if u.HasAllLibraryAccess() {
		return true // Background tasks have no personal library selection.
	}
	for _, lib := range u.Libraries {
		if lib.ID == libraryID {
			return true
		}
	}
	return false
}

func (u User) HasAllLibraryAccess() bool {
	return u.IsAdmin && (u.ID == "" || u.AllLibraries)
}

type Users []User

type UserRepository interface {
	rest.Repository[User]
	rest.Persistable[User]
	CountAll(ctx context.Context, options ...QueryOptions) (int64, error)
	Get(ctx context.Context, id string) (*User, error)
	GetAll(ctx context.Context, options ...QueryOptions) (Users, error)
	Put(ctx context.Context, u *User) error
	UpdateLastLoginAt(ctx context.Context, id string) error
	UpdateLastAccessAt(ctx context.Context, id string) error
	FindFirstAdmin(ctx context.Context) (*User, error)
	// FindByUsername must be case-insensitive
	FindByUsername(ctx context.Context, username string) (*User, error)
	// FindByUsernameWithPassword includes the stored plaintext credential for local verification.
	FindByUsernameWithPassword(ctx context.Context, username string) (*User, error)
	// SyncEmosUser uses the external identity as the user ID and preserves existing admin rights.
	// Nil download and password values preserve the corresponding existing fields.
	SyncEmosUser(ctx context.Context, emosID, username string, canDownload *bool, password *string) (*User, error)

	// Library association methods
	GetUserLibraries(ctx context.Context, userID string) (Libraries, error)
	SetUserLibraries(ctx context.Context, userID string, libraryIDs []int) error
}
