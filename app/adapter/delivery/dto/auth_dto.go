package dto

// LoginRequest is the body for POST /api/v1/auth/exam-login.
// Code is the Crockford base32 <EXAM6>-<PART6> string, case-insensitive on input.
type LoginRequest struct {
	Code string `json:"code"`
}
