package auth

import (
	"errors"
	"fmt"
	"time"
)

var (
	ErrNotFound           = errors.New("auth: not found")
	ErrConflict           = errors.New("auth: conflict")
	ErrInvalidCredentials = errors.New("auth: invalid credentials")
	ErrUnauthorized       = errors.New("auth: unauthorized")
	ErrDisabled           = errors.New("auth: account disabled")
	ErrPending            = errors.New("auth: account pending approval")
	ErrRegisterDisabled   = errors.New("auth: registration disabled")
	ErrInvalidInvite      = errors.New("auth: invalid invite")
	ErrBusy               = errors.New("auth: password service busy")
	ErrInvalidUsername    = errors.New("auth: invalid username")
	ErrInvalidEmail       = errors.New("auth: invalid email")
	ErrPasswordTooShort   = errors.New("auth: password too short")
	ErrPasswordTooLong    = errors.New("auth: password too long")
)

type RateLimitError struct {
	Wait time.Duration
}

func (e *RateLimitError) Error() string {
	if e.Wait > 0 {
		return fmt.Sprintf("auth: too many attempts, retry in %s", e.Wait.Round(time.Second))
	}
	return "auth: too many attempts"
}

type AdminUser struct {
	ID                int64
	Username          string
	PasswordHash      string
	DisplayName       string
	Role              string
	Status            string
	CreatedAt         int64
	UpdatedAt         int64
	LastLoginAt       int64
	PasswordChangedAt int64
}

type AdminSession struct {
	ID          string
	AdminID     int64
	Username    string
	DisplayName string
	Role        string
	Status      string
	CSRF        string
	IP          string
	UserAgent   string
	CreatedAt   int64
	ExpiresAt   int64
	LastSeenAt  int64
}

type User struct {
	ID                int64
	Username          string
	Email             string
	PasswordHash      string
	DisplayName       string
	AvatarMediaID     int64
	Status            string
	CreatedAt         int64
	UpdatedAt         int64
	LastLoginAt       int64
	PasswordChangedAt int64
}

type UserSession struct {
	ID          string
	UserID      int64
	Username    string
	Email       string
	DisplayName string
	Status      string
	CSRF        string
	IP          string
	UserAgent   string
	CreatedAt   int64
	ExpiresAt   int64
	LastSeenAt  int64
}

type Invite struct {
	ID               int64
	CodeHash         string
	CreatedByAdminID int64
	CreatedBy        string
	ExpiresAt        int64
	UsedByUserID     int64
	UsedAt           int64
	RevokedAt        int64
	CreatedAt        int64
}

type AuditLog struct {
	ID         int64
	ActorType  string
	ActorID    int64
	Action     string
	TargetType string
	TargetID   int64
	IP         string
	Meta       string
	CreatedAt  int64
}

type Counts struct {
	Admins       int
	Users        int
	PendingUsers int
}
