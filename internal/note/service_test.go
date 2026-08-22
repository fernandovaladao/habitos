package note

import (
	"context"
	"errors"
	"habitos/internal/auth"
	"habitos/internal/execution"
	"habitos/internal/habit"
	"strings"
	"testing"
	"time"
)

type memoryRepo struct {
	values map[string]Note
	next   int
}

func (r *memoryRepo) NewID() string { r.next++; return string(rune('a' + r.next)) }
func (r *memoryRepo) Create(_ context.Context, v Note) (Note, error) {
	r.values[v.ID] = v
	return v, nil
}
func (r *memoryRepo) Get(_ context.Context, u, id string) (Note, error) {
	v, ok := r.values[id]
	if !ok || v.OwnerUID != u {
		return Note{}, ErrNotFound
	}
	return v, nil
}
func (r *memoryRepo) ListByHabit(_ context.Context, u, h string) ([]Note, error) {
	var out []Note
	for _, v := range r.values {
		if v.OwnerUID == u && v.HabitID == h {
			out = append(out, v)
		}
	}
	return out, nil
}
func (r *memoryRepo) Update(_ context.Context, u, id, c string, n time.Time) (Note, error) {
	v, e := r.Get(context.Background(), u, id)
	if e != nil {
		return Note{}, e
	}
	v.Content = c
	v.UpdatedAt = n
	r.values[id] = v
	return v, nil
}
func (r *memoryRepo) Delete(_ context.Context, u, id string) error {
	if _, e := r.Get(context.Background(), u, id); e != nil {
		return e
	}
	delete(r.values, id)
	return nil
}

type habitReader struct{}

func (habitReader) Get(_ context.Context, i auth.Identity, id string) (habit.Habit, error) {
	if i.UID != "u1" || id != "h1" {
		return habit.Habit{}, habit.ErrNotFound
	}
	return habit.Habit{ID: id, OwnerUID: i.UID}, nil
}

type executionReader struct{}

func (executionReader) Get(_ context.Context, i auth.Identity, id string) (execution.Execution, error) {
	if i.UID != "u1" || id != "e1" {
		return execution.Execution{}, execution.ErrNotFound
	}
	return execution.Execution{ID: id, OwnerUID: i.UID, HabitID: "h1"}, nil
}
func TestNotesValidationAndAuthorization(t *testing.T) {
	repo := &memoryRepo{values: map[string]Note{}}
	s := NewService(repo, habitReader{}, executionReader{})
	identity := auth.Identity{UID: "u1", Email: "u@test"}
	for _, content := range []string{"", "   ", strings.Repeat("a", 1001)} {
		if _, err := s.Create(context.Background(), identity, "h1", "", content); !errors.Is(err, ErrInvalidContent) {
			t.Fatalf("conteúdo deveria falhar: %v", err)
		}
	}
	created, err := s.Create(context.Background(), identity, "h1", "e1", " reflexão ")
	if err != nil || created.Content != "reflexão" {
		t.Fatalf("criar=%#v erro=%v", created, err)
	}
	other := auth.Identity{UID: "u2", Email: "o@test"}
	if _, err := s.Update(context.Background(), other, created.ID, "invadir"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("outro usuário editou: %v", err)
	}
	if err := s.Delete(context.Background(), other, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("outro usuário excluiu: %v", err)
	}
}
