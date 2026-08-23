package ranking

import (
	"errors"
	"time"
)

var ErrNotFound = errors.New("participante do ranking não encontrado")

const (
	CollectionName    = "publicRanking"
	DefaultAvatarType = "default"
	TopLimit          = 10
)

type ProjectionInput struct {
	UID                  string
	Nickname             string
	AvatarType           string
	AvatarURL            string
	RankingOptIn         bool
	ProfileComplete      bool
	TotalPoints          int64
	TotalPointsReachedAt *time.Time
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

type Entry struct {
	UID              string    `firestore:"-" json:"-"`
	Nickname         string    `firestore:"nickname" json:"nickname"`
	AvatarType       string    `firestore:"avatarType" json:"avatarType"`
	AvatarURL        string    `firestore:"avatarUrl" json:"avatarUrl"`
	TotalPoints      int64     `firestore:"totalPoints" json:"points"`
	RankingReachedAt time.Time `firestore:"rankingReachedAt" json:"-"`
	UpdatedAt        time.Time `firestore:"updatedAt" json:"-"`
}

type PublicEntry struct {
	Position int    `json:"position"`
	Nickname string `json:"nickname"`
	Avatar   Avatar `json:"avatar"`
	Points   int64  `json:"points"`
	IsSelf   bool   `json:"-"`
}

type Avatar struct {
	Type string `json:"type"`
	URL  string `json:"url,omitempty"`
}

type Board struct {
	Top             []PublicEntry
	Self            *PublicEntry
	PointsToSurpass *int64
	Participating   bool
}

func BuildProjection(input ProjectionInput) (Entry, bool) {
	if !input.RankingOptIn || !input.ProfileComplete || input.UID == "" || input.Nickname == "" {
		return Entry{}, false
	}
	reachedAt := input.CreatedAt
	if input.TotalPointsReachedAt != nil {
		reachedAt = *input.TotalPointsReachedAt
	}
	avatarType := input.AvatarType
	if avatarType == "" {
		avatarType = DefaultAvatarType
	}
	return Entry{
		UID: input.UID, Nickname: input.Nickname, AvatarType: avatarType, AvatarURL: input.AvatarURL,
		TotalPoints: input.TotalPoints, RankingReachedAt: reachedAt, UpdatedAt: input.UpdatedAt,
	}, true
}

func ToPublic(value Entry, position int, self bool) PublicEntry {
	return PublicEntry{Position: position, Nickname: value.Nickname, Avatar: Avatar{Type: value.AvatarType, URL: value.AvatarURL}, Points: value.TotalPoints, IsSelf: self}
}

func PointsNeededToSurpass(current, previous int64) int64 {
	return previous - current + 1
}
