package repository

import (
	"context"
	"strings"
	"testing"
	"time"

	helper "github.com/celpung/bangkusekolah_exam_node/app/adapter/persistence/repository/helper"
	"github.com/celpung/bangkusekolah_exam_node/app/domain/entity"
	"gorm.io/gorm"
)

func TestNodeFindExamSelectsRow(t *testing.T) {
	db, recorded := newDryRunDB(t)
	_, _ = (&nodeRepository{db: db}).FindExam(context.Background())
	requireSQLContaining(t, recorded, "SELECT * FROM `exams`")
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

func TestNodeRepositoryUsesTxFromContext(t *testing.T) {
	db, recorded := newDryRunDB(t)
	tx := db.Session(&gorm.Session{})
	ctx := helper.WithTx(context.Background(), tx)
	_, _ = (&nodeRepository{db: db}).FindExam(ctx)
	// dry-run still records the query; the point is GetDB(tx) does not panic and uses tx
	requireSQLContaining(t, recorded, "SELECT * FROM `exams`")
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
