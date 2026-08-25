package grading

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"

	"github.com/celpung/bangkusekolah_exam_node/app/domain/entity"
)

const vectorFile = "../../../testdata/grading/vectors.json"

const centralChecksum = "45d8661588390c4c7eb3d2146c4b3a7bbbb5527277d29a0e7fce48cab7dd1ad4"

type gradingVector struct {
	Name         string                 `json:"name"`
	QuestionType string                 `json:"question_type"`
	Points       float64                `json:"points"`
	AnswerKey    map[string]interface{} `json:"answer_key"`
	Answer       map[string]interface{} `json:"answer"`
	WantScore    float64                `json:"want_score"`
	WantGraded   bool                   `json:"want_graded"`
}

type vectorFileContent struct {
	Version int             `json:"version"`
	Cases   []gradingVector `json:"cases"`
}

func TestGradingGoldenVectors(t *testing.T) {
	raw, err := os.ReadFile(vectorFile)
	if err != nil {
		t.Fatalf("read grading vectors: %v — did you cp testdata/grading/vectors.json from central?", err)
	}
	sum := sha256.Sum256(raw)
	got := hex.EncodeToString(sum[:])
	if got != centralChecksum {
		t.Fatalf("vectors.json checksum mismatch: got %s, want %s — file must be byte-identical to central's copy", got, centralChecksum)
	}
	var content vectorFileContent
	if err := json.Unmarshal(raw, &content); err != nil {
		t.Fatalf("parse vectors: %v", err)
	}
	if len(content.Cases) == 0 {
		t.Fatal("grading vectors file has no cases")
	}
	if content.Version != 1 {
		t.Fatalf("vectors version = %d, want 1", content.Version)
	}
	for _, v := range content.Cases {
		v := v
		t.Run(v.Name, func(t *testing.T) {
			item := entity.Item{
				QuestionType:          entity.QuestionType(v.QuestionType),
				Points:                v.Points,
				AnswerKeySnapshotJSON: v.AnswerKey,
			}
			answer := entity.Answer{AnswerJSON: v.Answer}
			score, graded := GradeObjectiveAnswer(item, &answer)
			if graded != v.WantGraded || score != v.WantScore {
				t.Fatalf("GradeObjectiveAnswer() = (%v, %v), want (%v, %v)", score, graded, v.WantScore, v.WantGraded)
			}
		})
	}
}

func TestGradingGoldenVectorsCoversEveryQuestionType(t *testing.T) {
	raw, err := os.ReadFile(vectorFile)
	if err != nil {
		t.Fatalf("read vectors: %v", err)
	}
	var content vectorFileContent
	if err := json.Unmarshal(raw, &content); err != nil {
		t.Fatalf("parse: %v", err)
	}
	seen := map[string]bool{}
	for _, c := range content.Cases {
		seen[c.QuestionType] = true
	}
	for _, qt := range []string{"single_choice", "multiple_choice", "true_false", "short_answer", "matching", "ordering", "essay", "file_upload"} {
		if !seen[qt] {
			t.Fatalf("vectors.json has no case for %q", qt)
		}
	}
}
