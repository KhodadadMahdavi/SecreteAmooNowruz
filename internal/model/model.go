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
