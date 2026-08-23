package gamification

import (
	"errors"
	"math/big"
)

var ErrInvalidGoal = errors.New("meta inválida para pontuação")

func QuantitativePoints(achieved, target int64) (int, error) {
	if target <= 0 || achieved < 0 {
		return 0, ErrInvalidGoal
	}
	if achieved == 0 {
		return 0, nil
	}
	if achieved >= target {
		return 10, nil
	}
	numerator := new(big.Int).Mul(big.NewInt(achieved), big.NewInt(10))
	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(numerator, big.NewInt(target), remainder)
	if new(big.Int).Mul(remainder, big.NewInt(2)).Cmp(big.NewInt(target)) >= 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	return int(quotient.Int64()), nil
}

func MilestonesReached(before, after int) []Milestone {
	var result []Milestone
	for _, milestone := range Milestones {
		if before < milestone.Value && after >= milestone.Value {
			result = append(result, milestone)
		}
	}
	return result
}

type SequenceRecord struct {
	ScheduledDate string
	Status        string
}

type SequenceStep struct {
	Before    int
	After     int
	Confirmed bool
}

type SequenceProjection struct {
	Steps             []SequenceStep
	Current           int
	Best              int
	LastConfirmedDate string
}

func ProjectSequence(records []SequenceRecord, historicalBest int, resetDate string) SequenceProjection {
	projection := SequenceProjection{Steps: make([]SequenceStep, len(records)), Best: historicalBest}
	current, blocked := 0, false
	resetApplied := resetDate == ""
	for index, record := range records {
		if !resetApplied && record.ScheduledDate >= resetDate {
			current, blocked, resetApplied = 0, false, true
		}
		step := SequenceStep{Before: current, After: current}
		if record.Status == "pending" {
			blocked = true
		} else if !blocked {
			step.Confirmed = true
			if record.Status == "completed" {
				step.After++
			} else {
				step.After = 0
			}
			current = step.After
			projection.LastConfirmedDate = record.ScheduledDate
			if current > projection.Best {
				projection.Best = current
			}
		}
		projection.Steps[index] = step
	}
	if !resetApplied {
		current = 0
		projection.LastConfirmedDate = ""
	}
	projection.Current = current
	return projection
}
