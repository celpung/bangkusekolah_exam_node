package grading

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/celpung/bangkusekolah_exam_node/app/domain/entity"
)

func GradeObjectiveAnswer(item entity.Item, answer *entity.Answer) (float64, bool) {
	if answer == nil || len(answer.AnswerJSON) == 0 {
		switch item.QuestionType {
		case entity.QuestionSingleChoice,
			entity.QuestionTrueFalse,
			entity.QuestionShortAnswer,
			entity.QuestionMultipleChoice,
			entity.QuestionOrdering,
			entity.QuestionMatching:
			return 0, true
		default:
			return 0, false
		}
	}

	switch item.QuestionType {
	case entity.QuestionSingleChoice, entity.QuestionTrueFalse:
		if normalizedScalar(item.AnswerKeySnapshotJSON["answer"]) == normalizedScalar(answer.AnswerJSON["answer"]) {
			return item.Points, true
		}
		return 0, true
	case entity.QuestionShortAnswer:
		actual := normalizedScalar(answer.AnswerJSON["answer"])
		expected := strings.ToLower(strings.TrimSpace(normalizedScalar(item.AnswerKeySnapshotJSON["answer"])))
		if actual == expected {
			return item.Points, true
		}
		for _, accepted := range stringSlice(item.AnswerKeySnapshotJSON["accepted_answers"]) {
			if actual == strings.ToLower(strings.TrimSpace(accepted)) {
				return item.Points, true
			}
		}
		return 0, true
	case entity.QuestionMultipleChoice, entity.QuestionOrdering:
		expected := stringSlice(item.AnswerKeySnapshotJSON["answers"])
		if len(expected) == 0 {
			expected = stringSlice(item.AnswerKeySnapshotJSON["order"])
		}
		actual := stringSlice(answer.AnswerJSON["answers"])
		if len(actual) == 0 {
			actual = stringSlice(answer.AnswerJSON["order"])
		}
		if sameOrderedOrSet(item.QuestionType, expected, actual) {
			return item.Points, true
		}
		return 0, true
	case entity.QuestionMatching:
		if reflect.DeepEqual(normalizedMap(item.AnswerKeySnapshotJSON["pairs"]), normalizedMap(answer.AnswerJSON["pairs"])) {
			return item.Points, true
		}
		return 0, true
	default:
		return 0, false
	}
}

func normalizedScalar(v interface{}) string {
	return strings.ToLower(strings.TrimSpace(fmt.Sprint(v)))
}

func stringSlice(v interface{}) []string {
	switch value := v.(type) {
	case []string:
		return value
	case []interface{}:
		result := make([]string, 0, len(value))
		for _, item := range value {
			result = append(result, normalizedScalar(item))
		}
		return result
	default:
		if value == nil {
			return nil
		}
		return []string{normalizedScalar(value)}
	}
}

func sameOrderedOrSet(questionType entity.QuestionType, expected []string, actual []string) bool {
	if len(expected) != len(actual) {
		return false
	}
	if questionType == entity.QuestionOrdering {
		for i := range expected {
			if normalizedScalar(expected[i]) != normalizedScalar(actual[i]) {
				return false
			}
		}
		return true
	}
	left := append([]string(nil), expected...)
	right := append([]string(nil), actual...)
	sort.Strings(left)
	sort.Strings(right)
	return reflect.DeepEqual(left, right)
}

func normalizedMap(v interface{}) map[string]string {
	result := map[string]string{}
	if m, ok := v.(map[string]interface{}); ok {
		for key, value := range m {
			result[normalizedScalar(key)] = normalizedScalar(value)
		}
	}
	if m, ok := v.(map[string]string); ok {
		for key, value := range m {
			result[normalizedScalar(key)] = normalizedScalar(value)
		}
	}
	return result
}
