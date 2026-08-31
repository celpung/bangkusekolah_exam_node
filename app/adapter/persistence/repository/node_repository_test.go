package repository

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	helper "github.com/celpung/bangkusekolah_exam_node/app/adapter/persistence/repository/helper"
	"github.com/celpung/bangkusekolah_exam_node/app/domain/entity"
	node_error "github.com/celpung/bangkusekolah_exam_node/app/domain/error"
	"gorm.io/gorm"
)

func TestNodeFindExamByIDSelectsRow(t *testing.T) {
	db, recorded := newDryRunDB(t)
	_, _ = (&nodeRepository{db: db}).FindExamByID(context.Background(), "exam-1")
	requireSQLContaining(t, recorded, "SELECT * FROM `exams`", "WHERE id = ?")
}

func TestNodeFindParticipantByIDSelectsRow(t *testing.T) {
	db, recorded := newDryRunDB(t)
	_, _ = (&nodeRepository{db: db}).FindParticipantByID(context.Background(), "part-1")
	requireSQLContaining(t, recorded, "SELECT * FROM `participants`", "WHERE id = ?")
}

func TestNodeFindParticipantByIDForUpdateLocksRow(t *testing.T) {
	db, recorded := newDryRunDB(t)
	_, _ = (&nodeRepository{db: db}).FindParticipantByIDForUpdate(context.Background(), "part-1")
	stmt := requireSQLContaining(t, recorded, "SELECT * FROM `participants`", "WHERE id = ?")
	if !strings.Contains(stmt.SQL, "FOR UPDATE") && !strings.Contains(stmt.SQL, "FOR SHARE") {
		t.Fatalf("expected FOR UPDATE locking clause, got: %s", stmt.SQL)
	}
}

func TestNodeFindActiveAttemptByParticipantSelectsRow(t *testing.T) {
	db, recorded := newDryRunDB(t)
	_, _ = (&nodeRepository{db: db}).FindActiveAttemptByParticipant(context.Background(), "part-1")
	requireSQLContaining(t, recorded, "SELECT * FROM `attempts`", "WHERE", "participant_id = ?", "status = ?")
}

func TestNodeFindAttemptByIDSelectsRow(t *testing.T) {
	db, recorded := newDryRunDB(t)
	_, _ = (&nodeRepository{db: db}).FindAttemptByID(context.Background(), "att-1")
	requireSQLContaining(t, recorded, "SELECT * FROM `attempts`", "WHERE id = ?")
}

func TestNodeListAnswersByAttemptSelectsRow(t *testing.T) {
	db, recorded := newDryRunDB(t)
	_, _ = (&nodeRepository{db: db}).ListAnswersByAttempt(context.Background(), "att-1")
	requireSQLContaining(t, recorded, "SELECT * FROM `answers`", "WHERE attempt_id = ?")
}

func TestNodeCreateAttemptInsertsRow(t *testing.T) {
	db, recorded := newDryRunDB(t)
	_ = (&nodeRepository{db: db}).CreateAttempt(context.Background(), &entity.Attempt{ID: "att-1", ParticipantID: "part-1", StudentID: "stu-1", AttemptNo: 1, Status: entity.AttemptInProgress, MaxScore: 40})
	requireSQLContaining(t, recorded, "INSERT INTO `attempts`")
}

func TestNodeUpdateParticipantOnlyWritesTwoColumns(t *testing.T) {
	db, recorded := newDryRunDB(t)
	latest := "att-1"
	_ = (&nodeRepository{db: db}).UpdateParticipant(context.Background(), &entity.Participant{ID: "part-1", AttemptCount: 2, LatestAttemptID: &latest})
	stmt := requireSQLContaining(t, recorded, "UPDATE `participants` SET", "WHERE id = ?")
	if !strings.Contains(stmt.SQL, "attempt_count") || !strings.Contains(stmt.SQL, "latest_attempt_id") {
		t.Fatalf("expected attempt_count and latest_attempt_id in UPDATE, got: %s", stmt.SQL)
	}
	// must not update unrelated columns
	if strings.Contains(stmt.SQL, "student_id") || strings.Contains(stmt.SQL, "student_name") || strings.Contains(stmt.SQL, "access_code") {
		t.Fatalf("UpdateParticipant must only write attempt_count and latest_attempt_id, got: %s", stmt.SQL)
	}
}

func TestNodeCountAttemptsByParticipantCounts(t *testing.T) {
	db, recorded := newDryRunDB(t)
	_, _ = (&nodeRepository{db: db}).CountAttemptsByParticipant(context.Background(), "part-1")
	requireSQLContaining(t, recorded, "SELECT count(*) FROM `attempts`", "WHERE participant_id = ?")
}

func TestNodeFindItemByIDSelectsRow(t *testing.T) {
	db, recorded := newDryRunDB(t)
	_, _ = (&nodeRepository{db: db}).FindItemByID(context.Background(), "item-1")
	requireSQLContaining(t, recorded, "SELECT * FROM `items`", "WHERE id = ?")
}

func TestNodeFindParticipantByAccessCodeSelectsRow(t *testing.T) {
	db, recorded := newDryRunDB(t)
	_, _ = (&nodeRepository{db: db}).FindParticipantByAccessCode(context.Background(), "K7M2QX-3B9FTD")
	requireSQLContaining(t, recorded, "SELECT * FROM `participants`", "WHERE access_code = ?")
}

func TestNodeRepositoryUsesTxFromContext(t *testing.T) {
	db, recorded := newDryRunDB(t)
	tx := db.Session(&gorm.Session{})
	ctx := helper.WithTx(context.Background(), tx)
	_, _ = (&nodeRepository{db: db}).FindExamByID(ctx, "exam-1")
	// dry-run still records the query; the point is GetDB(tx) does not panic and uses tx
	requireSQLContaining(t, recorded, "SELECT * FROM `exams`")
}

func TestNodeFindAttemptByIDForUpdateLocksRow(t *testing.T) {
	db, recorded := newDryRunDB(t)
	_, _ = (&nodeRepository{db: db}).FindAttemptByIDForUpdate(context.Background(), "att-1")
	stmt := requireSQLContaining(t, recorded, "SELECT * FROM `attempts`", "WHERE id = ?")
	if !strings.Contains(stmt.SQL, "FOR UPDATE") && !strings.Contains(stmt.SQL, "FOR SHARE") {
		t.Fatalf("expected FOR UPDATE locking clause, got: %s", stmt.SQL)
	}
}

// TestNodeUpdateAttemptConditionalOnInProgress pins BLOCKER-1: the UPDATE must
// carry a status='in_progress' guard so submit and sweeper can never overwrite
// each other's final state.
func TestNodeUpdateAttemptConditionalOnInProgress(t *testing.T) {
	db, recorded := newDryRunDB(t)
	now := time.Now().UTC()
	err := (&nodeRepository{db: db}).UpdateAttempt(context.Background(), &entity.Attempt{
		ID: "att-1", ParticipantID: "part-1", StudentID: "stu-1", Status: entity.AttemptSubmitted,
		SubmittedAt: &now, MaxScore: 30,
	})
	// Dry-run affects zero rows, so the repository maps it to ErrAttemptLocked —
	// which itself proves the conditional update path is wired.
	if !errors.Is(err, node_error.ErrAttemptLocked) {
		t.Fatalf("dry-run RowsAffected=0 must map to ErrAttemptLocked, got %v", err)
	}
	stmt := requireSQLContaining(t, recorded, "UPDATE `attempts` SET", "WHERE id = ? AND status = ?")
	// the status guard var pins in_progress; it is the last bound var
	found := false
	for _, v := range stmt.Vars {
		if s, ok := v.(string); ok && s == string(entity.AttemptInProgress) {
			found = true
		}
	}
	if !found {
		t.Fatalf("status guard must pin in_progress, vars: %v", stmt.Vars)
	}
}

func TestNodeCreateAttemptUsesTxFromContext(t *testing.T) {
	db, recorded := newDryRunDB(t)
	tx := db.Session(&gorm.Session{})
	ctx := helper.WithTx(context.Background(), tx)
	_ = (&nodeRepository{db: db}).CreateAttempt(ctx, &entity.Attempt{ID: "att-1", ParticipantID: "part-1", StudentID: "stu-1", AttemptNo: 1, Status: entity.AttemptInProgress, MaxScore: 40})
	requireSQLContaining(t, recorded, "INSERT INTO `attempts`")
}

func TestNodeUpsertAnswerSingleStatementOnDuplicateKey(t *testing.T) {
	db, recorded := newDryRunDB(t)
	_, _ = (&nodeRepository{db: db}).UpsertAnswer(context.Background(), &entity.Answer{
		ID: "ans-1", AttemptID: "att-1", ItemID: "item-1",
		AnswerJSON: map[string]interface{}{"answer": "A"}, MaxScore: 10,
		GradingStatus: entity.GradingAutoGraded, LastSavedAt: time.Now().UTC(), ClientSeq: 3,
	})
	stmt := requireSQLContaining(t, recorded, "INSERT INTO `answers`", "ON CONFLICT (`attempt_id`,`item_id`) DO UPDATE SET")
	if !strings.Contains(stmt.SQL, "client_seq") {
		t.Fatalf("upsert must guard client_seq: %s", stmt.SQL)
	}
	// every mutable field must be conditionally guarded so a stale seq cannot
	// clobber newer content (review Task 14 BLOCKER-1)
	for _, field := range []string{"answer_json", "answer_text", "score", "grading_status", "last_saved_at"} {
		if !strings.Contains(stmt.SQL, "IF(VALUES(client_seq) > client_seq, VALUES("+field+")") {
			t.Fatalf("field %s must be guarded against stale client_seq: %s", field, stmt.SQL)
		}
	}
}

func TestNodeUpsertAnswerUsesTxFromContext(t *testing.T) {
	db, recorded := newDryRunDB(t)
	tx := db.Session(&gorm.Session{})
	ctx := helper.WithTx(context.Background(), tx)
	_, _ = (&nodeRepository{db: db}).UpsertAnswer(ctx, &entity.Answer{
		ID: "ans-1", AttemptID: "att-1", ItemID: "item-1",
		MaxScore: 10, GradingStatus: entity.GradingPending, LastSavedAt: time.Now().UTC(), ClientSeq: 1,
	})
	requireSQLContaining(t, recorded, "INSERT INTO `answers`", "ON CONFLICT (`attempt_id`,`item_id`) DO UPDATE SET")
}
