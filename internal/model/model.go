package model

import "time"

type User struct {
	ID              int64
	Username        string
	PasswordHash    string
	DisplayName     string
	AvatarObjectKey *string
	IsAdmin         bool
	CreatedAt       time.Time
}

type Session struct {
	ID        int64
	UserID    int64
	TokenHash string
	ExpiresAt time.Time
	CreatedAt time.Time
	RevokedAt *time.Time
}

type Game struct {
	ID             int64
	Title          string
	Description    string
	YearGregorian  int
	YearSolarHijri int
	EventDate      time.Time
	SignupOpen     bool
	Status         string
	DrawnAt        *time.Time
	CreatedBy      *int64
	CreatedAt      time.Time
}

type CreateGameInput struct {
	Title          string
	Description    string
	YearGregorian  int
	YearSolarHijri int
	EventDate      time.Time
	CreatedBy      int64
}

type AlbumPhoto struct {
	ID         int64
	GameID     int64
	ObjectKey  string
	Caption    string
	UploadedBy *int64
	CreatedAt  time.Time
	SortOrder  int
}

type CreateAlbumPhotoInput struct {
	GameID     int64
	ObjectKey  string
	Caption    string
	UploadedBy int64
}

type AuditLogInput struct {
	ActorUserID *int64
	Action      string
	EntityType  string
	EntityID    *int64
	Meta        map[string]any
}
