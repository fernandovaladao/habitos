package gamification

import "time"

var Milestones = []Milestone{
	{Value: 3, Bonus: 10, Code: "first-streak", Name: "Primeira sequência", Description: "Concluiu 3 execuções programadas consecutivas."},
	{Value: 7, Bonus: 25, Code: "steady-pace", Name: "Ritmo firme", Description: "Concluiu 7 execuções programadas consecutivas."},
	{Value: 15, Bonus: 50, Code: "consistency", Name: "Constância", Description: "Concluiu 15 execuções programadas consecutivas."},
	{Value: 30, Bonus: 100, Code: "commitment", Name: "Compromisso", Description: "Concluiu 30 execuções programadas consecutivas."},
}

type Milestone struct {
	Value       int
	Bonus       int64
	Code        string
	Name        string
	Description string
}

type Streak struct {
	HabitID                    string    `firestore:"habitId" json:"habitId"`
	OwnerUID                   string    `firestore:"ownerUid" json:"-"`
	CurrentStreak              int       `firestore:"currentStreak" json:"currentStreak"`
	BestStreak                 int       `firestore:"bestStreak" json:"bestStreak"`
	LastScheduledExecutionDate string    `firestore:"lastScheduledExecutionDate,omitempty" json:"lastScheduledExecutionDate,omitempty"`
	MilestonesAwarded          []int     `firestore:"milestonesAwarded" json:"milestonesAwarded"`
	UpdatedAt                  time.Time `firestore:"updatedAt" json:"updatedAt"`
}

type BonusAward struct {
	ID                   string    `firestore:"id" json:"id"`
	OwnerUID             string    `firestore:"ownerUid" json:"-"`
	HabitID              string    `firestore:"habitId" json:"habitId"`
	Milestone            int       `firestore:"milestone" json:"milestone"`
	Points               int64     `firestore:"points" json:"points"`
	TriggerExecutionID   string    `firestore:"triggerExecutionId" json:"triggerExecutionId"`
	TriggerScheduledDate string    `firestore:"triggerScheduledDate" json:"triggerScheduledDate"`
	AwardedAt            time.Time `firestore:"awardedAt" json:"awardedAt"`
}

type UserAchievement struct {
	ID                  string    `firestore:"id" json:"id"`
	OwnerUID            string    `firestore:"ownerUid" json:"-"`
	AchievementCode     string    `firestore:"achievementCode" json:"achievementCode"`
	Name                string    `firestore:"name" json:"name"`
	Description         string    `firestore:"description" json:"description"`
	Milestone           int       `firestore:"milestone" json:"milestone"`
	BonusWhenApplicable int64     `firestore:"bonusWhenApplicable" json:"bonusWhenApplicable"`
	SourceHabitID       string    `firestore:"sourceHabitId" json:"sourceHabitId"`
	SourceExecutionID   string    `firestore:"sourceExecutionId" json:"sourceExecutionId"`
	UnlockedAt          time.Time `firestore:"unlockedAt" json:"unlockedAt"`
}
