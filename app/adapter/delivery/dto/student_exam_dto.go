package dto

import "time"

// StudentExamResponse is the frozen shape for GET /api/v1/student/exams.
// It contains the minimal exam metadata mobile needs.
type StudentExamResponse struct {
	ID              string    `json:"id"`
	Title           string    `json:"title"`
	StartsAt        time.Time `json:"starts_at"`
	EndsAt          time.Time `json:"ends_at"`
	DurationMinutes int       `json:"duration_minutes"`
	MaxAttempts     int       `json:"max_attempts"`
}

type StartAttemptRequest struct {
	DeviceID string `json:"device_id"`
}

// AttemptResponse is the frozen attempt metadata for POST /api/v1/student/exams/{examId}/attempts.
type AttemptResponse struct {
	ID              string     `json:"id"`
	ExamID          string     `json:"exam_id"`
	AttemptNo       int        `json:"attempt_no"`
	Status          string     `json:"status"`
	StartedAt       time.Time  `json:"started_at"`
	DueAt           time.Time  `json:"due_at"`
	SubmittedAt     *time.Time `json:"submitted_at"`
	AutoSubmittedAt *time.Time `json:"auto_submitted_at"`
	Score           *float64   `json:"score"`
	MaxScore        float64    `json:"max_score"`
	GradingStatus   string     `json:"grading_status"`
}
