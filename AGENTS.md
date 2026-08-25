# AGENTS — bangkusekolah_exam_node

Hexagonal layout mirroring bangkusekolah_service. Same coding rules as central AGENTS.md §2: DRY, single responsibility, KISS, YAGNI, clear naming, meaningful errors, no magic values, comments only where non-obvious.

Scope: student sitting flow only — login, ListStudentExams, StartAttempt, GetAttempt, AutosaveAnswer, SubmitAttempt, RecordIntegrityEvent, GetStudentResult. No authoring, question bank, participant management, teacher grading, analytics, gradebook, notifications or audit log.

DRY exception: app/domain/grading (gradeObjectiveAnswer + 4 helpers) copied from central and pinned by byte-identical testdata/grading/vectors.json.
